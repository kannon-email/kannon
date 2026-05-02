package validator

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/pool"
	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/proto/kannon/stats/types"
	"github.com/kannon-email/kannon/x/container"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
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

func (v *Validator) log() *logrus.Entry {
	return logrus.WithField("component", "validator")
}

func Run(ctx context.Context, cnt *container.Container) error {
	q := cnt.Queries()

	claimer := pool.NewClaimer(sqlc.NewDeliveryRepository(q))

	nc := cnt.NatsPublisher()

	v := Validator{
		claimer: claimer,
		pub:     nc,
	}

	v.log().Info("🚀 Starting validator")

	return runner.Run(ctx, v.Cycle, runner.WaitLoop(1*time.Second))
}

func (d *Validator) Cycle(pctx context.Context) error {
	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	emails, err := d.claimer.ClaimForValidation(ctx, 100)
	if err != nil {
		return fmt.Errorf("cannot prepare emails for send: %v", err)
	}

	d.log().Debugf("validating %d emails", len(emails))

	for _, dlv := range emails {
		if err := d.handleDelivery(ctx, dlv); err != nil {
			d.log().WithError(err).Errorf("error handling delivery: %v/%v", dlv.BatchID(), dlv.Email())
		}
	}
	return nil
}

func (d *Validator) handleDelivery(ctx context.Context, dlv *delivery.Delivery) error {
	statData := &types.Stats{
		MessageId: dlv.BatchID().String(),
		Domain:    dlv.Domain(),
		Email:     dlv.Email(),
		Timestamp: timestamppb.Now(),
	}

	if err := validateDelivery(dlv); err != nil {
		statData.Data = newRejectedStatData(err)
		if err := d.claimer.Drop(ctx, dlv); err != nil {
			return err
		}
		return publisher.PublishStat(d.pub, statData)
	}

	if err := d.claimer.MarkValidated(ctx, dlv); err != nil {
		return err
	}
	statData.Data = newAcceptedStatData()
	return publisher.PublishStat(d.pub, statData)
}

func newRejectedStatData(err error) *types.StatsData {
	return &types.StatsData{
		Data: &types.StatsData_Rejected{
			Rejected: &types.StatsDataRejected{
				Reason: err.Error(),
			},
		},
	}
}

func newAcceptedStatData() *types.StatsData {
	return &types.StatsData{
		Data: &types.StatsData_Accepted{
			Accepted: &types.StatsDataAccepted{},
		},
	}
}

func validateDelivery(d *delivery.Delivery) error {
	if err := validateEmail(d.Email()); err != nil {
		logrus.Errorf("invalid email %s: %v", d.Email(), err)
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

var ErrInvalidEmailAddress = fmt.Errorf(" is not a valid email")
