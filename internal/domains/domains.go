package domains

import (
	"context"
	"database/sql"
	"math/rand"
	"time"

	"kannon.gyozatech.dev/generated/sqlc"
	"kannon.gyozatech.dev/internal/dkim"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type domainManager struct {
	db *sqlc.Queries
}

// DomainManager Interface
type DomainManager interface {
	CreateDomain(domain string) (sqlc.Domain, error)
	FindDomain(domain string) (sqlc.Domain, error)
	FindDomainWithKey(domain string, key string) (sqlc.Domain, error)
	GetAllDomains() ([]sqlc.Domain, error)
	Close() error
}

// NewDomainManager is the contrusctor for a Domain Manager
func NewDomainManager(db *sql.DB) (DomainManager, error) {
	return &domainManager{
		db: sqlc.New(db),
	}, nil
}

// CreateDomain
func (dm *domainManager) CreateDomain(domain string) (sqlc.Domain, error) {
	keys, err := dkim.GenerateDKIMKeysPair()
	if err != nil {
		return sqlc.Domain{}, err
	}

	d, err := dm.db.CreateDomain(context.TODO(), sqlc.CreateDomainParams{
		Domain:         domain,
		Key:            generateRandomKey(20),
		DkimPrivateKey: keys.PrivateKey,
		DkimPublicKey:  keys.PublicKey,
	})

	if err != nil {
		return sqlc.Domain{}, err
	}

	return d, nil
}

func (dm *domainManager) FindDomain(d string) (sqlc.Domain, error) {
	domain, err := dm.db.FindDomain(context.TODO(), d)
	if err != nil {
		return domain, err
	}
	return domain, nil
}

func (dm *domainManager) FindDomainWithKey(d string, k string) (sqlc.Domain, error) {
	domain, err := dm.db.FindDomainWithKey(context.TODO(), sqlc.FindDomainWithKeyParams{
		Domain: d,
		Key:    k,
	})
	if err != nil {
		return domain, err
	}
	return domain, nil
}

func (dm *domainManager) GetAllDomains() ([]sqlc.Domain, error) {
	domains, err := dm.db.GetAllDomains(context.TODO())
	if err != nil {
		return domains, err
	}
	return domains, nil
}

func (dm *domainManager) Close() error {
	return nil
}

func generateRandomKey(size uint) string {
	letterRunes := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	b := make([]rune, size)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}
