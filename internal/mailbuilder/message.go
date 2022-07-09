package mailbuilder

import (
	"encoding/base64"
	"fmt"

	"github.com/ludusrusso/kannon/internal/pool"
)

func buildEmailMessageID(to string, messageID string) string {
	emailBase64 := base64.URLEncoding.EncodeToString([]byte(to))
	return fmt.Sprintf("<%v/%v>", emailBase64, messageID)
}

func buildReturnPath(to string, messageID string) string {
	emailBase64 := base64.URLEncoding.EncodeToString([]byte(to))
	return fmt.Sprintf("bump_%v-%v", emailBase64, messageID)
}

// buildHeaders for a message
func buildHeaders(subject string, sender pool.Sender, to string, poolMessageID string, messageID string, baseHeaders headers) headers {
	h := make(headers)
	for k, v := range baseHeaders {
		h[k] = v
	}
	h["Subject"] = subject
	h["From"] = fmt.Sprintf("%v <%v>", sender.Alias, sender.Email)
	h["To"] = to
	h["Message-ID"] = messageID
	h["X-Pool-Message-ID"] = poolMessageID
	return h
}
