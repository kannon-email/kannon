package validator

import (
	"context"
	"time"

	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/pool"
)

// The Validator's reclaim of the Deliveries stranded in 'validating' (CONTEXT.md,
// *Reclaim*).
//
// pool.Reclaimer is the mechanism; what belongs here is the decision to run it
// and the two things only the Validator can explain — how long a claim of
// 'validating' may live, and why taking one back must not spend a send attempt.
// The Validator reclaims that status itself for the same reason the Dispatcher
// reclaims its own: nothing else writes 'validating'. The rule is spelled out on
// pool.Reclaimer (ADR 0004 §"Why not the Pool", ADR 0007).

// validatingStrandThreshold is how long a Delivery may sit claimed for
// validation before the Validator takes it back. The whole validation cycle is
// bounded at ten seconds, so a row a few minutes old is stuck — two orders of
// magnitude below the dispatch threshold, because so is the plausible time in
// flight.
const validatingStrandThreshold = 5 * time.Minute

// reclaimer binds the shared mechanism to the one status the Validator owns.
//
// A reclaim from 'validating' leaves the attempt counter alone (internal/db,
// reclaimTargets): it is not a send attempt, and bumping it would silently
// advance the backoff curve of a Delivery that has never been near an MX. A row
// that permanently strands here has no per-row cause — the Validator either
// passes an address or Drops it as Rejected — so the cause is always
// infrastructural, always transient, and looping is the correct behaviour
// (ADR 0007).
func (v *Validator) reclaimer() pool.Reclaimer {
	return pool.NewReclaimer(v.claimer, v.log(), delivery.InFlightForValidation, validatingStrandThreshold)
}

// ReclaimCycle hands every Delivery that has been claimed for validation for
// longer than validatingStrandThreshold back to the Pool, and reports what it
// took back.
func (v *Validator) ReclaimCycle(ctx context.Context) error {
	return v.reclaimer().Cycle(ctx)
}

// reclaimLoop is the loop body wired into the validator's run loop.
func (v *Validator) reclaimLoop(ctx context.Context) error {
	return v.reclaimer().Loop(ctx)
}
