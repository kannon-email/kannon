package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/audit"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/authzconnect"
	sq "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/mailflow"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/templates"
	"github.com/kannon-email/kannon/pkg/api/adminapi"
	"github.com/kannon-email/kannon/pkg/api/apimetrics"
	"github.com/kannon-email/kannon/pkg/api/connecterr"
	"github.com/kannon-email/kannon/pkg/api/health"
	"github.com/kannon-email/kannon/pkg/api/hzapi"
	"github.com/kannon-email/kannon/pkg/api/mailapi"
	"github.com/kannon-email/kannon/pkg/api/rest"
	"github.com/kannon-email/kannon/pkg/hz"
	"github.com/kannon-email/kannon/pkg/statsapi/statsv1"
	"github.com/kannon-email/kannon/pkg/statsapi/statsv2"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	statsv1connect "github.com/kannon-email/kannon/proto/kannon/stats/apiv1/apiv1connect"
	statsv2connect "github.com/kannon-email/kannon/proto/kannon/stats/apiv2/apiv2connect"
	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/container"
	"github.com/kannon-email/kannon/x/metrics"
	"golang.org/x/sync/errgroup"
)

type Config struct {
	// Port serves the Connect, gRPC and gRPC-Web APIs.
	Port uint `mapstructure:"port"`
	// RESTPort serves the REST API on a listener of its own, so it can be published without the
	// Connect Admin and Stats services that share Port.
	RESTPort uint `mapstructure:"rest_port"`
	// HealthPort serves the readiness check for probes inside the cluster. It must not be
	// published: the check is kept off the REST API so the internet cannot poll it.
	HealthPort uint `mapstructure:"health_port"`
	// MaxPayload is the largest content, in bytes, a Batch may carry: its HTML plus each
	// attachment as base64. Each Envelope travels as one NATS message, so set it to about 70% of
	// the NATS server's max_payload, leaving room for the headers, DKIM signature and tracking
	// links building adds.
	MaxPayload config.ByteSize `mapstructure:"max_payload"`
}

// defaultMaxPayload is 500KiB, under 70% of the 1MiB max_payload a NATS server has by default.
const defaultMaxPayload config.ByteSize = 500 << 10

func (c *Config) setDefaults() {
	if c.Port == 0 {
		c.Port = 50051
	}
	if c.RESTPort == 0 {
		c.RESTPort = 8081
	}
	if c.HealthPort == 0 {
		c.HealthPort = 8082
	}
	if c.MaxPayload <= 0 {
		c.MaxPayload = defaultMaxPayload
	}
}

// AdminToken resolves the credential that authenticates the Admin API and both Stats API versions.
// Exported so the boot path can refuse to start a process asked to serve them without one, rather
// than let it come up and answer every request with unauthenticated (ADR 0009).
func AdminToken() (admintoken.Token, error) {
	raw, err := config.APIAdminToken()
	if err != nil {
		return admintoken.Token{}, adminTokenError(err)
	}
	token, err := admintoken.Parse(raw)
	if err != nil {
		return admintoken.Token{}, adminTokenError(err)
	}
	return token, nil
}

// adminTokenError is one message for both halves — a token that could not be resolved and a token
// that is not one — because the operator's next move is the same, and it is to look at this key.
func adminTokenError(err error) error {
	return fmt.Errorf(
		"the API needs an admin token to authenticate the Admin and Stats APIs: set %q in the config file "+
			"to a random value of at least %d characters (`openssl rand -hex 32`), taking it from the "+
			"environment as `admin_token: env://KANNON_ADMIN_TOKEN` if you would rather not write it there (%w)",
		config.APIAdminTokenKey, admintoken.MinSecretLength, err)
}

const (
	configKey = "api"
	// PortKey, RESTPortKey and HealthPortKey name the keys the API's ports are read from.
	PortKey       = configKey + ".port"
	RESTPortKey   = configKey + ".rest_port"
	HealthPortKey = configKey + ".health_port"
)

