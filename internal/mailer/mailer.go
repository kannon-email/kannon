package mailer

import (
	"smtp.ludusrusso.space/internal/db"
)

// Mailer model a sistem able to send complete Email
type Mailer interface {
	Send(data db.SendingPoolEmail) error
}
