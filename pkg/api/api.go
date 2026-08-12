package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/audit"
	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/authzconnect"
	sq "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/pkg/api/adminapi"
	"github.com/kannon-email/kannon/pkg/api/hzapi"
	"github.com/kannon-email/kannon/pkg/api/mailapi"
	"github.com/kannon-email/kannon/pkg/statsapi/statsv1"
	"github.com/kannon-email/kannon/pkg/statsapi/statsv2"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	statsv1connect "github.com/kannon-email/kannon/proto/kannon/stats/apiv1/apiv1connect"
	statsv2connect "github.com/kannon-email/kannon/proto/kannon/stats/apiv2/apiv2connect"
	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/container"
)

type Config struct {
	Port uint `mapstructure:"port"`
}

func (c *Config) setDefaults() {
	if c.Port == 0 {
		c.Port = 50051
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
		"the API needs an admin token to authenticate the Admin and Stats APIs: set %q in the config file, "+
			"taking it from the environment as `admin_token: env://KANNON_ADMIN_TOKEN` if you would rather "+
			"not write it there (%w)",
		config.APIAdminTokenKey, err)
}

// New constructs the API runnable, loading its slice of configuration from
// viper under the "api" key.
func New(cnt *container.Container) container.Runnable {
	var cfg Config
	config.LoadSection("api", &cfg)
	cfg.setDefaults()
	return container.Runnable{
		Name: "api",
		Run: func(ctx context.Context) error {
			return run(ctx, cfg, cnt)
		},
	}
}

func run(ctx context.Context, config Config, cnt *container.Container) error {
	port := config.Port

	// Resolved before the listener opens, and again here rather than only in the boot path: a
	// caller registering this runnable directly — the e2e suite does — must not be able to
	// stand up an Admin API whose credential nobody checked.
	adminToken, err := AdminToken()
	if err != nil {
		return err
	}

	slog.Info(fmt.Sprintf("Starting API Service on port %d", port))

	db := cnt.DB()

	statsRepo := sq.NewStatsRepository(db)
	aggregatedRepo := sq.NewAggregatedStatsRepository(db)
	statsService := stats.NewService(statsRepo, stats.WithAggregatedStatsRepository(aggregatedRepo))

	// Started before the handlers are mounted, and unable to fail: what an audit trail costs an
	// operator who enabled it must not include the API refusing to serve.
	recorder := startAuditRecording(ctx, cnt)

	adminAPIService := adminapi.CreateAdminAPIService(db)
	mailAPIService := mailapi.NewMailerAPIV1(db, cnt.BackoffPolicy(), cnt.RetryWindow())
	statsAPIService := statsv1.NewStatsAPIService(statsService)
	statsV2APIService := statsv2.NewStatsAPIService(statsService)
	hzAPIService := hzapi.CreateHZAPIService(cnt)

	// The operator's credential is read here, once, and handed down. Nothing beneath
	// this point asks the configuration layer what authority a request has.
	adminAuth := authzconnect.AdminTokenHandlerOptions(adminToken)

	return startAPIServer(ctx, port, adminAuth, recorder, adminAPIService, mailAPIService, statsAPIService, statsV2APIService, hzAPIService)
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

// startAPIServer mounts every handler. adminAuth authenticates the three surfaces that answer to
// the operator's admin token. The mailer handler must never get it — it authenticates its own
// sender credential — and neither must health, which discloses nothing and is polled unauthenticated.
func startAPIServer(ctx context.Context, port uint, adminAuth []connect.HandlerOption, recorder authz.Recorder, adminServer adminv1connect.ApiHandler, mailerServer mailerv1connect.MailerHandler, statsServer statsv1connect.StatsApiV1Handler, statsV2Server statsv2connect.StatsApiV2Handler, hzServer adminv1connect.HZServiceHandler) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	mux := http.NewServeMux()

	adminPath, adminHandler := adminv1connect.NewApiHandler(adminServer, adminAuth...)
	mailerPath, mailerHandler := mailerv1connect.NewMailerHandler(mailerServer)
	statsPath, statsHandler := statsv1connect.NewStatsApiV1Handler(statsServer, adminAuth...)
	statsV2Path, statsV2Handler := statsv2connect.NewStatsApiV2Handler(statsV2Server, adminAuth...)
	hzPath, hzHandler := adminv1connect.NewHZServiceHandler(hzServer)

	mux.Handle(adminPath, adminHandler)
	mux.Handle(mailerPath, mailerHandler)
	mux.Handle(statsPath, statsHandler)
	mux.Handle(statsV2Path, statsV2Handler)
	mux.Handle(hzPath, hzHandler)

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	server := &http.Server{Addr: addr, Handler: withRecorder(recorder, mux), Protocols: protocols}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("error shutting down server", "err", err)
		}
	}()

	slog.Info("Connect API server listening on " + addr)
	return server.ListenAndServe()
}
