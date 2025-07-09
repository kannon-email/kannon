package e2e_test

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"

	"mime/quotedprintable"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ParsedEmail struct {
	From        string
	To          string
	Subject     string
	Body        string
	Attachments []Attachment
}

func parseEmail(t *assert.CollectT, body []byte) ParsedEmail {
	msg, err := mail.ReadMessage(bytes.NewReader(body))
	require.NoError(t, err)

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	require.NoError(t, err)

	from := msg.Header.Get("From")
	to := msg.Header.Get("To")
	subject := msg.Header.Get("Subject")

	if strings.HasPrefix(mediaType, "multipart/") {
		emailBody, attachments := parseMultipartEmail(t, msg.Body, params["boundary"])
		return ParsedEmail{
			From:        from,
			To:          to,
			Subject:     subject,
			Body:        emailBody,
			Attachments: attachments,
		}
	}

	emailBody := parseSimpleEmailBody(t, msg.Body)
	return ParsedEmail{
		From:        from,
		To:          to,
		Subject:     subject,
		Body:        emailBody,
		Attachments: nil,
	}
}

type Attachment struct {
	Filename string
	Content  []byte
}

// readDecodedPartContent reads and decodes the content of a MIME part based on its encoding.
func readDecodedPartContent(p *multipart.Part, t *assert.CollectT) ([]byte, string, string) {
	contentType := p.Header.Get("Content-Type")
	disposition := p.Header.Get("Content-Disposition")
	contentEncoding := strings.ToLower(p.Header.Get("Content-Transfer-Encoding"))

	var content []byte
	var err error
	switch contentEncoding {
	case "base64":
		content, err = parseBase64Content(p)
		require.NoError(t, err)
		return content, contentType, disposition
	case "quoted-printable":
		content, err = parseQuotedPrintableContent(p)
		require.NoError(t, err)
		return content, contentType, disposition
	default:
		slurp, err := io.ReadAll(p)
		require.NoError(t, err)
		content = slurp
		return content, contentType, disposition
	}
}

func parseQuotedPrintableContent(p *multipart.Part) ([]byte, error) {
	qpReader := quotedprintable.NewReader(p)
	decoded, err := io.ReadAll(qpReader)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}
func parseBase64Content(p *multipart.Part) ([]byte, error) {
	decoded, err := io.ReadAll(p)
	if err != nil {
		return nil, err
	}
	decodedBuf := make([]byte, base64.StdEncoding.DecodedLen(len(decoded)))
	n, err := base64.StdEncoding.Decode(decodedBuf, decoded)
	if err != nil {
		return nil, err
	}
	return decodedBuf[:n], nil
}

// parseMultipartEmail parses the body and attachments from a multipart email.
func parseMultipartEmail(t *assert.CollectT, body io.Reader, boundary string) (string, []Attachment) {
	mr := multipart.NewReader(body, boundary)
	var attachments []Attachment
	var emailBody string

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		content, contentType, disposition := readDecodedPartContent(p, t)

		if strings.HasPrefix(disposition, "attachment") {
			filename := p.FileName()
			attachments = append(attachments, Attachment{
				Filename: filename,
				Content:  content,
			})
		} else if strings.HasPrefix(contentType, "text/plain") ||
			strings.HasPrefix(contentType, "text/html") {
			emailBody = string(content)
		}
	}
	return emailBody, attachments
}

// parseSimpleEmailBody reads the body from a non-multipart email.
func parseSimpleEmailBody(t *assert.CollectT, body io.Reader) string {
	b, err := io.ReadAll(body)
	require.NoError(t, err)
	return string(b)
}
