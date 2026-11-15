package startup

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kannon-email/kannon/internal/audit"
	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/container"
)

// configureStreams configures every JetStream stream the process's components share, once and before
// any of them starts. Components only publish on these streams and create consumers on them: a stream
// updated while another component creates a consumer on it is a data race inside nats-server.
//
// Every process using a stream still configures it, so that none depends on another's boot order: a
// stat or an Envelope is published with a plain NATS publish, and one sent before its stream exists
// is silently lost.
func configureStreams(ctx context.Context, cnt *container.Container, services config.Services) error {
	if usesStatsStream(services) {
		js, err := cnt.TryNatsJetStream(ctx)
		if err != nil {
			return fmt.Errorf("cannot reach NATS: %w", err)
		}
		if services.Dispatcher.Enabled || services.Sender.Enabled {
			if err := publisher.ConfigureSendingStream(ctx, js); err != nil {
				return fmt.Errorf("cannot configure the sending stream: %w", err)
			}
		}
		if err := stats.ConfigureStream(ctx, js); err != nil {
			return fmt.Errorf("cannot configure the stats stream: %w", err)
		}
	}
	return configureAuditStream(ctx, cnt, services)
}

// usesStatsStream reports whether the process runs a component that publishes stat events or
// consumes them: every component but the API and the audit writer.
func usesStatsStream(s config.Services) bool {
	return s.Sender.Enabled || s.Dispatcher.Enabled || s.Validator.Enabled || s.Stats.Enabled ||
		s.Tracker.Enabled || s.SMTP.Enabled
}

// configureAuditStream configures the audit stream for the API, which publishes on it, and the audit
// writer, which consumes it, when the audit trail is on. A failure stops only a process running the
// writer: an audit trail Kannon cannot write must not stop the API from serving (ADR 0010), and the
// API logs the decisions it cannot publish.
func configureAuditStream(ctx context.Context, cnt *container.Container, services config.Services) error {
	if !services.API.Enabled && !services.Audit.Enabled {
		return nil
	}
	tolerate := func(err error) error {
		if services.Audit.Enabled {
			return err
		}
		slog.Error("cannot configure the audit stream; the API is serving regardless", "err", err)
		return nil
	}
	cfg, err := audit.TryLoadConfig()
	if err != nil {
		return tolerate(fmt.Errorf("cannot read the audit configuration: %w", err))
	}
	if !cfg.Enabled {
		return nil
	}
	js, err := cnt.TryNatsJetStream(ctx)
	if err != nil {
		return tolerate(fmt.Errorf("cannot reach NATS: %w", err))
	}
	if err := audit.ConfigureStream(ctx, js); err != nil {
		return tolerate(fmt.Errorf("cannot configure the audit stream: %w", err))
	}
	return nil
}
