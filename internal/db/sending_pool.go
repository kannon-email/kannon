package db

import (
	"fmt"
	"time"

	psql "github.com/jinzhu/gorm/dialects/postgres"
	"gopkg.in/lucsky/cuid.v1"
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
	return fmt.Sprintf("%v <%v>", s.Alias, s.Email)
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
	sp.MessageID = fmt.Sprintf("message-%v@%v", cuid.New(), sp.Domain)
	return nil
}
