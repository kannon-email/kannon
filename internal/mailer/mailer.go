package mailer

import (
	"kannon.gyozatech.dev/internal/db"
)

// Mailer model a sistem able to send complete Email
type Mailer interface {
	Send(data db.SendingPoolEmail) error
}
