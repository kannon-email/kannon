package statssec

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/tracking"
)

const tokenExpirePeriod = time.Hour * 24 * 30 * 3 // 3 months

// OpenClaims are the claims of an open token: what the Tracker is allowed to
// know about the request that retrieved the tracking pixel.
//
// Mode is the Tracking Mode governing opens for the Delivery the token was
// minted for, frozen at intake (ADR 0003). It travels in the token rather than
// being looked up because Pool rows are deleted on terminal outcomes, so by the
// time an open arrives there may be no Delivery row left to consult — and
// because the token is signed, so a recipient can neither escalate their own
// tracking nor suppress it to skew a sender's statistics.
//
// A token minted before the Mode became a claim carries none, which states
// nothing and therefore never reaches Full: the absence can only ever restrict
// what the Tracker retains, never widen it.
type OpenClaims struct {
	MessageID string        `json:"message_id"`
	Email     string        `json:"email"`
	Mode      tracking.Mode `json:"mode,omitempty"`
	jwt.RegisteredClaims
}

// LinkClaims are the claims of a link token. Mode governs links rather than
// opens, and carries the same guarantees as OpenClaims.Mode.
type LinkClaims struct {
	MessageID string        `json:"message_id"`
	Email     string        `json:"email"`
	URL       string        `json:"url"`
	Mode      tracking.Mode `json:"mode,omitempty"`
	jwt.RegisteredClaims
}

func generateKeyPair() (*rsa.PrivateKey, *rsa.PublicKey, error) {
	privatekey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot generate private key: %w", err)
	}

	publickey := privatekey.Public()
	return privatekey, publickey.(*rsa.PublicKey), nil //nolint:errcheck // RSA private key always returns *rsa.PublicKey
}

func createOpenToken(privateKey *rsa.PrivateKey, kid string, now time.Time, messageID string, email string, mode tracking.Mode) (string, error) {
	claims := &OpenClaims{
		MessageID: messageID,
		Email:     email,
		Mode:      mode,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenExpirePeriod)),
			Audience:  []string{"stats"},
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token, err := createJWT(claims, privateKey, kid)
	if err != nil {
		return "", err
	}

	return token, nil
}

func createLinkToken(privateKey *rsa.PrivateKey, kid string, now time.Time, messageID string, email string, url string, mode tracking.Mode) (string, error) {
	claims := &LinkClaims{
		MessageID: messageID,
		Email:     email,
		URL:       url,
		Mode:      mode,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenExpirePeriod)),
			Audience:  []string{"stats"},
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token, err := createJWT(claims, privateKey, kid)
	if err != nil {
		return "", err
	}

	return token, nil
}

func createJWT(claims jwt.Claims, privateKey *rsa.PrivateKey, kid string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
	token.Header["kid"] = kid

	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("cannot creating JWT: %w", err)
	}

	return tokenString, nil
}

func exportRsaPrivateKeyAsPemStr(privkey *rsa.PrivateKey) (string, error) {
	privkeyBytes, err := x509.MarshalPKCS8PrivateKey(privkey)
	if err != nil {
		return "", err
	}
	privkeyPem := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: privkeyBytes,
		},
	)
	return string(privkeyPem), nil
}

func exportRsaPublicKeyAsPemStr(pubkey *rsa.PublicKey) (string, error) {
	pubkeyBytes, err := x509.MarshalPKIXPublicKey(pubkey)
	if err != nil {
		return "", err
	}
	pubkeyPem := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pubkeyBytes,
		},
	)

	return string(pubkeyPem), nil
}

func verifyOpenToken(ctx context.Context, tokenString string, q *sqlc.Queries) (*OpenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &OpenClaims{}, getVerifyTokenFunc(ctx, q))
	if err != nil {
		return nil, fmt.Errorf("cannot parse jwt: %w", err)
	}

	if !token.Valid {
		return nil, errors.New("invalit token")
	}

	claims, ok := token.Claims.(*OpenClaims)
	if !ok {
		return nil, errors.New("cannot unstructure claims")
	}
	return claims, nil
}

func verifyLinkToken(ctx context.Context, tokenString string, q *sqlc.Queries) (*LinkClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &LinkClaims{}, getVerifyTokenFunc(ctx, q))
	if err != nil {
		return nil, fmt.Errorf("cannot parse jwt: %w", err)
	}

	if !token.Valid {
		return nil, errors.New("invalit token")
	}

	claims, ok := token.Claims.(*LinkClaims)
	if !ok {
		return nil, errors.New("cannot unstructure claims")
	}
	return claims, nil
}

func getVerifyTokenFunc(ctx context.Context, q *sqlc.Queries) func(token *jwt.Token) (interface{}, error) {
	return func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("key not found for kid: %v", kid)
		}

		publicKeyString, err := q.GetValidPublicStatsKeyByKid(ctx, kid)
		if err != nil {
			return nil, fmt.Errorf("key not found for provided kid: %w", err)
		}

		publicKey, err := jwt.ParseRSAPublicKeyFromPEM([]byte(publicKeyString.PublicKey))
		if err != nil {
			return nil, fmt.Errorf("error parsing publicKey: %w", err)
		}

		return publicKey, nil
	}
}
