package smtp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiagnosticCode(t *testing.T) {
	msg := `------33D00BEF02924490820158550AD524A1
Content-type: text/plain; charset=Windows-1252

This is the mail system at host service.socketlabs.com.

I'm sorry to have to inform you that your message could not
be delivered to one or more recipients.

bounce-test@service.socketlabs.com :host service.socketlabs.com said: 550 No such recipient

------33D00BEF02924490820158550AD524A1
Content-Type: message/delivery-status
Content-Description: Delivery report

Reporting-MTA: smtp; service.socketlabs.com
Original-Recipient: rfc822; bounce-test@service.socketlabs.com
Action: failed
Status: 5.1.1
Diagnostic-Code: SMTP; 550 No such recipient

------33D00BEF02924490820158550AD524A1--`

	code, msg := parseCode(strings.NewReader(msg))
	assert.Equal(t, 550, code)
	assert.Equal(t, "SMTP; 550 No such recipient", msg)
}
