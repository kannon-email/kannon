package domains

import (
	"fmt"
	"math/rand"
	"time"

	"gorm.io/gorm"
	"kannon.gyozatech.dev/internal/db"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type domainManager struct {
	db *gorm.DB
}

// DomainManager Interface
type DomainManager interface {
	CreateDomain(domain string) (db.Domain, error)
	FindDomain(domain string) (db.Domain, error)
	FindDomainWithKey(domain string, key string) (db.Domain, error)
	GetAllDomains() ([]db.Domain, error)
	Close() error
}

// NewDomainManager is the contrusctor for a Domain Manager
func NewDomainManager(db *gorm.DB) (DomainManager, error) {
	return &domainManager{
		db: db,
	}, nil
}

func (dm *domainManager) CreateDomain(domain string) (db.Domain, error) {
	newDomain := db.Domain{
		Domain: domain,
	}

	err := dm.db.Create(&newDomain).Error
	if err != nil {
		return newDomain, err
	}

	return newDomain, nil
}

func (dm *domainManager) FindDomain(d string) (db.Domain, error) {
	domain := db.Domain{}
	err := dm.db.First(&domain, "domain = ?", d).Error
	if err != nil {
		return domain, err
	}
	return domain, nil
}

func (dm *domainManager) FindDomainWithKey(d string, k string) (db.Domain, error) {
	domain, err := dm.FindDomain(d)
	if err != nil {
		return domain, err
	}

	if domain.Key != k {
		return domain, fmt.Errorf("Invalid key")
	}
	return domain, nil
}

func (dm *domainManager) GetAllDomains() ([]db.Domain, error) {
	domains := []db.Domain{}
	err := dm.db.Find(&domains).Error
	if err != nil {
		return domains, err
	}
	return domains, nil
}

func (dm *domainManager) Close() error {
	return nil
}
