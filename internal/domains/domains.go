package domains

import (
	"math/rand"
	"time"

	"gorm.io/gorm"
	"smtp.ludusrusso.space/internal/db"
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

func (dm *domainManager) FindDomain(domain string) (db.Domain, error) {
	domainModel := db.Domain{}
	err := dm.db.Find(&domainModel, "domain = ?", domain).Error
	if err != nil {
		return domainModel, err
	}
	return domainModel, nil
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
