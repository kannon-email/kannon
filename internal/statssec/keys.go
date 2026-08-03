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
	"github.com/kannon-email/kannon/internal/utils"
)

const tokenExpirePeriod = time.Hour * 24 * 30 * 3 // 3 months

// A token is bound to the engagement channel it was minted for by its audience: the two claim
// shapes differ only by a field JSON ignores when absent, so without the binding a link token
// would verify as open claims and apply the links Mode to /o/ (ADR 0003).
const (
	audienceOpen = "stats:open"
	audienceLink = "stats:link"
	// audienceLegacy is what both token types carried before the Tracking Mode
	// became a claim, when nothing distinguished them. Accepted only from a token
	// that states no Mode: see assertAudience.
	audienceLegacy = "stats"
)

// OpenClaims are the claims of an open token: what the Tracker may know about the request that
// retrieved the pixel. Mode is frozen at intake and travels signed, since the Delivery row may be
// gone by then; Email is the identity claim. Both are absent on a pre-upgrade token.
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

// ErrIdentityOutsideNamespace is a mint refused because the identity offered for
// a Mode that must name nobody is not a sentinel address of the Batch's Domain —
// in practice, a caller passing the recipient's real address under Pseudonymous.
var ErrIdentityOutsideNamespace = errors.New("tracking identity outside the reserved namespace")

// identityUnder returns the identity a token minted under mode carries, always email-shaped and
// decided by the Mode alone (ADR 0006): the Recipient's address, the Builder's pseudonym, or the
// Anonymous sentinel. Decided here so no caller can name somebody under a Mode that must not.
func identityUnder(mode tracking.Mode, offered string, messageID string) (string, error) {
	if mode.IdentifiesRecipient() {
		return offered, nil
	}

	domain, err := utils.ExtractDomainFromMessageID(messageID)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the reserved namespace: %w", err)
	}

	if !mode.IsolatesRecipient() {
		return tracking.AnonymousIdentity(domain), nil
	}

	// Deliberately never logged or wrapped with the offending value: the whole
	// point of the refusal is that the value may be a recipient address.
	if !tracking.IsPseudonym(offered, domain) {
		return "", fmt.Errorf("%w: %s under %q", ErrIdentityOutsideNamespace, tracking.ReservedNamespace(domain), mode)
	}
	return offered, nil
}

func generateKeyPair() (*rsa.PrivateKey, *rsa.PublicKey, error) {
	privatekey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot generate private key: %w", err)
	}

	publickey := privatekey.Public()
	return privatekey, publickey.(*rsa.PublicKey), nil //nolint:errcheck // RSA private key always returns *rsa.PublicKey
}

func createOpenToken(privateKey *rsa.PrivateKey, kid string, now time.Time, messageID string, offered string, mode tracking.Mode) (string, error) {
	identity, err := identityUnder(mode, offered, messageID)
	if err != nil {
		return "", err
	}

	claims := &OpenClaims{
		MessageID: messageID,
		Email:     identity,
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

func createLinkToken(privateKey *rsa.PrivateKey, kid string, now time.Time, messageID string, offered string, url string, mode tracking.Mode) (string, error) {
	identity, err := identityUnder(mode, offered, messageID)
	if err != nil {
		return "", err
	}

	claims := &LinkClaims{
		MessageID: messageID,
		Email:     identity,
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

// channelClaims is what the two token types have in common at verification time: the channel a
// token was minted for and the Mode it carries. Both are needed together, since whether a Mode
// may be honoured depends on which channel signed it.
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

// verifyToken checks a stats token's signature, its registered claims and the channel it was
// minted for, returning the claims only if all three hold. Shared by both channels so a check
// added for one can never be forgotten for the other.
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

// verifyOptions pins what every stats token must satisfy regardless of channel: the signing
// method Kannon actually mints with, and an expiry. Stating them here means a future mint path
// cannot quietly drop either.
func verifyOptions() []jwt.ParserOption {
	return []jwt.ParserOption{
		jwt.WithValidMethods([]string{jwt.SigningMethodRS512.Alg()}),
		jwt.WithExpirationRequired(),
	}
}

// assertAudience refuses a token minted for a different engagement channel, so a Mode can only
// govern the channel it was signed for. Checked by hand rather than with jwt.WithAudience: a
// pre-upgrade token carries the legacy audience and no Mode, and is accepted until it expires.
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