// ListenPorts reads the ports the API listens on, so the boot path can refuse another listener asked
// to share one.
func ListenPorts() ([]metrics.ListenPort, error) {
	var cfg Config
	if err := config.TryLoadSection(configKey, &cfg); err != nil {
		return nil, err
	}
	cfg.setDefaults()
	return cfg.listenPorts(), nil
}

func (c Config) listenPorts() []metrics.ListenPort {
	return []metrics.ListenPort{{Key: PortKey, Port: c.Port}, {Key: RESTPortKey, Port: c.RESTPort}, {Key: HealthPortKey, Port: c.HealthPort}}
}

func (c Config) checkPorts() error {
	ports := c.listenPorts()
	for i, a := range ports {
		for _, b := range ports[i+1:] {
			if a.Port == b.Port {
				return fmt.Errorf("%s and %s are both %d: the Connect API, the REST API and the health check need a port each", a.Key, b.Key, a.Port)
			}
		}
	}
	return nil
}

// New constructs the API runnable, loading its slice of configuration from
// viper under the "api" key.
func New(cnt *container.Container) container.Runnable {
	var cfg Config
	config.LoadSection(configKey, &cfg)
	cfg.setDefaults()
	return container.Runnable{
		Name: "api",
		Run: func(ctx context.Context) error {
			return run(ctx, cfg, cnt)
		},
	}
}

func run(ctx context.Context, config Config, cnt *container.Container) error {
	if err := config.checkPorts(); err != nil {
		return err
	}

	// Resolved before the listener opens, and again here rather than only in the boot path: a
	// caller registering this runnable directly — the e2e suite does — must not be able to
	// stand up an Admin API whose credential nobody checked.
	adminToken, err := AdminToken()
	if err != nil {
		return err
	}

	db := cnt.DB()

	statsRepo := sq.NewStatsRepository(db)
	aggregatedRepo := sq.NewAggregatedStatsRepository(db)
	statsService := stats.NewService(statsRepo, stats.WithAggregatedStatsRepository(aggregatedRepo))

	// Started before the handlers are mounted, and unable to fail: what an audit trail costs an
	// operator who enabled it must not include the API refusing to serve.
	recorder := startAuditRecording(ctx, cnt)

	adminAPIService := adminapi.CreateAdminAPIService(db)
	flow := mailflow.New(cnt.Metrics())
	mailAPIService := mailapi.NewMailerAPIV1(db, cnt.BackoffPolicy(), cnt.RetryWindow(), int(config.MaxPayload), flow)
	statsAPIService := statsv1.NewStatsAPIService(statsService)
	statsV2APIService := statsv2.NewStatsAPIService(statsService)
	hzAPIService := hzapi.CreateHZAPIService(cnt)

	// The operator's credential is read here, once, and handed down. Nothing beneath
	// this point asks the configuration layer what authority a request has.
	adminAuth := authzconnect.AdminTokenHandlerOptions(adminToken)

	calls := apimetrics.New(cnt.Metrics())

	connectServer := newServer(config.Port, recorder, newAPIMux(connectReadMaxBytes(config.MaxPayload), adminAuth, calls, adminAPIService, mailAPIService, statsAPIService, statsV2APIService, hzAPIService), connectProtocols())
	restServer := newServer(config.RESTPort, recorder, calls.REST(rest.NewHandler(rest.Services{
		Domains:   domains.NewService(sq.NewDomainsRepository(db)),
		Token:     adminToken,
		Keys:      apikeys.NewService(sq.NewAPIKeysRepository(db)),
		Templates: templates.NewService(sq.NewTemplatesRepository(db)),
		Inline:    intake.NewInlineService(sq.NewInlineStore(db), sq.NewDomainsRepository(db), cnt.BackoffPolicy(), cnt.RetryWindow(), int(config.MaxPayload)),
		Flow:      flow,
		Stats:     statsService,
	})), nil)
	healthServer := newServer(config.HealthPort, nil, health.NewHandler(func(ctx context.Context) error {
		for _, err := range hz.NewHZ(cnt).HZ(ctx) {
			if err != nil {
				return err
			}
		}
		return nil
	}), nil)
	slog.Info("Connect API server listening on " + connectServer.Addr)
	slog.Info("REST API server listening on " + restServer.Addr)
	slog.Info("health server listening on " + healthServer.Addr)
	return serveAll(ctx, connectServer, restServer, healthServer)
}

