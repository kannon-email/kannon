package envelope

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"
	"github.com/kannon-email/kannon/internal/batch"
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

func renderMsg(html string, hdrs headers, attachments Attachments) ([]byte, error) {
	var h mail.Header
	for key, values := range hdrs {
		h.Set(key, strings.Join(values, ", "))
	}
	h.SetDate(time.Now())

	var buf bytes.Buffer
	if err := writeMessage(&buf, h, html, attachments); err != nil {
		slog.Warn(fmt.Sprintf("🤢 Error writing message: %v\n", err))
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeMessage(buf *bytes.Buffer, h mail.Header, html string, attachments Attachments) error {
	if len(attachments) == 0 {
		h.Set("Content-Type", "text/html; charset=utf-8")
		w, err := mail.CreateSingleInlineWriter(buf, h)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(w, html); err != nil {
			return err
		}
		return w.Close()
	}

	mw, err := mail.CreateWriter(buf, h)
	if err != nil {
		return err
	}

	var ih mail.InlineHeader
	ih.SetContentType("text/html", map[string]string{"charset": "utf-8"})
	bw, err := mw.CreateSingleInline(ih)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(bw, html); err != nil {
		return err
	}
	if err := bw.Close(); err != nil {
		return err
	}

	for name, r := range attachments {
		var ah mail.AttachmentHeader
		ah.SetFilename(name)
		aw, err := mw.CreateAttachment(ah)
		if err != nil {
			return err
		}
		if _, err := io.Copy(aw, r); err != nil {
			return err
		}
		if err := aw.Close(); err != nil {
			return err
		}
	}

	return mw.Close()
}

// regBodyClose matches the closing </body> tag. Tag names are case-insensitive in
// HTML and whitespace is allowed before the '>', so </BODY> and </body > close the
// same body.
var regBodyClose = regexp.MustCompile(`(?i)</body\s*>`)

// insertTrackLinkInHTML puts the open pixel at the end of the body, immediately
// before the closing tag, which is left exactly as its author wrote it.
//
// An HTML fragment with no closing tag at all is returned unchanged: there is no
// end of body to place the pixel at, so such a message carries no open pixel.
func insertTrackLinkInHTML(html, link string) string {
	at := regBodyClose.FindStringIndex(html)
	if at == nil {
		return html
	}
	pixel := fmt.Sprintf(`<img src="%s" style="display:none;"/>`, link)
	return html[:at[0]] + pixel + html[at[0]:]
}

var (
	// regATag matches an opening <a> tag as a whole. The tag — not the bare href —
	// is the unit of work, because whether a link is tracked depends on the other
	// attributes it carries. A quoted attribute value may hold a raw '>', so
	// quoted spans are matched as units instead of scanning for the first '>'.
	regATag = regexp.MustCompile(`(?i)<a\s(?:[^>"']|"[^"]*"|'[^']*')*>`)
	// regHref captures the href value of a tag. Attribute names are case-insensitive
	// in HTML, so HREF is the same attribute as href. The leading whitespace keeps a
	// look-alike attribute such as data-href out of the match.
	regHref = regexp.MustCompile(`(?i)\shref=["'](.+?)["']`)
	// regNoTrack matches the data-no-track opt-out attribute in every spelling a
	// sender may reach for — valueless, quoted, unquoted, any case — together with
	// the character that terminates it, which the replacement puts back.
	regNoTrack = regexp.MustCompile(`(?i)\s+data-no-track(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]*))?([\s/>]|$)`)
)

// nonTrackableSchemes are the href schemes a click redirect cannot serve: the
// Tracker answers a /c/ hit with an HTTP redirect, and a Location pointing at a
// mailto:/tel:/sms: URI is not something a mail client will follow. Rewriting
// such an href breaks the link outright, so it is left alone.
var nonTrackableSchemes = []string{"mailto:", "tel:", "sms:"}

// replaceLinks routes every trackable href in the HTML through replace, which
// mints the click-tracking redirect for it.
//
// Two kinds of link are handed back untouched: one whose scheme no redirect can
// serve (see nonTrackableSchemes, plus in-page anchors), and one whose <a> tag
// opts out with data-no-track. The opt-out is a sender-side decision — it exists
// for unsubscribe and preference links, where recording the click is not
// something to do silently — and it costs no token, because replace is never
// called for a link that is skipped.
//
// Removing the attribute afterwards is not this function's job: stripNoTrackAttrs
// does that for every Delivery, tracked or not.
func replaceLinks(html string, replace func(link string) (string, error)) (string, error) {
	var out strings.Builder
	last := 0
	for _, span := range regATag.FindAllStringIndex(html, -1) {
		tag, err := rewriteATag(html[span[0]:span[1]], replace)
		if err != nil {
			return "", err
		}
		out.WriteString(html[last:span[0]])
		out.WriteString(tag)
		last = span[1]
	}
	out.WriteString(html[last:])
	return out.String(), nil
}

// rewriteATag applies the rewrite to a single opening <a> tag.
func rewriteATag(tag string, replace func(link string) (string, error)) (string, error) {
	if regNoTrack.MatchString(tag) {
		return tag, nil
	}

	href := regHref.FindStringSubmatchIndex(tag)
	if href == nil {
		return tag, nil
	}
	// Indices 2 and 3 delimit the href value, so the rewrite lands on the value
	// alone even when the same string appears elsewhere in the tag.
	link := tag[href[2]:href[3]]
	if !isTrackableLink(link) {
		return tag, nil
	}

	newLink, err := replace(link)
	if err != nil {
		return "", err
	}
	return tag[:href[2]] + newLink + tag[href[3]:], nil
}

// stripNoTrackAttrs removes the opt-out attribute from every <a> tag in the
// HTML. The attribute is an instruction to Kannon rather than content, so it is
// dropped whatever the Tracking Policy says — including under a links Mode that
// rewrites nothing, where replaceLinks never runs at all.
//
// Stripping stays scoped to <a> tags: the same string in body text, or inside an
// href, is content and is delivered as written.
func stripNoTrackAttrs(html string) string {
	return regATag.ReplaceAllStringFunc(html, stripNoTrackFromTag)
}

// stripNoTrackFromTag drops the attribute from one tag. It repeats until the tag
// is clean because each match consumes the character that terminates the
// attribute, which would otherwise hide a second copy of it.
func stripNoTrackFromTag(tag string) string {
	for regNoTrack.MatchString(tag) {
		tag = regNoTrack.ReplaceAllString(tag, "${1}")
	}
	return tag
}

// isTrackableLink reports whether a click redirect could serve this href at all.
func isTrackableLink(link string) bool {
	link = strings.TrimSpace(link)
	if link == "" || strings.HasPrefix(link, "#") {
		return false
	}
	lower := strings.ToLower(link)
	for _, scheme := range nonTrackableSchemes {
		if strings.HasPrefix(lower, scheme) {
			return false
		}
	}
	return true
}
