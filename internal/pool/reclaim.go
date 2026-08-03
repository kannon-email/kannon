package pool

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/utils"
)

// Reclaiming the Deliveries stranded in an in-flight status (CONTEXT.md,
// *Reclaim*; ADR 0007).
//
// The mechanism is shared here; the decision to run it is not. Each claiming
// worker builds a Reclaimer for the one status it owns and reclaims that status
// itself — the Dispatcher what it claimed for dispatch, the Validator what it
// claimed for validation — because a reclaim run from outside both workers would
// write to a status it does not own, which is the arrangement ADR 0004 §"Why not
// the Pool" declined to create when it kept the SMTPSender out of the Pool.
// Everything that differs between the two travels as an argument, or lives with
// the worker that can explain it.

// ReclaimInterval is how often a worker reclaims its own in-flight status. Every
// threshold is minutes or hours, so there is nothing to gain from looking more
// often.
const ReclaimInterval = 5 * time.Minute

const (
	// reclaimPageSize bounds one cycle, so a mass strand is drained over
	// several of them instead of materialising the whole backlog — and one log
	// line per row — in a single pass.
	reclaimPageSize = 1000

	// reclaimTimeout budgets the single statement a cycle issues.
	reclaimTimeout = 30 * time.Second
)

// Reclaimer recovers the Deliveries stranded in one in-flight status, on behalf
// of the worker that owns it.
//
// It is deliberately incapable of covering more than the one status it was built
// for: the threshold and the in-flight value are fixed at construction, by the
// worker, so nothing here can reclaim a status its caller does not own.
type Reclaimer struct {
	claimer   Claimer
	log       *slog.Logger
	inFlight  delivery.InFlight
	olderThan time.Duration
}

// NewReclaimer binds the shared mechanism to one worker's status and threshold.
// Whether handing a row back also spends a send attempt follows from f alone
// (internal/db, reclaimTargets), so a caller cannot pair the wrong status with
// the wrong bump.
func NewReclaimer(c Claimer, log *slog.Logger, f delivery.InFlight, olderThan time.Duration) Reclaimer {
	return Reclaimer{claimer: c, log: log, inFlight: f, olderThan: olderThan}
}

// Cycle hands every Delivery that has held a claim of this kind for longer than
// the threshold back to the Pool, and reports what it took back.
//
// It recovers; it never terminates and emits no outcome, because it genuinely
// does not know what happened: the work may have completed in full — for a
// dispatch claim, the Envelope was published, the SMTP transaction may have run
// to the end, and the mail may be in the recipient's inbox. Handing the Delivery
// back therefore risks a duplicate, and that is the same trade ADR 0004 took
// twice, since for password resets and receipts a missing email is worse than a
// second copy. Only the Retry Budget ends a Delivery (ADR 0007).
func (r Reclaimer) Cycle(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, reclaimTimeout)
	defer cancel()

	reclaimed, err := r.claimer.ReclaimStranded(ctx, r.inFlight, r.olderThan, reclaimPageSize)
	if err != nil {
		return fmt.Errorf("cannot reclaim deliveries stranded in %s: %w", r.inFlight, err)
	}
	if len(reclaimed) == 0 {
		return nil
	}

	// The condition this recovers from is invisible from outside the database,
	// which is half of why it went unnoticed for so long: every reclaim is
	// logged individually, and the cycle carries the count.
	log := r.log.With("in_flight", r.inFlight.String(), "threshold", r.olderThan)
	for _, dlv := range reclaimed {
		log.Warn("[reclaimed stranded delivery]",
			"email", utils.ObfuscateEmail(dlv.Email()),
			"batch_id", dlv.BatchID().String(),
			"send_attempts", dlv.SendAttempts())
	}
	log.Warn("reclaimed stranded deliveries", "count", len(reclaimed))

	return nil
}

// Loop is Cycle as a runner loop body. It logs the error rather than returning
// it: runner.Run stops at the first error and every runnable in the process
// shares one errgroup (x/container.Registry.Run), so a Postgres blip inside a
// best-effort recovery would take the whole of Kannon down with it. The next
// cycle is ReclaimInterval away.
func (r Reclaimer) Loop(ctx context.Context) error {
	if err := r.Cycle(ctx); err != nil {
		r.log.Error("reclaim cycle failed, stranded claims stay until the next one", "err", err)
	}
	return nil
}
