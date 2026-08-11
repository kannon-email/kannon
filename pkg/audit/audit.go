// Package audit runs the writer of the authorization register: one JetStream consumer turning the
// decisions a Guard published into rows, and beside it the sweep that enforces the retention the
// operator asked for (ADR 0010). Selected by `services.audit.enabled`, like every other Kannon component.
//
// It is a runnable of its own rather than two more goroutines inside stats, which already consumes
// events and persists them and which every deployment runs. Folding it in would have needed no
// deployment change at all, and would have left one runnable covering two domains under a name that
// says one, reading two retentions out of two configuration namespaces.
package audit

import (
	"context"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/kannon-email/kannon/internal/audit"
	sq "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/kannon-email/kannon/x/container"

	"github.com/nats-io/nats.go/jetstream"
)

// durableName is the consumer this worker reads the audit stream under. Durable, so a worker that
// restarts resumes where it stopped instead of replaying seven days of decisions or skipping the
// ones published while it was down.
const durableName = "kannon-audit-writer"

// cleanupInterval is how often the retention sweep runs. Hourly and not per-minute because a Record
// held an hour past its expiry is an hour of a month-long retention, and because each run is a
// DELETE over an indexed range that a shorter interval would only repeat emptier.
const cleanupInterval = time.Hour

type auditHandler struct {
	js        jetstream.JetStream
	repo      audit.Repository
	retention time.Duration
}

// New constructs the audit runnable. The configuration is read through internal/audit, as the
// producer's is, so the two processes cannot come to disagree about whether the feature is on or about
// how long a Record lives.
func New(cnt *container.Container) container.Runnable {
	cfg := audit.LoadConfig()
	return container.Runnable{
		Name: "audit",
		Run: func(ctx context.Context) error {
			return run(ctx, cnt, cfg)
		},
	}
}

func run(ctx context.Context, cnt *container.Container, cfg audit.Config) error {
	// audit.enabled governs the producer, so with it unset nothing is ever published and this
	// worker would sit against an empty stream forever, holding a database connection and a
	// consumer that record nothing. Refusing to start is what that means here: it stops, and the
	// operator is told which half of the configuration is missing rather than left with a process
	// that looks healthy and collects nothing.
	//
	// nil and not an error, deliberately. container.Registry.Run puts every runnable in one
	// errgroup, so an error here would take the whole process down — the API with it — over a
	// feature that is off. And `kannon standalone` turns on every flag while audit.enabled stays
	// false by default, so an error would make standalone refuse to boot for something nobody asked
	// for.
	if !cfg.Enabled {
		slog.Warn("the audit writer has nothing to consume: authorization decisions are only "+
			"published when audit.enabled is set, so this worker will not start",
			"key", "audit.enabled")
		return nil
	}

	js := cnt.NatsJetStream()

	// Configured here as well as by the API. CreateOrUpdateStream is idempotent and neither process
	// may depend on the other's boot order: the producer must not publish into a stream that does
	// not exist, and this worker must not require the API to have come up first.
	if err := audit.ConfigureStream(ctx, js); err != nil {
		return err
	}

	h := auditHandler{
		js:        js,
		repo:      sq.NewAuditRepository(cnt.DB()),
		retention: cfg.Retention,
	}

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return h.handleRecords(ctx)
	})

	eg.Go(func() error {
		return runner.Run(ctx, h.cleanupCycle, runner.WaitLoop(cleanupInterval))
	})

	return eg.Wait()
}

// handleRecords consumes kannon.audit.* — both outcomes on one consumer. The outcome is in the
// subject so an operator can alert on refusals alone; nothing here needs to tell them apart, since
// every decision gets the same row.
func (h *auditHandler) handleRecords(ctx context.Context) error {
	con := utils.MustGetPullSubscriber(ctx, h.js, audit.StreamName, audit.StreamSubjects, durableName)
	c, err := con.Consume(func(msg jetstream.Msg) {
		if err := h.handleRecordMsg(ctx, msg); err != nil {
			slog.Error("Cannot handle audit record msg", "err", err)
		}
	})
	if err != nil {
		return err
	}

	defer c.Drain()

	<-ctx.Done()
	return nil
}

// handleRecordMsg writes one published decision down. The three settlements are the whole of the
// policy: a payload that cannot be read is abandoned, a database that cannot be written to is asked
// again, and everything else is finished with.
func (h *auditHandler) handleRecordMsg(ctx context.Context, msg jetstream.Msg) error {
	record, err := audit.Unmarshal(msg.Data())
	if err != nil {
		// Termed and not Naked: a payload does not become parseable on redelivery, so asking for
		// it again buys the #396 hot loop and nothing else. The decision is not lost in silence —
		// the producer logged it through the Recorder this one decorates.
		slog.Error("cannot read an Audit Record off the stream, abandoning it", "err", err)
		return msg.Term()
	}

	// A transient database failure should cost a delay and not a Record, which is why this is the
	// one settlement that comes back. Insert is a no-op for an identifier already stored, so a
	// redelivery — after this Nak, or after a crash between the write and the acknowledgement —
	// costs a statement rather than one decision written down twice.
	if err := h.repo.Insert(ctx, record); err != nil {
		slog.Error("cannot insert an Audit Record", "id", record.ID, "err", err)
		return msg.Nak()
	}

	return msg.Ack()
}

// cleanupCycle enforces the operator's retention: one DELETE over what has expired. Logged only when
// it deleted something, so the line is a fact about the register rather than an hourly heartbeat in
// the logs of every deployment that has the feature on.
//
// The statement is idempotent and each run takes only what has fallen out of the window, so replicas
// of this worker are harmless: whichever gets there first does the work and the others find nothing.
//
// A failed sweep is logged and not returned, which is where this parts company with pkg/stats. An
// error out of the loop would reach the errgroup and take the whole process down, and this worker
// shares that process with the API whenever an operator co-locates them — as standalone and the
// Kubernetes manifest both do. A register Kannon could not prune for an hour must not become an
// interruption of service for somebody's customers; the next run takes what this one left.
func (h *auditHandler) cleanupCycle(ctx context.Context) error {
	deleted, err := h.repo.DeleteOlderThan(ctx, time.Now().Add(-h.retention))
	if err != nil {
		slog.Error("cannot delete expired Audit Records; the next sweep will take them",
			"retention", h.retention, "err", err)
		return nil
	}

	if deleted > 0 {
		slog.Info("audit cleanup: deleted expired Audit Records",
			"deleted", deleted, "retention", h.retention)
	}

	return nil
}
