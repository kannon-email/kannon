package mailbuilder

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/mail.v2"
	"kannon.gyozatech.dev/internal/pool"
)

func buildEmailMessageID(to string, messageId string) string {
	emailBase64 := base64.URLEncoding.EncodeToString([]byte(to))
	return fmt.Sprintf("<%v/%v>", emailBase64, messageId)
}

func buildReturnPath(to string, messageId string) string {
	emailBase64 := base64.URLEncoding.EncodeToString([]byte(to))
	return fmt.Sprintf("bump_%v-%v", emailBase64, messageId)
}

// buildHeaders for a message
func buildHeaders(subject string, sender pool.Sender, to string, poolMessageID string, messageID string, baseHeaders headers) headers {
	headers := headers(baseHeaders)
	headers["Subject"] = subject
	headers["From"] = fmt.Sprintf("%v <%v>", sender.Alias, sender.Email)
	headers["To"] = to
	headers["Message-ID"] = messageID
	headers["X-Pool-Message-ID"] = poolMessageID
	return headers
}

// renderMsg render a MsgPayload to an SMTP message
func renderMsg(html string, from, to string, headers headers) ([]byte, error) {
	msg := mail.NewMessage()

	for key, value := range headers {
		msg.SetHeader(key, value)
	}
	msg.SetDateHeader("Date", time.Now())
	msg.SetBody("text/html", html)

	var buff bytes.Buffer
	if _, err := msg.WriteTo(&buff); err != nil {
		logrus.Warnf("🤢 Error writing message: %v\n", err)
		return nil, err
	}

	return buff.Bytes(), nil
}
