// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.13.0

package sq

import (
	"time"
)

type Accepted struct {
	ID        int32
	Email     string
	MessageID string
	Domain    string
	Timestamp time.Time
}

type HardBounced struct {
	ID        int32
	Email     string
	MessageID string
	Domain    string
	ErrCode   int32
	ErrMsg    string
	Timestamp time.Time
}

type Open struct {
	ID        int32
	Email     string
	MessageID string
	Domain    string
	Ip        string
	UserAgent string
	Timestamp time.Time
}

type Prepared struct {
	ID             int32
	Email          string
	MessageID      string
	Domain         string
	Timestamp      time.Time
	FirstTimestamp time.Time
}
