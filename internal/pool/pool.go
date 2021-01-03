package pool

import (
	"fmt"
	"time"

	"github.com/jinzhu/gorm/dialects/postgres"
	"gorm.io/gorm"
	"smtp.ludusrusso.space/internal/db"
)

// SendingPoolManager is a manger for sending pool
type SendingPoolManager interface {
	AddPool(
		template db.Template,
		to []string,
		from db.Sender,
		subject string,
		domain string,
	) (db.SendingPool, error)
	PrepareForSend(max uint) ([]db.SendingPoolEmail, error)
}

type sendingPoolManager struct {
	db *gorm.DB
}

// AddPool starts a new schedule in the pool
func (m *sendingPoolManager) AddPool(
	template db.Template,
	to []string,
	from db.Sender,
	subject string,
	domain string,
) (db.SendingPool, error) {
	sendingPool := db.SendingPool{
		Domain:     domain,
		Subject:    subject,
		Sender:     db.Sender(from),
		TemplateID: template.TemplateID,
	}

	var poolEmails []db.SendingPoolEmail

	for _, email := range to {
		newPool := db.SendingPoolEmail{
			Status:                db.SendingPoolStatusScheduled,
			ScheduledTime:         time.Now(),
			OriginalScheduledTime: time.Now(),
			Trial:                 0,
			To:                    email,
			Data:                  postgres.Jsonb{},
		}
		poolEmails = append(poolEmails, newPool)
	}
	sendingPool.Emails = poolEmails

	err := m.db.Create(&sendingPool).Error
	return sendingPool, err
}

func (m *sendingPoolManager) PrepareForSend(
	max uint,
) ([]db.SendingPoolEmail, error) {
	emails := []db.SendingPoolEmail{}

	err := m.db.Transaction(func(tx *gorm.DB) error {
		err := m.db.Limit(int(max)).Find(&emails, "scheduled_time <= ? AND status = ?", time.Now(), db.SendingPoolStatusScheduled).Error
		if err != nil {
			return fmt.Errorf("Cannot find Emails%v", err)
		}
		if len(emails) == 0 {
			return nil
		}

		err = m.db.Model(&emails).
			UpdateColumn("status", db.SendingPoolStatusSending).Error

		if err != nil {
			return fmt.Errorf("Cannot set Emails in SendingPoolStatusSending %v", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return emails, nil
}

// NewSendingPoolManager constructs a new Sending Pool Manager
func NewSendingPoolManager(db *gorm.DB) (SendingPoolManager, error) {
	return &sendingPoolManager{
		db: db,
	}, nil
}
