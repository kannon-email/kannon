package dkim

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
)

// KeysPair sturct contains public and private key in base64 ecoding
type KeysPair struct {
	PrivateKey string
	PublicKey  string
}

// GenerateDKIMKeysPair generates DKIM private and public keys pair
func GenerateDKIMKeysPair() (KeysPair, error) {
	reader := rand.Reader
	bitSize := 2048
	key, err := rsa.GenerateKey(reader, bitSize)
	if err != nil {
		return KeysPair{}, err
	}

	privateKey := exportRsaPrivateKeyAsStr(key)
	publicKey, err := exportRsaPublicKeyAsStr(&key.PublicKey)
	if err != nil {
		return KeysPair{}, err
	}
	return KeysPair{
		PrivateKey: privateKey,
		PublicKey:  publicKey,
	}, nil
}

func exportRsaPrivateKeyAsStr(privkey *rsa.PrivateKey) string {
	privkeyBytes := x509.MarshalPKCS1PrivateKey(privkey)
	return base64.StdEncoding.EncodeToString(privkeyBytes)
}

func exportRsaPublicKeyAsStr(key *rsa.PublicKey) (string, error) {
	privkeyBytes, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(privkeyBytes), nil
}
