package statssec

import (
	"context"
	"crypto/rsa"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/utils"
)

type StatsService interface {
	CreateOpenToken(ctx context.Context, messageID string, email string) (string, error)
	CreateLinkToken(ctx context.Context, messageID string, email string, url string) (string, error)
	VertifyOpenToken(ctx context.Context, token string) (*OpenClaims, error)
	VertifyLinkToken(ctx context.Context, token string) (*LinkClaims, error)
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

func (s *service) CreateOpenToken(ctx context.Context, messageID string, email string) (string, error) {
	privateKey, kid, err := s.getSignKeys(ctx)
	if err != nil {
		return "", err
	}

	token, err := createOpenToken(privateKey, kid, s.now(), messageID, email)
	if err != nil {
		return "", err
	}

	return token, nil
}

func (s *service) CreateLinkToken(ctx context.Context, messageID string, email string, url string) (string, error) {
	privateKey, kid, err := s.getSignKeys(ctx)
	if err != nil {
		return "", err
	}

	token, err := createLinkToken(privateKey, kid, s.now(), messageID, email, url)
	if err != nil {
		return "", err
	}

	return token, nil
}

func (s *service) VertifyOpenToken(ctx context.Context, token string) (*OpenClaims, error) {
	return verifyOpenToken(ctx, token, s.q)
}

func (s *service) VertifyLinkToken(ctx context.Context, token string) (*LinkClaims, error) {
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

	keys, err := q.GetValidStatsKeys(ctx, s.now().Add(tokenExpirePeriod))
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

	id, err := utils.NewID("key")
	if err != nil {
		return nil, nil, "", err
	}

	netKeys, err := q.CreateStatsKeys(ctx, sqlc.CreateStatsKeysParams{
		ID:             id,
		PrivateKey:     pemPrivate,
		PublicKey:      pemPublic,
		ExpirationTime: s.now().Add(2 * tokenExpirePeriod),
	})
	if err != nil {
		return nil, nil, "", err
	}

	return privateKey, publicKey, netKeys.ID, nil
}
