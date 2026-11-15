// Package startup boots Kannon: it reads the config file, refuses a process that is misconfigured,
// binds every listener, builds the container, starts the components the configuration selects and
// shuts them down in order. Production and the e2e suite both start Kannon through it.
//
// Configuration is global, so a process runs one Instance at a time.
package startup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/kannon-email/kannon/internal/admintoken"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/returnpath"
	"github.com/kannon-email/kannon/internal/smtp"
	"github.com/kannon-email/kannon/pkg/api"
	kaudit "github.com/kannon-email/kannon/pkg/audit"
	"github.com/kannon-email/kannon/pkg/dispatcher"
	ksmtp "github.com/kannon-email/kannon/pkg/smtp"
	"github.com/kannon-email/kannon/pkg/smtpsender"
	"github.com/kannon-email/kannon/pkg/stats"
	"github.com/kannon-email/kannon/pkg/tracker"
	"github.com/kannon-email/kannon/pkg/validator"
	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/container"
	"github.com/kannon-email/kannon/x/metrics"
	"github.com/spf13/viper"
)

const shutdownTimeout = 30 * time.Second

// Instance is a running Kannon.
type Instance interface {
	// Addrs reports the address each listener is bound to.
	Addrs() Addrs
	// Ready blocks until every component is ready. It fails with the instance's failure when the
	// instance stops first, and with ctx's error when ctx is done first.
	Ready(ctx context.Context) error
	// Wait blocks until every component has returned, and reports the first failure.
	Wait() error
	// Close stops every component, waits for them to return, then closes the container.
	Close() error
}

// Addrs are the bound addresses of an Instance's listeners, nil for a listener it does not run.
type Addrs struct {
	API, REST, Health, Tracker, SMTP, Metrics net.Addr
}

// Option adjusts how Start assembles Kannon. None of them is a config key.
type Option func(*options)

type options struct {
	container         []container.Option
	standalone        bool
	logLevel          *slog.LevelVar
	senderConcurrency uint
}

// WithSender replaces the outbound Sender: the remote MX is simulated without making a test Sender
// deployable through config.
func WithSender(s smtp.Sender) Option {
	return func(o *options) { o.container = append(o.container, container.WithSender(s)) }
}

// WithBackoff sets the retry backoff policy (ADR 0001).
func WithBackoff(p delivery.BackoffPolicy) Option {
	return func(o *options) { o.container = append(o.container, container.WithBackoff(p)) }
}

// WithRetryWindow sets the Retry Budget.
func WithRetryWindow(w time.Duration) Option {
	return func(o *options) { o.container = append(o.container, container.WithRetryWindow(w)) }
}

// WithSenderConcurrency sets how many Envelopes the sender transmits at once.
func WithSenderConcurrency(n uint) Option {
	return func(o *options) { o.senderConcurrency = n }
}

// Standalone runs every component in one process against an embedded NATS server, whatever the
// `services` section says.
func Standalone() Option {
	return func(o *options) { o.standalone = true }
}

// WithLogLevel is the level the config file's `debug` key raises to Debug.
func WithLogLevel(l *slog.LevelVar) Option {
	return func(o *options) { o.logLevel = l }
}

// Start boots Kannon from the config file at configPath, or from $HOME/.kannon.yaml when it is
// empty. It returns once every listener is bound and every component has been started; Ready tells
// when they are all ready. A refused boot is an error and never a process exit.
func Start(ctx context.Context, configPath string, opts ...Option) (Instance, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	viper.Reset()
	config.Prepare(configPath)
	cfg, err := config.Read()
	if err != nil {
		return nil, fmt.Errorf("cannot read the configuration: %w", err)
	}
	if cfg.Debug && o.logLevel != nil {
		o.logLevel.Set(slog.LevelDebug)
	}

	services := config.AllServices()
	if o.standalone {
		cfg.UseEmbeddedNats = true
	} else if services, err = config.LoadServices(); err != nil {
		return nil, err
	}

	// Every refusal is read off the `services` section and answered before anything is built. The
	// container and every runnable read sections of their own, any of which may name a variable only
	// some other pod sets, so a mistake in what this process was asked to be has to be reported before
	// a section belonging to a component it does not even run can panic first.
	//
	// A process with nothing to run used to log "Starting Kannon runnables: []" and exit 0, which
	// reads as a clean shutdown in every dashboard there is.
	if len(services.Enabled()) == 0 {
		return nil, errors.New("this process was asked to run nothing: enable at least one component " +
			"under `services` in the config file, e.g. `services: {api: {enabled: true}}`")
	}

	// A process serving the Admin and Stats APIs without their credential would come up healthy and
	// answer every request with unauthenticated. It stops the whole boot — the workers included —
	// because a Kannon half up hides the reason the API is unusable.
	token, err := requireAdminToken(services)
	if err != nil {
		return nil, err
	}

	// The same refusal for the secret bounce return paths are signed with: a Dispatcher without it
	// cannot build an Envelope, and an inbound SMTP server without it would refuse every bounce.
	if err := requireBounceSecret(services); err != nil {
		return nil, err
	}

	sec, err := loadSections(services)
	if err != nil {
		return nil, err
	}
	if o.senderConcurrency > 0 {
		sec.sender.MaxJobs = o.senderConcurrency
	}

	l, err := listen(services, sec)
	if err != nil {
		return nil, err
	}

	cnt := container.New(context.WithoutCancel(ctx), cfg, o.container...)
	closeContainer := func() error { return cnt.CloseWithTimeout(shutdownTimeout) }

	if err := configureStreams(ctx, cnt, services); err != nil {
		for _, c := range l.all {
			_ = c.Close()
		}
		return nil, errors.Join(err, closeContainer())
	}

	inst := newInstance(l.addrs(), l.all, closeContainer)
	reg := &container.Registry{}
	if l.metrics != nil {
		reg.Register(metrics.Listener(l.metrics, cnt.Metrics()))
	}
	if services.Sender.Enabled {
		reg.Register(smtpsender.New(cnt, sec.sender))
	}
	if services.Dispatcher.Enabled {
		reg.Register(dispatcher.New(cnt))
	}
	if services.Validator.Enabled {
		reg.Register(validator.New(cnt))
	}
	if services.Stats.Enabled {
		reg.Register(stats.New(cnt))
	}
	if services.Tracker.Enabled {
		reg.Register(tracker.New(cnt, l.tracker))
	}
	if services.API.Enabled {
		reg.Register(api.New(cnt, sec.api, token, l.api, inst.ready))
	}
	if services.SMTP.Enabled {
		reg.Register(ksmtp.New(cnt, sec.smtp, l.smtp))
	}
	if services.Audit.Enabled {
		reg.Register(kaudit.New(cnt))
	}

	slog.Info(fmt.Sprintf("Starting Kannon runnables: %v", reg.Names()))

	inst.launch(ctx, reg)
	return inst, nil
}

