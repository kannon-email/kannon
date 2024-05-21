package sendingpool

import (
	"github.com/ludusrusso/kannon/pkg/values/email"
	"github.com/ludusrusso/kannon/pkg/values/fqdn"
	"github.com/ludusrusso/kannon/pkg/values/id"
)

type Sender interface {
	Email() email.Email
	Alias() string
}

type PoolMessage struct {
	MessageID  id.ID
	Subject    string
	Sender     Sender
	TemplateID id.ID
	Domain     fqdn.FQDN
}
