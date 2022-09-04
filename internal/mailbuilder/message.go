package mailbuilder

import (
	"encoding/base64"
	"fmt"
	"regexp"

	"github.com/ludusrusso/kannon/internal/pool"
	"github.com/sirupsen/logrus"
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
	h["Reply-To"] = fmt.Sprintf("%v <%v>", sender.Alias, sender.Email)
	return h
}

func replaceCustomFields(str string, fields map[string]string) (string, error) {
	logrus.Infof("replaceCustomFields: %v", fields)
	for key, value := range fields {
		regExp := fmt.Sprintf(`\{\{ *%s *\}\}`, key)
		fmt.Printf("\n\n%v\n\n", regExp)
		logrus.Infof("replace custom field %s with %s", regExp, value)
		reg, err := regexp.Compile(regExp)
		if err != nil {
			return "", err
		}
		str = reg.ReplaceAllString(str, value)
	}
	return str, nil
}
