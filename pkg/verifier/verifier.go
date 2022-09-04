package verifier

import (
	"context"
	"fmt"
	"regexp"
	"time"

	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/ludusrusso/kannon/internal/publisher"
	"github.com/ludusrusso/kannon/internal/runner"
	"github.com/ludusrusso/kannon/internal/utils"
	"github.com/ludusrusso/kannon/proto/kannon/stats/types"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func NewVerifier(pm pool.SendingPoolManager, pub publisher.Publisher) *Verifier {
	return &Verifier{
		pm:  pm,
		pub: pub,
	}
}

type Verifier struct {
	pm  pool.SendingPoolManager
	pub publisher.Publisher
}

func Run(ctx context.Context) error {
	dbURL := viper.GetString("database_url")
	natsURL := viper.GetString("nats_url")

	logrus.Info("🚀 Starting dispatcher")

	db, q, err := sqlc.Conn(ctx, dbURL)
	if err != nil {
		logrus.Fatalf("cannot connect to database: %v", err)
	}
	defer db.Close()

	pm := pool.NewSendingPoolManager(q)

	nc, _, closeNats := utils.MustGetNats(natsURL)
	defer closeNats()

	v := Verifier{
		pm:  pm,
		pub: nc,
	}

	return runner.Run(ctx, v.Cycle, runner.WaitLoop(1*time.Second))
}

func (d *Verifier) Cycle(pctx context.Context) error {
	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()
	poolEmails, err := d.pm.PrepareForValidate(ctx, 100)
	if err != nil {
		return fmt.Errorf("cannot prepare emails for send: %v", err)
	}
	for _, pool := range poolEmails {
		if err := d.handlePool(ctx, pool); err != nil {
			logrus.Errorf("error handling pool email: %#v", pool)
		}
	}
	logrus.Debugf("done sending emails")
	return nil
}

func (d *Verifier) handlePool(ctx context.Context, pool sqlc.SendingPoolEmail) error {
	domain, err := utils.ExtractDomainFromMessageID(pool.MessageID)
	if err != nil {
		return err
	}
	statData := &types.Stats{
		MessageId: pool.MessageID,
		Domain:    domain,
		Email:     pool.Email,
		Timestamp: timestamppb.Now(),
	}

	if err := verifyPool(pool); err != nil {
		statData.Data = newRejectedStatData(err)
		if err := d.pm.CleanEmail(ctx, pool.MessageID, pool.Email); err != nil {
			return err
		}
		return publisher.PublishStat(d.pub, statData)
	}

	if err := d.pm.SetScheduled(ctx, pool.MessageID, pool.Email); err != nil {
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

func verifyPool(pool sqlc.SendingPoolEmail) error {
	if err := verifyEmail(pool.Email); err != nil {
		return err
	}
	return nil
}

var emailReg = regexp.MustCompile("(?:[a-z0-9!#$%&'*+/=?^_`{|}~-]+(?:\\.[a-z0-9!#$%&'*+/=?^_`{|}~-]+)*|\"(?:[\x01-\x08\x0b\x0c\x0e-\x1f\x21\x23-\x5b\x5d-\x7f]|\\[\x01-\x09\x0b\x0c\x0e-\x7f])*\")@(?:(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\\.)+[a-z0-9](?:[a-z0-9-]*[a-z0-9])?|\\[(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?|[a-z0-9-]*[a-z0-9]:(?:[\x01-\x08\x0b\x0c\x0e-\x1f\x21-\x5a\x53-\x7f]|\\[\x01-\x09\x0b\x0c\x0e-\x7f])+)\\])")

func verifyEmail(email string) error {
	if emailReg.Match([]byte(email)) {
		return nil
	}
	return ErrInvalidEmailAddress
}

var ErrInvalidEmailAddress = fmt.Errorf(" is not a valid email")
