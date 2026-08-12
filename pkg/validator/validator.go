package validator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/x/container"
	"golang.org/x/sync/errgroup"
)

func NewValidator(c pool.Claimer, pub publisher.Publisher) *Validator {
	return &Validator{
		claimer: c,
		pub:     pub,
	}
}

type Validator struct {
	claimer pool.Claimer
	pub     publisher.Publisher
}

func (v *Validator) log() *slog.Logger {
	return slog.With("component", "validator")
}

// New constructs the validator runnable. The validator has no configurable
// knobs today, so it does not call config.LoadSection.
func New(cnt *container.Container) container.Runnable {
	return container.Runnable{
		Name: "validator",
		Run: func(ctx context.Context) error {
			claimer := pool.NewClaimer(sqlc.NewDeliveryRepository(cnt.DB(), cnt.BackoffPolicy(), cnt.RetryWindow()))

			v := Validator{
				claimer: claimer,
				pub:     cnt.NatsPublisher(),
			}

			v.log().Info("🚀 Starting validator")

			eg, ctx := errgroup.WithContext(ctx)

			eg.Go(func() error {
				return runner.Run(ctx, v.Cycle, runner.WaitLoop(1*time.Second))
			})

			eg.Go(func() error {
				return runner.Run(ctx, v.reclaimLoop, runner.WaitLoop(pool.ReclaimInterval))
			})

			return eg.Wait()
		},
	}
}

func (d *Validator) Cycle(pctx context.Context) error {
	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	emails, err := d.claimer.ClaimForValidation(ctx, 100)
	if err != nil {
		return fmt.Errorf("cannot prepare emails for send: %w", err)
	}

	d.log().Debug(fmt.Sprintf("validating %d emails", len(emails)))

	for _, dlv := range emails {
		if err := d.handleDelivery(ctx, dlv); err != nil {
			d.log().Error("error handling delivery", "batch", dlv.BatchID(), "email", dlv.Email(), "err", err)
		}
	}
	return nil
}

func (d *Validator) handleDelivery(ctx context.Context, dlv *delivery.Delivery) error {
	event := stats.Event{
		MessageID: dlv.BatchID().String(),
		Domain:    dlv.Domain(),
		Email:     dlv.Email(),
		Timestamp: time.Now(),
	}

	if err := validateDelivery(dlv); err != nil {
		event.Outcome = stats.Rejected(err.Error())
		if err := d.claimer.Drop(ctx, dlv); err != nil {
			return err
		}
		return publisher.PublishStat(d.pub, event)
	}

	if err := d.claimer.MarkValidated(ctx, dlv); err != nil {
		return err
	}
	event.Outcome = stats.Accepted()
	return publisher.PublishStat(d.pub, event)
}

func validateDelivery(d *delivery.Delivery) error {
	if err := validateEmail(d.Email()); err != nil {
		slog.Error("invalid email", "email", d.Email(), "err", err)
		return err
	}
	return nil
}

var emailReg = regexp.MustCompile("(?:[a-z0-9!#$%&'*+/=?^_`{|}~-]+(?:\\.[a-z0-9!#$%&'*+/=?^_`{|}~-]+)*|\"(?:[\x01-\x08\x0b\x0c\x0e-\x1f\x21\x23-\x5b\x5d-\x7f]|\\[\x01-\x09\x0b\x0c\x0e-\x7f])*\")@(?:(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\\.)+[a-z0-9](?:[a-z0-9-]*[a-z0-9])?|\\[(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?|[a-z0-9-]*[a-z0-9]:(?:[\x01-\x08\x0b\x0c\x0e-\x1f\x21-\x5a\x53-\x7f]|\\[\x01-\x09\x0b\x0c\x0e-\x7f])+)\\])")

func validateEmail(email string) error {
	if strings.HasSuffix(email, "@localhost") {
		return nil
	}
	if emailReg.Match([]byte(email)) {
		return nil
	}
	return ErrInvalidEmailAddress
}

var ErrInvalidEmailAddress = errors.New(" is not a valid email")
