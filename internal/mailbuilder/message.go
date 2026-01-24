package mailbuilder

import (
	"encoding/base64"
	"fmt"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/pool"
)

func buildEmailID(to string, messageID string) string {
	emailBase64 := base64.URLEncoding.EncodeToString([]byte(to))
	return fmt.Sprintf("<%v/%v>", emailBase64, messageID)
}

func buildReturnPath(to string, messageID string) string {
	emailBase64 := base64.URLEncoding.EncodeToString([]byte(to))
	return fmt.Sprintf("bump_%v+%v", emailBase64, messageID)
}

// buildHeaders for a message
func buildHeaders(subject string, sender pool.Sender, to string, poolMessageID string, messageID string, baseHeaders headers, customHeaders sqlc.Headers) headers {
	h := make(headers)
	for k, v := range baseHeaders {
		h[k] = make([]string, len(v))
		copy(h[k], v)
	}
	h["Subject"] = []string{subject}
	h["From"] = []string{fmt.Sprintf("%v <%v>", sender.Alias, sender.Email)}
	h["Message-ID"] = []string{messageID}
	h["X-Pool-Message-ID"] = []string{poolMessageID}
	h["Reply-To"] = []string{fmt.Sprintf("%v <%v>", sender.Alias, sender.Email)}
	h["To"] = []string{to}
	h["Cc"] = []string{}

	if len(customHeaders.To) > 0 {
		h["To"] = customHeaders.To
	}

	if len(customHeaders.Cc) > 0 {
		h["Cc"] = customHeaders.Cc
	}

	return h
}
