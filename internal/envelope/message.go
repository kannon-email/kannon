package envelope

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/sirupsen/logrus"
	"gopkg.in/mail.v2"
)

type headers map[string][]string

// Attachments maps an attachment filename to a reader producing its bytes.
type Attachments map[string]io.Reader

func buildEmailID(to, messageID string) string {
	emailBase64 := base64.URLEncoding.EncodeToString([]byte(to))
	return fmt.Sprintf("<%v/%v>", emailBase64, messageID)
}

// The "bump_" prefix is the wire-format token interpreted by the Tracker
// when parsing return-path bounces; renaming it would be wire-breaking.
func buildReturnPath(to, messageID string) string {
	emailBase64 := base64.URLEncoding.EncodeToString([]byte(to))
	return fmt.Sprintf("bump_%v+%v", emailBase64, messageID)
}

func buildHeaders(subject string, sender batch.Sender, to, poolMessageID, messageID string, baseHeaders headers, customHeaders batch.Headers) headers {
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

	if len(customHeaders.To) > 0 {
		h["To"] = customHeaders.To
	}
	if len(customHeaders.Cc) > 0 {
		h["Cc"] = customHeaders.Cc
	}

	return h
}

func renderMsg(html string, headers headers, attachments Attachments) ([]byte, error) {
	msg := mail.NewMessage()

	for key, values := range headers {
		msg.SetHeader(key, values...)
	}
	msg.SetDateHeader("Date", time.Now())
	msg.SetBody("text/html", html)
	for name, r := range attachments {
		msg.AttachReader(name, r)
	}

	var buff bytes.Buffer
	if _, err := msg.WriteTo(&buff); err != nil {
		logrus.Warnf("🤢 Error writing message: %v\n", err)
		return nil, err
	}
	return buff.Bytes(), nil
}

func insertTrackLinkInHTML(html, link string) string {
	return strings.Replace(html, "</body>", fmt.Sprintf(`<img src="%s" style="display:none;"/></body>`, link), 1)
}

var regLink = regexp.MustCompile(`<a\s+(?:[^>]*?\s+)?href=["'](.+?)["']`)

func replaceLinks(html string, replace func(link string) (string, error)) (string, error) {
	matches := regLink.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		link := match[1]
		newLink, err := replace(link)
		if err != nil {
			return "", err
		}
		oldHref := match[0]
		newHref := strings.Replace(oldHref, link, newLink, 1)
		html = strings.Replace(html, oldHref, newHref, 1)
	}
	return html, nil
}
