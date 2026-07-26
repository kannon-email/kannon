package envelope

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/batch"
	"github.com/stretchr/testify/assert"
)

func TestBuildHeaders(t *testing.T) {
	sender := batch.Sender{Email: "from@email.com", Alias: "email"}

	baseHeaders := headers{"testH": {"testH"}}
	h := buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", baseHeaders, batch.Headers{})

	assert.Equal(t, "testH", h["testH"][0])
	assert.Equal(t, "test subject", h["Subject"][0])
	assert.Equal(t, fmt.Sprintf("%v <%v>", sender.Alias, sender.Email), h["From"][0])
	assert.Equal(t, "to@email.com", h["To"][0])
}

func TestBuildHeadersShouldCopyBaseHeader(t *testing.T) {
	baseHeaders := headers{"testH": {"testH"}}
	sender := batch.Sender{Email: "from@email.com", Alias: "email"}

	buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", baseHeaders, batch.Headers{})
	assert.Equal(t, 1, len(baseHeaders))
}

func TestBuildHeadersCustomTo(t *testing.T) {
	sender := batch.Sender{Email: "from@email.com", Alias: "email"}
	ch := batch.Headers{To: []string{"visible@example.com"}}
	h := buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", headers{}, ch)

	assert.Equal(t, []string{"visible@example.com"}, h["To"])
	_, hasCC := h["Cc"]
	assert.False(t, hasCC)
}

func TestBuildHeadersWithCC(t *testing.T) {
	sender := batch.Sender{Email: "from@email.com", Alias: "email"}
	ch := batch.Headers{Cc: []string{"cc1@example.com", "cc2@example.com"}}
	h := buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", headers{}, ch)

	assert.Equal(t, []string{"to@email.com"}, h["To"])
	assert.Equal(t, []string{"cc1@example.com", "cc2@example.com"}, h["Cc"])
}

func TestBuildHeadersBothToAndCC(t *testing.T) {
	sender := batch.Sender{Email: "from@email.com", Alias: "email"}
	ch := batch.Headers{
		To: []string{"visible@example.com", "visible2@example.com"},
		Cc: []string{"cc@example.com"},
	}
	h := buildHeaders("test subject", sender, "to@email.com", "132@email.com", "<msg-123@email.com>", headers{}, ch)

	assert.Equal(t, []string{"visible@example.com", "visible2@example.com"}, h["To"])
	assert.Equal(t, []string{"cc@example.com"}, h["Cc"])
}

func TestInsertTrackOpen(t *testing.T) {
	html := `<html><body></body></html>`
	expected := `<html><body><img src="https://test.com/o/xxx" style="display:none;"/></body></html>`
	assert.Equal(t, expected, insertTrackLinkInHTML(html, "https://test.com/o/xxx"))
}

func TestInsertTrackLink(t *testing.T) {
	html := `<html>
<body>
<a href="http://link1.com" />
<a href="http://link2.com" />
<img src="https://test.com/o/xxx" style="display:none;"/>
</body></html>`

	expectedhtml := `<html>
<body>
<a href="http://link1.comx" />
<a href="http://link2.comx" />
<img src="https://test.com/o/xxx" style="display:none;"/>
</body></html>`

	res, err := replaceLinks(html, func(link string) (string, error) {
		return link + "x", nil
	})
	assert.Nil(t, err)
	assert.Equal(t, expectedhtml, res)
}

func TestEmptyAHrefLink(t *testing.T) {
	html := `<html>
<body>
<a href="" />
</body></html>`

	res, err := replaceLinks(html, func(link string) (string, error) {
		return link + "x", nil
	})
	assert.Nil(t, err)
	assert.Equal(t, html, res)
}

func sampleHeaders() headers {
	return headers{
		"Subject":           {"Hello World"},
		"From":              {"Test <noreply@test.com>"},
		"To":                {"rcpt@example.com"},
		"Reply-To":          {"Test <noreply@test.com>"},
		"Message-ID":        {"<msg-id@test.com>"},
		"X-Pool-Message-ID": {"pool-msg-1"},
		"X-Mailer":          {"SMTP Mailer"},
	}
}

