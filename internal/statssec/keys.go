package statssec

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/tracking"
)

const tokenExpirePeriod = time.Hour * 24 * 30 * 3 // 3 months

// A token is bound to the engagement channel it was minted for by its audience,
// and the Tracker's two endpoints accept only their own.
//
// Without that binding the two token types are interchangeable: their claim
// shapes differ only by a field JSON parsing ignores when absent, so a link
// token parses cleanly as open claims and hands the Tracker the Mode governing
// *links*. Any Domain whose two axes differ would then have its more permissive
// axis apply to both endpoints — a Domain on `opens=off, links=full` could be
// made to record an identified open, with the requester's IP, by replaying a
// link token against /o/. Since the Domain's Policy is the only guarantee an
// operator has (ADR 0003), the Mode has to be bound to the channel it governs
// and not merely present.
const (
	audienceOpen = "stats:open"
	audienceLink = "stats:link"
	// audienceLegacy is what both token types carried before the Tracking Mode
	// became a claim, when nothing distinguished them. Accepted only from a token
	// that states no Mode: see assertAudience.
	audienceLegacy = "stats"
)

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
//
// Email is empty under a Mode that does not identify the Recipient — see
// identityUnder.
type OpenClaims struct {
	MessageID string        `json:"message_id"`
	Email     string        `json:"email"`
	Mode      tracking.Mode `json:"mode,omitempty"`
	jwt.RegisteredClaims
}

// LinkClaims are the claims of a link token. Mode governs links rather than
// opens, and Mode and Email carry the same guarantees as their OpenClaims
// counterparts.
type LinkClaims struct {
	MessageID string        `json:"message_id"`
	Email     string        `json:"email"`
	URL       string        `json:"url"`
	Mode      tracking.Mode `json:"mode,omitempty"`
	jwt.RegisteredClaims
}

// identityUnder returns the Recipient identity a token minted under mode may
// carry: none at all under a Mode that does not identify the Recipient
// (Anonymous, and the reserved Pseudonymous).
//
// That is the privacy property of Anonymous made true in fact rather than in
// intent — a token holding no address cannot isolate one Recipient from another
// even if it is captured — and it is what makes the token a function of the Batch
// instead of the Delivery, so the same one can be issued to every Recipient of a
// Batch instead of signing RSA-4096 once per Delivery.
//
// The drop happens here, where the claim is assembled, rather than in the caller:
// this is the one place every token passes through, so no caller can mint an
// Anonymous token that names somebody by forgetting to blank the address first.
func identityUnder(mode tracking.Mode, email string) string {
	if !mode.IdentifiesRecipient() {
		return ""
	}
	return email
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
		Email:     identityUnder(mode, email),
		Mode:      mode,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenExpirePeriod)),
			Audience:  []string{audienceOpen},
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
		Email:     identityUnder(mode, email),
		URL:       url,
		Mode:      mode,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenExpirePeriod)),
			Audience:  []string{audienceLink},
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

// channelClaims is what the two token types have in common at verification time:
// the channel a token was minted for, and the Mode it carries. Both are needed
// together, because whether a Mode may be honoured depends on which channel
// signed it.
type channelClaims interface {
	jwt.Claims
	boundTo() (jwt.ClaimStrings, tracking.Mode)
}

func (c *OpenClaims) boundTo() (jwt.ClaimStrings, tracking.Mode) { return c.Audience, c.Mode }
func (c *LinkClaims) boundTo() (jwt.ClaimStrings, tracking.Mode) { return c.Audience, c.Mode }

func verifyOpenToken(ctx context.Context, tokenString string, q *sqlc.Queries) (*OpenClaims, error) {
	return verifyToken(ctx, tokenString, q, &OpenClaims{}, audienceOpen)
}

func verifyLinkToken(ctx context.Context, tokenString string, q *sqlc.Queries) (*LinkClaims, error) {
	return verifyToken(ctx, tokenString, q, &LinkClaims{}, audienceLink)
}

// verifyToken checks a stats token's signature, its registered claims and the
// channel it was minted for, returning the claims only if all three hold. The two
// channels share it so that a check added for one can never be forgotten for the
// other — the shape of the two token types differs only in a URL.
func verifyToken[C channelClaims](ctx context.Context, tokenString string, q *sqlc.Queries, into C, want string) (C, error) {
	var none C

	token, err := jwt.ParseWithClaims(tokenString, into, getVerifyTokenFunc(ctx, q), verifyOptions()...)
	if err != nil {
		return none, fmt.Errorf("cannot parse jwt: %w", err)
	}

	if !token.Valid {
		return none, errors.New("invalid token")
	}

	claims, ok := token.Claims.(C)
	if !ok {
		return none, errors.New("cannot unstructure claims")
	}

	audience, mode := claims.boundTo()
	if err := assertAudience(audience, mode, want); err != nil {
		return none, err
	}

	return claims, nil
}

// verifyOptions pins what every stats token must satisfy regardless of channel:
// the signing method Kannon actually mints with, and an expiry. Both are true of
// every token this codebase has ever produced; stating them here means a future
// mint path cannot quietly drop either.
func verifyOptions() []jwt.ParserOption {
	return []jwt.ParserOption{
		jwt.WithValidMethods([]string{jwt.SigningMethodRS512.Alg()}),
		jwt.WithExpirationRequired(),
	}
}

// assertAudience refuses a token minted for a different engagement channel, so
// that the Tracking Mode a token carries can only ever govern the channel it was
// signed for.
//
// The audience is checked here rather than through jwt.WithAudience because the
// rule is not a single expected value: a token minted before the Mode became a
// claim carries the one legacy audience and no Mode, and refusing those outright
// would silently drop every open and click from mail already in flight, for up to
// tokenExpirePeriod after an upgrade. Such a token is accepted, and states
// nothing, so it can never widen what the Tracker retains — and because this
// build always signs a Mode into a token, a Mode-bearing token can never take
// the legacy path. The exception disappears on its own as those tokens expire.
func assertAudience(audience jwt.ClaimStrings, mode tracking.Mode, want string) error {
	if slices.Contains(audience, want) {
		return nil
	}
	if mode == tracking.ModeUnspecified && slices.Contains(audience, audienceLegacy) {
		return nil
	}
	return fmt.Errorf("token audience %v is not valid for %q", audience, want)
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
