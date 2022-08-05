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
	jwt.StandardClaims
}

type LinkClaims struct {
	MessageID string `json:"message_id"`
	Email     string `json:"email"`
	Url       string `json:"url"`
	jwt.StandardClaims
}

func generateKeyPair() (*rsa.PrivateKey, *rsa.PublicKey, error) {
	privatekey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot generate private key: %w", err)
	}

	publickey := privatekey.Public()
	return privatekey, publickey.(*rsa.PublicKey), nil
}

func createOpenToken(ctx context.Context, q *sqlc.Queries, privateKey *rsa.PrivateKey, kid string, now time.Time, messageID string, email string) (string, error) {
	claims := &OpenClaims{
		MessageID: messageID,
		Email:     email,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: now.Add(tokenExpirePeriod).Unix(),
			Audience:  "stats",
			IssuedAt:  now.Unix(),
		},
	}

	token, err := createJWT(claims, privateKey, kid)
	if err != nil {
		return "", err
	}

	return token, nil
}

func createLinkToken(ctx context.Context, q *sqlc.Queries, privateKey *rsa.PrivateKey, kid string, now time.Time, messageID string, email string, url string) (string, error) {
	claims := &LinkClaims{
		MessageID: messageID,
		Email:     email,
		Url:       url,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: now.Add(tokenExpirePeriod).Unix(),
			Audience:  "stats",
			IssuedAt:  now.Unix(),
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
	privkey_bytes, err := x509.MarshalPKCS8PrivateKey(privkey)
	if err != nil {
		return "", err
	}
	privkey_pem := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: privkey_bytes,
		},
	)
	return string(privkey_pem), nil
}

func exportRsaPublicKeyAsPemStr(pubkey *rsa.PublicKey) (string, error) {
	pubkey_bytes, err := x509.MarshalPKIXPublicKey(pubkey)
	if err != nil {
		return "", err
	}
	pubkey_pem := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pubkey_bytes,
		},
	)

	return string(pubkey_pem), nil
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
