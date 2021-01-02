package dkim

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"

	"github.com/emersion/go-msgauth/dkim"
)

// SignData to pass to dkim
type SignData struct {
	PrivateKey string
	Domain     string
	Selector   string
	Headers    []string
}

// SignMessage signes an email message with DKIM
func SignMessage(data SignData, reader *bytes.Reader) ([]byte, error) {
	signer, err := decodeKey(data.PrivateKey)
	if err != nil {
		return nil, err
	}
	options := &dkim.SignOptions{
		Domain:     data.Domain,
		Selector:   data.Selector,
		Signer:     signer,
		HeaderKeys: data.Headers,
	}

	var b bytes.Buffer
	if err := dkim.Sign(&b, reader, options); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func decodeKey(dkimPrivateKey string) (*rsa.PrivateKey, error) {
	dkimPrivateKeyInBytes, err := base64.StdEncoding.DecodeString(dkimPrivateKey)
	if err != nil {
		return nil, err
	}
	rsak, err := x509.ParsePKCS1PrivateKey(dkimPrivateKeyInBytes)
	return rsak, nil
}
