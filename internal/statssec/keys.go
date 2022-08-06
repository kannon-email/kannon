package statssec

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
	sqlc "github.com/ludusrusso/kannon/internal/db"
)

const tokenExpirePeriod = time.Hour * 24 * 30 * 3 // 3 months

type OpenClaims struct {
	MessageID string `json:"message_id"`
	Email     string `json:"email"`
	jwt.RegisteredClaims
}

type LinkClaims struct {
	MessageID string `json:"message_id"`
	Email     string `json:"email"`
	URL       string `json:"url"`
	jwt.RegisteredClaims
}

func generateKeyPair() (*rsa.PrivateKey, *rsa.PublicKey, error) {
	privatekey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot generate private key: %w", err)
	}

	publickey := privatekey.Public()
	return privatekey, publickey.(*rsa.PublicKey), nil
}

func createOpenToken(privateKey *rsa.PrivateKey, kid string, now time.Time, messageID string, email string) (string, error) {
	claims := &OpenClaims{
		MessageID: messageID,
		Email:     email,
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

func createLinkToken(privateKey *rsa.PrivateKey, kid string, now time.Time, messageID string, email string, url string) (string, error) {
	claims := &LinkClaims{
		MessageID: messageID,
		Email:     email,
		URL:       url,
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
		return nil, fmt.Errorf("invalit token")
	}

	claims, ok := token.Claims.(*OpenClaims)
	if !ok {
		return nil, fmt.Errorf("cannot unstructure claims")
	}
	return claims, nil
}

func verifyLinkToken(ctx context.Context, tokenString string, q *sqlc.Queries) (*LinkClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &LinkClaims{}, getVerifyTokenFunc(ctx, q))
	if err != nil {
		return nil, fmt.Errorf("cannot parse jwt: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalit token")
	}

	claims, ok := token.Claims.(*LinkClaims)
	if !ok {
		return nil, fmt.Errorf("cannot unstructure claims")
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