// startAuditRecording resolves the Recorder every authorization decision on this process reports to,
// and nil when the operator asked for no audit trail — which is the default. Nil means "install
// nothing", so Guard keeps the logging Recorder it has always had and this process never connects to
// NATS on account of a feature nobody enabled: no new network requirement, no new failure mode.
//
// Named for what it starts and not for what it returns, because it does both: it acquires the
// connection and leaves a watch running behind it (ADR 0010).
//
// Nothing here can fail the boot. An audit trail Kannon cannot write must not be an outage for
// somebody's customers, so every failure below is reported and stepped over.
func startAuditRecording(ctx context.Context, cnt *container.Container) authz.Recorder {
	// TryLoadConfig and not LoadConfig: this runs inside the API runnable's goroutine, where the
	// panic a section read normally raises would be recovered by nobody and would end the process —
	// an `audit.retention` naming a variable this pod does not set would stop the API from serving.
	cfg, err := audit.TryLoadConfig()
	if err != nil {
		slog.Error("cannot read the audit configuration, so authorization decisions stay in the log "+
			"rather than reaching the audit table; the API is serving regardless", "err", err)
		return nil
	}

	if !cfg.Enabled {
		return nil
	}

	// TryNatsJetStream and not NatsJetStream: the latter exits the process, which for the workers is
	// right — a NATS they cannot reach is fatal to their whole job — and for the API is not. Before
	// this feature the API never touched NATS at all, so exiting here would mean enabling an audit
	// trail bought an API that crash-loops whenever NATS is slow to come up (#365).
	js, err := cnt.TryNatsJetStream()
	if err != nil {
		slog.Error("cannot reach NATS, so authorization decisions stay in the log rather than "+
			"reaching the audit table; the API is serving regardless", "err", err)
		return nil
	}

	// Configured here as well as in the audit runnable. CreateOrUpdateStream is idempotent, and
	// neither process may depend on the other's boot order: a publish must not go into a stream
	// that does not exist, and the worker must not require the API to have come up.
	//
	// A failure is logged and the Recorder installed anyway. The worker configures the same stream,
	// so this having failed does not mean the stream never appears — and refusing to serve mail
	// because NATS was slow would answer a gap in the register with an interruption of service.
	// Decisions published before the stream exists are the unconfirmed loss the ADR already records
	// as the cost of a core publish, and the pending-with-no-consumers warning is what watches for it.
	if err := audit.ConfigureStream(ctx, js); err != nil {
		slog.Error("cannot configure the audit stream; recording anyway, and the audit writer will "+
			"configure it when it starts", "err", err)
	}

	// Beside the server rather than on the request path, and its failure is logged rather than
	// returned: a deployment that enabled collection and forgot the worker has a gap in its
	// register, which must not be turned into an interruption of service.
	go func() {
		if err := audit.WatchBacklog(ctx, js); err != nil {
			slog.Debug("stopped watching the audit stream for a backlog", "err", err)
		}
	}()

	// Decorating the logging Recorder and not replacing it: turning the table on must not take away
	// the lines an operator already relies on, and a publish that fails falls through to them.
	return audit.NewRecorder(cnt.NatsPublisher(), authz.LogRecorder())
}