// decodePartBody decodes a MIME part body using the
// Content-Transfer-Encoding header (defaults to identity).
func decodePartBody(t *testing.T, encoding string, body []byte) []byte {
	t.Helper()
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "7bit", "8bit", "binary":
		return body
	case "quoted-printable":
		out, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
		assert.Nil(t, err)
		return out
	case "base64":
		out, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(body)))
		assert.Nil(t, err)
		return out
	default:
		t.Fatalf("unknown Content-Transfer-Encoding: %q", encoding)
		return nil
	}
}

func TestRenderMsgPreservesHeadersAndBody(t *testing.T) {
	html := `<html><body><p>hi &amp; bye</p></body></html>`
	out, err := renderMsg(html, sampleHeaders(), nil)
	assert.Nil(t, err)

	parsed, err := mail.ReadMessage(bytes.NewReader(out))
	assert.Nil(t, err)

	assert.Equal(t, "Hello World", parsed.Header.Get("Subject"))
	assert.Equal(t, "Test <noreply@test.com>", parsed.Header.Get("From"))
	assert.Equal(t, "rcpt@example.com", parsed.Header.Get("To"))
	assert.Equal(t, "Test <noreply@test.com>", parsed.Header.Get("Reply-To"))
	assert.Equal(t, "<msg-id@test.com>", parsed.Header.Get("Message-ID"))
	assert.Equal(t, "pool-msg-1", parsed.Header.Get("X-Pool-Message-ID"))
	assert.Equal(t, "SMTP Mailer", parsed.Header.Get("X-Mailer"))
	assert.Equal(t, "1.0", parsed.Header.Get("MIME-Version"))

	date, err := mail.ParseDate(parsed.Header.Get("Date"))
	assert.Nil(t, err)
	assert.WithinDuration(t, time.Now(), date, time.Minute)

	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	assert.Nil(t, err)
	assert.Equal(t, "text/html", mediaType)
	assert.Equal(t, "utf-8", strings.ToLower(params["charset"]))

	body, err := io.ReadAll(parsed.Body)
	assert.Nil(t, err)
	decoded := decodePartBody(t, parsed.Header.Get("Content-Transfer-Encoding"), body)
	assert.Equal(t, html, string(decoded))
}

func TestRenderMsgWithSingleAttachment(t *testing.T) {
	html := `<html><body>hi</body></html>`
	atts := Attachments{
		"file.txt": strings.NewReader("hello world"),
	}
	out, err := renderMsg(html, sampleHeaders(), atts)
	assert.Nil(t, err)

	parsed, err := mail.ReadMessage(bytes.NewReader(out))
	assert.Nil(t, err)

	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	assert.Nil(t, err)
	assert.Equal(t, "multipart/mixed", mediaType)
	boundary := params["boundary"]
	assert.NotEmpty(t, boundary)

	mr := multipart.NewReader(parsed.Body, boundary)

	var foundHTML bool
	gotAttachments := map[string][]byte{}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		assert.Nil(t, err)

		ct := part.Header.Get("Content-Type")
		//nolint:errcheck // attachment parts may omit Content-Type
		mt, _, _ := mime.ParseMediaType(ct)
		raw, err := io.ReadAll(part)
		assert.Nil(t, err)
		decoded := decodePartBody(t, part.Header.Get("Content-Transfer-Encoding"), raw)

		if mt == "text/html" {
			foundHTML = true
			assert.Equal(t, html, string(decoded))
			continue
		}

		// Attachment part
		_, dispParams, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		assert.Nil(t, err)
		name := dispParams["filename"]
		assert.NotEmpty(t, name)
		gotAttachments[name] = decoded
	}

	assert.True(t, foundHTML, "missing text/html part")
	assert.Equal(t, []byte("hello world"), gotAttachments["file.txt"])
}

func TestRenderMsgWithMultipleAttachments(t *testing.T) {
	html := `<html><body>hi</body></html>`
	atts := Attachments{
		"a.txt": strings.NewReader("first"),
		"b.bin": strings.NewReader("second"),
	}
	out, err := renderMsg(html, sampleHeaders(), atts)
	assert.Nil(t, err)

	parsed, err := mail.ReadMessage(bytes.NewReader(out))
	assert.Nil(t, err)

	_, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	assert.Nil(t, err)

	mr := multipart.NewReader(parsed.Body, params["boundary"])

	got := map[string][]byte{}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		assert.Nil(t, err)
		//nolint:errcheck // attachment parts may omit Content-Type
		mt, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if mt == "text/html" {
			continue
		}
		_, dispParams, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		assert.Nil(t, err)
		raw, err := io.ReadAll(part)
		assert.Nil(t, err)
		got[dispParams["filename"]] = decodePartBody(t, part.Header.Get("Content-Transfer-Encoding"), raw)
	}

	assert.Equal(t, []byte("first"), got["a.txt"])
	assert.Equal(t, []byte("second"), got["b.bin"])
}

