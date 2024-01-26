// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.22.0

package sqlc

import (
	"database/sql/driver"
	"fmt"
	"time"
)

type TemplateType string

const (
	TemplateTypeTransient TemplateType = "transient"
	TemplateTypeTemplate  TemplateType = "template"
)

func (e *TemplateType) Scan(src interface{}) error {
	switch s := src.(type) {
	case []byte:
		*e = TemplateType(s)
	case string:
		*e = TemplateType(s)
	default:
		return fmt.Errorf("unsupported scan type for TemplateType: %T", src)
	}
	return nil
}

type NullTemplateType struct {
	TemplateType TemplateType
	Valid        bool // Valid is true if TemplateType is not NULL
}

// Scan implements the Scanner interface.
func (ns *NullTemplateType) Scan(value interface{}) error {
	if value == nil {
		ns.TemplateType, ns.Valid = "", false
		return nil
	}
	ns.Valid = true
	return ns.TemplateType.Scan(value)
}

// Value implements the driver Valuer interface.
func (ns NullTemplateType) Value() (driver.Value, error) {
	if !ns.Valid {
		return nil, nil
	}
	return string(ns.TemplateType), nil
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
	ScheduledTime         time.Time
	OriginalScheduledTime time.Time
	SendAttemptsCnt       int32
	Email                 string
	MessageID             string
	Fields                CustomFields
	Status                SendingPoolStatus
	CreatedAt             time.Time
	Domain                string
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
	Type       TemplateType
	Title      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