// withRecorder installs the Recorder on every request's context, so that Guard finds it wherever the
// request ends up. One middleware over the whole mux rather than a Connect interceptor per handler:
// the Mailer API reaches Guard exactly as the other surfaces do, and it is the one surface the admin
// token does not authenticate — so anything hung off the admin options would miss every send.
//
// A nil Recorder installs nothing, which is how a deployment with no audit trail is left untouched.
func withRecorder(r authz.Recorder, next http.Handler) http.Handler {
	if r == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		next.ServeHTTP(w, req.WithContext(authz.WithRecorder(req.Context(), r)))
	})
}

// connectReadHeadroom is what a Connect request may carry beyond the content intake measures
// against max_payload: Recipients and their fields, global fields, headers and framing, none of
// which that check counts. 10MiB, the same as a REST body may be.
const connectReadHeadroom = 10 << 20

// connectReadMaxBytes bounds the Connect request a handler will read, decompressed. connect-go reads
// and decodes the whole body before any interceptor or handler runs — before the admin token or the
// sender credential is checked — so without it anyone can make this process allocate without limit,
// and a gzip-compressed body gets past a proxy's cap. Derived from max_payload rather than fixed, so
// a deployment that raises the content it accepts does not find it refused at the transport.
func connectReadMaxBytes(maxPayload config.ByteSize) int {
	return int(maxPayload) + connectReadHeadroom
}

// connectProtocols adds h2c, which gRPC clients need to reach the Connect listener without TLS.
func connectProtocols() *http.Protocols {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	return protocols
}

// newServer bounds header reads and idle connections without limiting body transfers.
func newServer(port uint, recorder authz.Recorder, handler http.Handler, protocols *http.Protocols) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", port),
		Handler:           withRecorder(recorder, handler),
		Protocols:         protocols,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// serveAll serves every server until ctx is done or one of them fails, which stops the others: a
// process that has lost one of its APIs must not go on looking healthy through the other.
func serveAll(ctx context.Context, servers ...*http.Server) error {
	g, ctx := errgroup.WithContext(ctx)
	for _, server := range servers {
		g.Go(func() error { return container.ServeHTTP(ctx, server) })
	}
	return g.Wait()
}

// newAPIMux routes every Connect handler. readMaxBytes bounds every Connect handler, the unauthenticated
// ones included. adminAuth authenticates the three surfaces that answer to the operator's admin
// token. The mailer handler must never get it — it authenticates its own sender credential — and
// neither must health, which discloses nothing and is polled unauthenticated.
func newAPIMux(readMaxBytes int, adminAuth []connect.HandlerOption, calls *apimetrics.Metrics, adminServer adminv1connect.ApiHandler, mailerServer mailerv1connect.MailerHandler, statsServer statsv1connect.StatsApiV1Handler, statsV2Server statsv2connect.StatsApiV2Handler, hzServer adminv1connect.HZServiceHandler) *http.ServeMux {
	mux := http.NewServeMux()

	common := []connect.HandlerOption{connect.WithReadMaxBytes(readMaxBytes), connect.WithInterceptors(calls.Connect(), connecterr.Interceptor())}
	admin := append(slices.Clone(common), adminAuth...)

	adminPath, adminHandler := calls.ConnectService(adminv1connect.NewApiHandler(adminServer, admin...))
	mailerPath, mailerHandler := calls.ConnectService(mailerv1connect.NewMailerHandler(mailerServer, common...))
	statsPath, statsHandler := calls.ConnectService(statsv1connect.NewStatsApiV1Handler(statsServer, admin...))
	statsV2Path, statsV2Handler := calls.ConnectService(statsv2connect.NewStatsApiV2Handler(statsV2Server, admin...))
	hzPath, hzHandler := calls.ConnectService(adminv1connect.NewHZServiceHandler(hzServer, common...))

	mux.Handle(adminPath, adminHandler)
	mux.Handle(mailerPath, mailerHandler)
	mux.Handle(statsPath, statsHandler)
	mux.Handle(statsV2Path, statsV2Handler)
	mux.Handle(hzPath, hzHandler)
	return mux
}