type listeners struct {
	api                    api.Listeners
	tracker, smtp, metrics net.Listener
	all                    []io.Closer
}

func (l listeners) addrs() Addrs {
	addr := func(ln net.Listener) net.Addr {
		if ln == nil {
			return nil
		}
		return ln.Addr()
	}
	return Addrs{
		API:     addr(l.api.Connect),
		REST:    addr(l.api.REST),
		Health:  addr(l.api.Health),
		Tracker: addr(l.tracker),
		SMTP:    addr(l.smtp),
		Metrics: addr(l.metrics),
	}
}

// listen binds every listener the process runs before any component starts, so a port taken by
// another process is a boot failure and the bound addresses are known up front.
func listen(services config.Services, sec sections) (l listeners, err error) {
	defer func() {
		if err != nil {
			for _, c := range l.all {
				_ = c.Close()
			}
		}
	}()
	if sec.metrics.Enabled {
		if l.metrics, err = metrics.Listen(sec.metrics); err != nil {
			return l, fmt.Errorf("cannot bind the metrics listener: %w", err)
		}
		l.all = append(l.all, l.metrics)
	}
	if services.API.Enabled {
		if l.api, err = api.Listen(sec.api); err != nil {
			return l, fmt.Errorf("cannot bind the API listeners: %w", err)
		}
		l.all = append(l.all, l.api)
	}
	if services.Tracker.Enabled {
		if l.tracker, err = tracker.Listen(sec.tracker); err != nil {
			return l, fmt.Errorf("cannot bind the tracker listener: %w", err)
		}
		l.all = append(l.all, l.tracker)
	}
	if services.SMTP.Enabled {
		if l.smtp, err = ksmtp.Listen(sec.smtp); err != nil {
			return l, fmt.Errorf("cannot bind the inbound SMTP listener: %w", err)
		}
		l.all = append(l.all, l.smtp)
	}
	return l, nil
}

// requireAdminToken refuses a boot that would serve the Admin and Stats APIs with no credential to
// authenticate them. Gated on the API runnable rather than demanded of every process: a deployment
// running only the dispatcher serves none of those endpoints, and making it carry an admin secret
// would put the credential on hosts that have no use for it.
func requireAdminToken(services config.Services) (admintoken.Token, error) {
	if !services.API.Enabled {
		return admintoken.Token{}, nil
	}
	return api.AdminToken()
}

// requireBounceSecret refuses a boot that would sign or read bounce return paths with no secret to do
// it with. Gated on the two runnables that do, for the reason requireAdminToken is gated on the API:
// the secret belongs on no host that has no use for it.
func requireBounceSecret(services config.Services) error {
	if !services.Dispatcher.Enabled && !services.SMTP.Enabled {
		return nil
	}
	_, err := returnpath.Load()
	return err
}

// sections are the configuration sections Start reads for the components it builds, each read once.
// A section of a component the process does not run is left unread: it may name a variable only
// another pod sets.
type sections struct {
	api     api.Config
	tracker tracker.Config
	smtp    ksmtp.Config
	sender  smtpsender.Config
	metrics metrics.Config
}

// loadSections reads the sections of the enabled components, and refuses a metrics listener that
// would collide with one the process actually runs. A component it does not run opens no port, so
// cannot collide.
func loadSections(services config.Services) (sec sections, err error) {
	var ports []metrics.ListenPort
	if services.API.Enabled {
		if sec.api, err = api.LoadConfig(); err != nil {
			return sec, err
		}
		ports = append(ports, sec.api.ListenPorts()...)
	}
	if services.Tracker.Enabled {
		if sec.tracker, err = tracker.LoadConfig(); err != nil {
			return sec, err
		}
		ports = append(ports, sec.tracker.ListenPorts()...)
	}
	if services.SMTP.Enabled {
		if sec.smtp, err = ksmtp.LoadConfig(); err != nil {
			return sec, err
		}
	}
	if services.Sender.Enabled {
		if sec.sender, err = smtpsender.LoadConfig(); err != nil {
			return sec, err
		}
	}
	if sec.metrics, err = metrics.LoadConfig(); err != nil {
		return sec, err
	}
	return sec, sec.metrics.RequireOwnPort(ports)
}