// addX is a stand-in for the click-redirect rewriter: it makes a rewritten
// link recognisable without pulling a token issuer into the test.
func addX(link string) (string, error) {
	return link + "x", nil
}

func TestReplaceLinksNoTrackAttribute(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "valueless attribute",
			html:     `<a href="https://example.com" data-no-track>unsubscribe</a>`,
			expected: `<a href="https://example.com">unsubscribe</a>`,
		},
		{
			name:     "empty value",
			html:     `<a href="https://example.com" data-no-track="">unsubscribe</a>`,
			expected: `<a href="https://example.com">unsubscribe</a>`,
		},
		{
			name:     "true value",
			html:     `<a href="https://example.com" data-no-track="true">unsubscribe</a>`,
			expected: `<a href="https://example.com">unsubscribe</a>`,
		},
		{
			name:     "single quoted value",
			html:     `<a href="https://example.com" data-no-track='true'>unsubscribe</a>`,
			expected: `<a href="https://example.com">unsubscribe</a>`,
		},
		{
			name:     "unquoted value",
			html:     `<a href="https://example.com" data-no-track=true>unsubscribe</a>`,
			expected: `<a href="https://example.com">unsubscribe</a>`,
		},
		{
			name:     "uppercase attribute",
			html:     `<a href="https://example.com" DATA-NO-TRACK>unsubscribe</a>`,
			expected: `<a href="https://example.com">unsubscribe</a>`,
		},
		{
			name:     "mixed case attribute and value",
			html:     `<a href="https://example.com" Data-No-Track="TRUE">unsubscribe</a>`,
			expected: `<a href="https://example.com">unsubscribe</a>`,
		},
		{
			name:     "attribute before href",
			html:     `<a data-no-track href="https://example.com">unsubscribe</a>`,
			expected: `<a href="https://example.com">unsubscribe</a>`,
		},
		{
			name:     "self closing tag",
			html:     `<a href="https://example.com" data-no-track />`,
			expected: `<a href="https://example.com" />`,
		},
		{
			name:     "self closing tag without space",
			html:     `<a href="https://example.com" data-no-track/>`,
			expected: `<a href="https://example.com"/>`,
		},
		{
			name:     "sibling attributes are preserved",
			html:     `<a class="btn" href="https://example.com" data-no-track title="opt out">unsubscribe</a>`,
			expected: `<a class="btn" href="https://example.com" title="opt out">unsubscribe</a>`,
		},
		{
			name:     "attribute on an empty href",
			html:     `<a href="" data-no-track></a>`,
			expected: `<a href=""></a>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := replaceLinks(tt.html, addX)
			assert.Nil(t, err)
			assert.Equal(t, tt.expected, res)
		})
	}
}

// A similar attribute name must not be mistaken for the opt-out, and the
// literal string in body text or in an href value must survive untouched.
func TestReplaceLinksNoTrackLookalikes(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "longer attribute name",
			html:     `<a href="https://example.com" data-no-tracking="yes">go</a>`,
			expected: `<a href="https://example.comx" data-no-tracking="yes">go</a>`,
		},
		{
			name:     "literal in body text",
			html:     `<p>use data-no-track to opt out</p>`,
			expected: `<p>use data-no-track to opt out</p>`,
		},
		{
			name:     "literal in the href value",
			html:     `<a href="https://example.com/?data-no-track=1">go</a>`,
			expected: `<a href="https://example.com/?data-no-track=1x">go</a>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := replaceLinks(tt.html, addX)
			assert.Nil(t, err)
			assert.Equal(t, tt.expected, res)
		})
	}
}

