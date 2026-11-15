package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
	Port uint
	// RESTPort serves the REST API on a listener of its own, so it can be published without the
	// Connect Admin and Stats services that share Port.
	RESTPort uint
	// HealthPort serves the readiness check for probes inside the cluster. It must not be
	// published: the check is kept off the REST API so the internet cannot poll it.
	HealthPort uint
	// MaxPayload is the largest content, in bytes, a Batch may carry: its HTML plus each
	// attachment as base64. Each Envelope travels as one NATS message, so set it to about 70% of
	// the NATS server's max_payload, leaving room for the headers, DKIM signature and tracking
	// links building adds.
	MaxPayload config.ByteSize
}

// defaultMaxPayload is 500KiB, under 70% of the 1MiB max_payload a NATS server has by default.
const defaultMaxPayload config.ByteSize = 500 << 10

// LoadConfig reads the "api" section. An unset port takes its default; port 0 lets the system pick
// one.
func LoadConfig() (Config, error) {
	var raw struct {
		Port       *uint           `mapstructure:"port"`
		RESTPort   *uint           `mapstructure:"rest_port"`
		HealthPort *uint           `mapstructure:"health_port"`
		MaxPayload config.ByteSize `mapstructure:"max_payload"`
	}
	if err := config.TryLoadSection(configKey, &raw); err != nil {
		return Config{}, err
	}
	cfg := Config{
		Port:       config.Or(raw.Port, 50051),
		RESTPort:   config.Or(raw.RESTPort, 8081),
		HealthPort: config.Or(raw.HealthPort, 8082),
		MaxPayload: raw.MaxPayload,
	}
	if cfg.MaxPayload <= 0 {
		cfg.MaxPayload = defaultMaxPayload
	}
	return cfg, nil
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

// ListenPorts are the ports the API listens on, so the boot path can refuse another listener asked
// to share one.
func (c Config) ListenPorts() []metrics.ListenPort {
	return []metrics.ListenPort{{Key: PortKey, Port: c.Port}, {Key: RESTPortKey, Port: c.RESTPort}, {Key: HealthPortKey, Port: c.HealthPort}}
}

func (c Config) checkPorts() error {
	ports := c.ListenPorts()
	for i, a := range ports {
		for _, b := range ports[i+1:] {
			if a.Port != 0 && a.Port == b.Port {
				return fmt.Errorf("%s and %s are both %d: the Connect API, the REST API and the health check need a port each", a.Key, b.Key, a.Port)
			}
		}
	}
	return nil
}

// Listeners are the API's three listeners, bound before the runnable starts.
type Listeners struct {
	Connect, REST, Health net.Listener
}

// Close closes every listener, for a boot that fails after binding them.
func (l Listeners) Close() error {
	return errors.Join(l.Connect.Close(), l.REST.Close(), l.Health.Close())
}

// Listen binds the API's listeners on cfg's ports, refusing two of them on one port.
func Listen(cfg Config) (Listeners, error) {
	if err := cfg.checkPorts(); err != nil {
		return Listeners{}, err
	}
	var bound []net.Listener
	for _, port := range []uint{cfg.Port, cfg.RESTPort, cfg.HealthPort} {
		l, err := container.Listen(port)
		if err != nil {
			for _, b := range bound {
				_ = b.Close()
			}
			return Listeners{}, err
		}
		bound = append(bound, l)
	}
	return Listeners{Connect: bound[0], REST: bound[1], Health: bound[2]}, nil
}

// New constructs the API runnable serving on l, configured by cfg. token is the admin credential the boot path has
// already resolved, and instanceReady closes once the whole process is ready: until then the health
// check answers unavailable.
func New(cnt *container.Container, cfg Config, token admintoken.Token, l Listeners, instanceReady <-chan struct{}) container.Runnable {
	return container.Runnable{
		Name: "api",
		Run: func(ctx context.Context, ready func()) error {
			return run(ctx, cfg, cnt, token, l, instanceReady, ready)
		},
	}
}

// errNotReady is what the health check reports before every component of the process is ready.
var errNotReady = errors.New("the instance is not ready yet")

// readiness reports the process unavailable until instanceReady closes, and dependency health after.
func readiness(instanceReady <-chan struct{}, dependencies func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		select {
		case <-instanceReady:
			return dependencies(ctx)
		default:
			return errNotReady
		}
	}
}

func run(ctx context.Context, config Config, cnt *container.Container, adminToken admintoken.Token, l Listeners, instanceReady <-chan struct{}, ready func()) error {
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

	connectServer := newServer(recorder, newAPIMux(connectReadMaxBytes(config.MaxPayload), adminAuth, calls, adminAPIService, mailAPIService, statsAPIService, statsV2APIService, hzAPIService), connectProtocols())
	restServer := newServer(recorder, calls.REST(rest.NewHandler(rest.Services{
		Domains:   domains.NewService(sq.NewDomainsRepository(db)),
		Token:     adminToken,
		Keys:      apikeys.NewService(sq.NewAPIKeysRepository(db)),
		Templates: templates.NewService(sq.NewTemplatesRepository(db)),
		Inline:    intake.NewInlineService(sq.NewInlineStore(db), sq.NewDomainsRepository(db), cnt.BackoffPolicy(), cnt.RetryWindow(), int(config.MaxPayload)),
		Flow:      flow,
		Stats:     statsService,
	})), nil)
	healthServer := newServer(nil, health.NewHandler(readiness(instanceReady, func(ctx context.Context) error {
		for _, err := range hz.NewHZ(cnt).HZ(ctx) {
			if err != nil {
				return err
			}
		}
		return nil
	})), nil)
	slog.Info("Connect API server listening on " + l.Connect.Addr().String())
	slog.Info("REST API server listening on " + l.REST.Addr().String())
	slog.Info("health server listening on " + l.Health.Addr().String())
	ready()
	return serveAll(ctx, served{connectServer, l.Connect}, served{restServer, l.REST}, served{healthServer, l.Health})
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
func newServer(recorder authz.Recorder, handler http.Handler, protocols *http.Protocols) *http.Server {
	return &http.Server{
		Handler:           withRecorder(recorder, handler),
		Protocols:         protocols,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// serveAll serves every server until ctx is done or one of them fails, which stops the others: a
// process that has lost one of its APIs must not go on looking healthy through the other.
func serveAll(ctx context.Context, servers ...served) error {
	g, ctx := errgroup.WithContext(ctx)
	for _, s := range servers {
		g.Go(func() error { return container.ServeHTTP(ctx, s.server, s.listener) })
	}
	return g.Wait()
}

type served struct {
	server   *http.Server
	listener net.Listener
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
