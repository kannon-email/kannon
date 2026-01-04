package domains

import (
	"context"
	"crypto/rand"

	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/dkim"
)

type domainManager struct {
	q *sqlc.Queries
}

type DomainManager interface {
	CreateDomain(ctx context.Context, domain string) (sqlc.Domain, error)
	FindDomain(ctx context.Context, domain string) (sqlc.Domain, error)
	FindDomainWithKey(ctx context.Context, domain string, key string) (sqlc.Domain, error)
	GetAllDomains(ctx context.Context) ([]sqlc.Domain, error)
	RegenerateDomainKey(ctx context.Context, domain string) (sqlc.Domain, error)
	Close() error
}

func NewDomainManager(q *sqlc.Queries) DomainManager {
	return &domainManager{
		q: q,
	}
}

func (dm *domainManager) CreateDomain(ctx context.Context, domain string) (sqlc.Domain, error) {
	keys, err := dkim.GenerateDKIMKeysPair()
	if err != nil {
		return sqlc.Domain{}, err
	}

	d, err := dm.q.CreateDomain(ctx, sqlc.CreateDomainParams{
		Domain:         domain,
		Key:            generateRandomKey(),
		DkimPrivateKey: keys.PrivateKey,
		DkimPublicKey:  keys.PublicKey,
	})

	if err != nil {
		return sqlc.Domain{}, err
	}

	return d, nil
}

func (dm *domainManager) FindDomain(ctx context.Context, d string) (sqlc.Domain, error) {
	domain, err := dm.q.FindDomain(ctx, d)
	if err != nil {
		return domain, err
	}
	return domain, nil
}

func (dm *domainManager) FindDomainWithKey(ctx context.Context, d string, k string) (sqlc.Domain, error) {
	domain, err := dm.q.FindDomainWithKey(ctx, sqlc.FindDomainWithKeyParams{
		Domain: d,
		Key:    k,
	})
	if err != nil {
		return domain, err
	}
	return domain, nil
}

func (dm *domainManager) GetAllDomains(ctx context.Context) ([]sqlc.Domain, error) {
	domains, err := dm.q.GetAllDomains(ctx)
	if err != nil {
		return domains, err
	}
	return domains, nil
}

func (dm *domainManager) RegenerateDomainKey(ctx context.Context, d string) (sqlc.Domain, error) {
	domain, err := dm.q.SetDomainKey(ctx, sqlc.SetDomainKeyParams{
		Key:    generateRandomKey(),
		Domain: d,
	})
	if err != nil {
		return sqlc.Domain{}, err
	}
	return domain, nil
}

func (dm *domainManager) Close() error {
	return nil
}

// TODO: Pass keySize from the controller
const (
	keySize = 30
)

func generateRandomKey() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, keySize)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}
