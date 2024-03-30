package sendingpool

import (
	"time"

	"github.com/ludusrusso/kannon/pkg/values/email"
	"github.com/ludusrusso/kannon/pkg/values/id"
)

type CustomFields map[string]string

type SendingPoolEmailStatus string

const (
	SendingPoolEmailStatusInitializing SendingPoolEmailStatus = "initializing"
	SendingPoolEmailStatusToValidate   SendingPoolEmailStatus = "to_validate"
	SendingPoolEmailStatusValidating   SendingPoolEmailStatus = "validating"
	SendingPoolEmailStatusSending      SendingPoolEmailStatus = "sending"
	SendingPoolEmailStatusSent         SendingPoolEmailStatus = "sent"
	SendingPoolEmailStatusScheduled    SendingPoolEmailStatus = "scheduled"
	SendingPoolEmailStatusError        SendingPoolEmailStatus = "error"
)

type PoolEmail struct {
	ID                    int32
	ScheduledTime         time.Time
	OriginalScheduledTime time.Time
	SendAttemptsCnt       int32
	Email                 email.Email
	MessageID             id.ID
	Fields                CustomFields
	Status                SendingPoolEmailStatus
	CreatedAt             time.Time
	Domain                string
}
