// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.13.0

package sqlc

import (
	"fmt"
	"time"
)

type SendingPoolStatus string

const (
	SendingPoolStatusInitializing SendingPoolStatus = "initializing"
	SendingPoolStatusSending      SendingPoolStatus = "sending"
	SendingPoolStatusSent         SendingPoolStatus = "sent"
	SendingPoolStatusScheduled    SendingPoolStatus = "scheduled"
	SendingPoolStatusError        SendingPoolStatus = "error"
)

func (e *SendingPoolStatus) Scan(src interface{}) error {
	switch s := src.(type) {
	case []byte:
		*e = SendingPoolStatus(s)
	case string:
		*e = SendingPoolStatus(s)
	default:
		return fmt.Errorf("unsupported scan type for SendingPoolStatus: %T", src)
	}
	return nil
}

type Domain struct {
	ID             int32
	Domain         string
	CreatedAt      time.Time
	Key            string
	DkimPrivateKey string
	DkimPublicKey  string
}

type Message struct {
	MessageID   string
	Subject     string
	SenderEmail string
	SenderAlias string
	TemplateID  string
	Domain      string
}

type SendingPoolEmail struct {
	ID                    int32
	Status                SendingPoolStatus
	ScheduledTime         time.Time
	OriginalScheduledTime time.Time
	SendAttemptsCnt       int32
	Email                 string
	MessageID             string
	ErrorMsg              string
	ErrorCode             int32
}

type StatsKey struct {
	ID             string
	PrivateKey     string
	PublicKey      string
	CreationTime   time.Time
	ExpirationTime time.Time
}

type Template struct {
	ID         int32
	TemplateID string
	Html       string
	Domain     string
}
