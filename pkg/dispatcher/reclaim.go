package dispatcher

import (
	"context"
	"time"

	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/pool"
)

// The Dispatcher's reclaim of the Deliveries stranded in 'sending' (CONTEXT.md,
// *Reclaim*).
//
// pool.Reclaimer is the mechanism; what belongs here is the decision to run it
// and the two things only the Dispatcher can explain — how long a claim of
// 'sending' may live, and why taking one back spends a send attempt. The
// Dispatcher reclaims that status itself because it owns every transition of it;
// the rule that keeps a reclaim inside the worker that owns the status is spelled
// out on pool.Reclaimer (ADR 0004 §"Why not the Pool").

// sendingStrandThreshold is how long a Delivery may sit claimed for dispatch
// before the Dispatcher takes it back. It is sendGuardTTL
// (pkg/smtpsender/guard.go), already reasoned there as long enough to outlast
// every redelivery window plus the replacement of a dead worker — the codebase's
// existing answer to "how long can an Envelope still be alive". Past it,
// whatever was going to happen to it has happened.
const sendingStrandThreshold = 1 * time.Hour

// reclaimer binds the shared mechanism to the one status the Dispatcher owns.
//
// A reclaim from 'sending' bumps the attempt counter (internal/db,
// reclaimTargets): the Envelope was published, so an attempt was genuinely
// spent, and the bump is what makes a condition that strands systematically
// converge on termination through the Retry Budget instead of looping for ever.
func (d *disp) reclaimer() pool.Reclaimer {
	return pool.NewReclaimer(d.claimer, d.log(), delivery.InFlightForDispatch, sendingStrandThreshold)
}

// ReclaimCycle hands every Delivery that has been claimed for dispatch for
// longer than sendingStrandThreshold back to the Pool, with its attempt counter
// bumped, and reports what it took back.
func (d *disp) ReclaimCycle(ctx context.Context) error {
	return d.reclaimer().Cycle(ctx)
}

// reclaimLoop is the loop body wired into the dispatcher's run loop.
func (d *disp) reclaimLoop(ctx context.Context) error {
	return d.reclaimer().Loop(ctx)
}
