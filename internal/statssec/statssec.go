package statssec

import (
	"context"
	"crypto/rsa"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/tracking"
	"github.com/kannon-email/kannon/internal/utils"
)

// StatsService mints and verifies the signed tokens the Tracker acts on. The
// Tracking Mode is minted into the token, so what the Tracker may observe about
// a request is fixed by a signature at send time rather than looked up when the
// engagement arrives.
//
// The Mode also decides which address the email argument of a Create call becomes
// in the token: the Recipient's own under a Mode that names them, a sentinel
// address under the Modes that name nobody (see identityUnder). Under Anonymous
// that sentinel is constant per Domain, so the minted token is the same one for
// every Recipient of a Batch; under Pseudonymous it is the caller's per-Delivery
// pseudonym, and a mint whose identity is not a sentinel of the Batch's Domain is
// refused with ErrIdentityOutsideNamespace.
type StatsService interface {
	CreateOpenToken(ctx context.Context, messageID string, identity string, mode tracking.Mode) (string, error)
	CreateLinkToken(ctx context.Context, messageID string, identity string, url string, mode tracking.Mode) (string, error)
	VerifyOpenToken(ctx context.Context, token string) (*OpenClaims, error)
	VerifyLinkToken(ctx context.Context, token string) (*LinkClaims, error)
}

func NewStatsService(q *sqlc.Queries) StatsService {
	return &service{
		q:   q,
		now: time.Now,
	}
}

type service struct {
	q   *sqlc.Queries
	now func() time.Time
}

func (s *service) CreateOpenToken(ctx context.Context, messageID string, identity string, mode tracking.Mode) (string, error) {
	privateKey, kid, err := s.getSignKeys(ctx)
	if err != nil {
		return "", err
	}

	token, err := createOpenToken(privateKey, kid, s.now(), messageID, identity, mode)
	if err != nil {
		return "", err
	}

	return token, nil
}

func (s *service) CreateLinkToken(ctx context.Context, messageID string, identity string, url string, mode tracking.Mode) (string, error) {
	privateKey, kid, err := s.getSignKeys(ctx)
	if err != nil {
		return "", err
	}

	token, err := createLinkToken(privateKey, kid, s.now(), messageID, identity, url, mode)
	if err != nil {
		return "", err
	}

	return token, nil
}

func (s *service) VerifyOpenToken(ctx context.Context, token string) (*OpenClaims, error) {
	return verifyOpenToken(ctx, token, s.q)
}

func (s *service) VerifyLinkToken(ctx context.Context, token string) (*LinkClaims, error) {
	return verifyLinkToken(ctx, token, s.q)
}

func (s *service) getSignKeys(ctx context.Context) (*rsa.PrivateKey, string, error) {
	privateKey, _, kid, err := s.getExistingSignKeys(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, "", err
	}

	if errors.Is(err, sql.ErrNoRows) {
		privateKey, _, kid, err := s.generateNewKeyPairs(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("cannot generate new keys: %w", err)
		}
		return privateKey, kid, nil
	}

	return privateKey, kid, nil
}

func (s *service) getExistingSignKeys(ctx context.Context) (*rsa.PrivateKey, *rsa.PublicKey, string, error) {
	q := s.q

	ts := sqlc.PgTimestampFromTime(s.now().Add(tokenExpirePeriod))
	keys, err := q.GetValidStatsKeys(ctx, ts)
	if err != nil {
		return nil, nil, "", err
	}

	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(keys.PrivateKey))
	if err != nil {
		return nil, nil, "", err
	}

	publicKey, err := jwt.ParseRSAPublicKeyFromPEM([]byte(keys.PublicKey))
	if err != nil {
		return nil, nil, "", err
	}

	return privateKey, publicKey, keys.ID, nil
}

func (s *service) generateNewKeyPairs(ctx context.Context) (*rsa.PrivateKey, *rsa.PublicKey, string, error) {
	q := s.q

	privateKey, publicKey, err := generateKeyPair()
	if err != nil {
		return nil, nil, "", err
	}

	pemPrivate, err := exportRsaPrivateKeyAsPemStr(privateKey)
	if err != nil {
		return nil, nil, "", err
	}

	pemPublic, err := exportRsaPublicKeyAsPemStr(publicKey)
	if err != nil {
		return nil, nil, "", err
	}

	id := utils.NewID("key")

	exp := sqlc.PgTimestampFromTime(s.now().Add(2 * tokenExpirePeriod))

	netKeys, err := q.CreateStatsKeys(ctx, sqlc.CreateStatsKeysParams{
		ID:             id,
		PrivateKey:     pemPrivate,
		PublicKey:      pemPublic,
		ExpirationTime: exp,
	})
	if err != nil {
		return nil, nil, "", err
	}

	return privateKey, publicKey, netKeys.ID, nil
}