func TestReplaceLinksSkipsNonHTTPSchemes(t *testing.T) {
	untouched := []struct {
		name string
		html string
	}{
		{name: "mailto", html: `<a href="mailto:info@example.com">write us</a>`},
		{name: "mailto uppercase", html: `<a href="MAILTO:info@example.com">write us</a>`},
		{name: "mailto with query", html: `<a href="mailto:info@example.com?subject=hi">write us</a>`},
		{name: "tel", html: `<a href="tel:+390123456789">call us</a>`},
		{name: "tel mixed case", html: `<a href="Tel:+390123456789">call us</a>`},
		{name: "sms", html: `<a href="sms:+390123456789">text us</a>`},
		{name: "in page anchor", html: `<a href="#section">jump</a>`},
		{name: "whitespace only href", html: `<a href=" ">nowhere</a>`},
	}

	for _, tt := range untouched {
		t.Run(tt.name, func(t *testing.T) {
			res, err := replaceLinks(tt.html, addX)
			assert.Nil(t, err)
			assert.Equal(t, tt.html, res)
		})
	}
}

func TestReplaceLinksRewritesTrackableSchemes(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "https",
			html:     `<a href="https://example.com">go</a>`,
			expected: `<a href="https://example.comx">go</a>`,
		},
		{
			name:     "http",
			html:     `<a href="http://example.com">go</a>`,
			expected: `<a href="http://example.comx">go</a>`,
		},
		{
			name:     "single quoted href",
			html:     `<a href='https://example.com'>go</a>`,
			expected: `<a href='https://example.comx'>go</a>`,
		},
		{
			name:     "relative link",
			html:     `<a href="/promo">go</a>`,
			expected: `<a href="/promox">go</a>`,
		},
		{
			name:     "url containing an anchor",
			html:     `<a href="https://example.com/page#section">go</a>`,
			expected: `<a href="https://example.com/page#sectionx">go</a>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := replaceLinks(tt.html, addX)
			assert.Nil(t, err)
			assert.Equal(t, tt.expected, res)
		})
	}
}

// A tracked and an opted-out link in the same document must not interfere:
// only the tracked one is rewritten, and only the opted-out one loses its
// attribute.
func TestReplaceLinksMixedDocument(t *testing.T) {
	html := `<html>
<body>
<a href="https://example.com/promo">promo</a>
<a href="mailto:info@example.com">write us</a>
<a href="https://example.com/preferences" data-no-track>preferences</a>
<a href="https://example.com/promo">promo again</a>
</body></html>`

	expected := `<html>
<body>
<a href="https://example.com/promox">promo</a>
<a href="mailto:info@example.com">write us</a>
<a href="https://example.com/preferences">preferences</a>
<a href="https://example.com/promox">promo again</a>
</body></html>`

	res, err := replaceLinks(html, addX)
	assert.Nil(t, err)
	assert.Equal(t, expected, res)
}

// An error from the rewriter aborts the whole document: a partially rewritten
// body must never be delivered.
func TestReplaceLinksPropagatesError(t *testing.T) {
	html := `<a href="https://example.com">go</a>`

	res, err := replaceLinks(html, func(string) (string, error) {
		return "", errors.New("token issuer down")
	})
	assert.NotNil(t, err)
	assert.Equal(t, "", res)
}

// Opting out must not cost a token: the rewriter is not called at all for a
// skipped link.
func TestReplaceLinksDoesNotCallRewriterWhenSkipped(t *testing.T) {
	html := `<a href="https://example.com" data-no-track>a</a>
<a href="mailto:info@example.com">b</a>
<a href="#top">c</a>`

	calls := 0
	_, err := replaceLinks(html, func(link string) (string, error) {
		calls++
		return link + "x", nil
	})
	assert.Nil(t, err)
	assert.Equal(t, 0, calls)
}

// https://github.com/kannon-email/kannon/issues/276
func TestIssue276Link(t *testing.T) {
	html := `<html>
<body>
<img src="https://google.com/test" />
<a href="https://google.com" />
</body></html>`

	expectedhtml := `<html>
<body>
<img src="https://google.com/test" />
<a href="https://google.comx" />
</body></html>`

	res, err := replaceLinks(html, func(link string) (string, error) {
		return link + "x", nil
	})
	assert.Nil(t, err)
	assert.Equal(t, expectedhtml, res)
}
