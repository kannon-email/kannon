package db

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	psql "github.com/jinzhu/gorm/dialects/postgres"
	"gorm.io/gorm"
)

// SendingPoolStatus is the status of a sending pool
type SendingPoolStatus string

const (
	// SendingPoolStatusInitializing initializing status
	SendingPoolStatusInitializing SendingPoolStatus = "initializing"

	// SendingPoolStatusSending sending
	SendingPoolStatusSending SendingPoolStatus = "sending"

	// SendingPoolStatusSent sent email
	SendingPoolStatusSent SendingPoolStatus = "sent"

	// SendingPoolStatusScheduled scheduled
	SendingPoolStatusScheduled SendingPoolStatus = "scheduled"

	// SendingPoolStatusError error
	SendingPoolStatusError SendingPoolStatus = "error"
)

// SendingPoolEmail represent a sending pool entry
type SendingPoolEmail struct {
	ID                    uint              `gorm:"primarykey"`
	Status                SendingPoolStatus `gorm:"index,default:initializing"`
	ScheduledTime         time.Time         `gorm:"default:now()"`
	OriginalScheduledTime time.Time         `gorm:"index,default:now()"`
	Trial                 uint8             `gorm:"default:0"`
	To                    string
	Data                  psql.Jsonb
	SendingPoolID         uint
	Error                 string
}

// Sender struct
type Sender struct {
	Email string
	Alias string
}

// GetSender retuns Sender in form of Email From Header
func (s Sender) GetSender() string {
	return fmt.Sprintf("%v <%v>", s.Email, s.Alias)
}

// SendingPool represents a group of sending emails with associated Template
type SendingPool struct {
	ID         uint   `gorm:"primarykey"`
	MessageID  string `gorm:"index"`
	Subject    string
	Emails     []SendingPoolEmail
	Sender     Sender `gorm:"embedded;embeddedPrefix:sender_"`
	TemplateID string
	Domain     string
}

// BeforeCreate hooks build UID of the messageID
func (sp *SendingPool) BeforeCreate(tx *gorm.DB) error {
	sp.MessageID = fmt.Sprintf("message/%v@%v", uuid.New().String(), sp.Domain)
	return nil
}
