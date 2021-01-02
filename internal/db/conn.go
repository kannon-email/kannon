package db

import (
	"fmt"
	"log"

	"github.com/kelseyhightower/envconfig"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type dBConfig struct {
	Host     string `default:"localhost"`
	Port     int    `default:"5432"`
	User     string `required:"true"`
	Name     string `required:"true"`
	Password string `required:"true"`
}

// NewDb crate a new DB instance using env variable configuration
func NewDb(automigrate bool) (*gorm.DB, error) {
	var config dBConfig
	err := envconfig.Process("db", &config)
	if err != nil {
		log.Fatal(err.Error())
	}

	dbConnString := fmt.Sprintf("host=%v port=%v user=%v dbname=%v password=%v sslmode=disable", config.Host, config.Port, config.User, config.Name, config.Password)

	db, err := gorm.Open(postgres.Open(dbConnString), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// db.DB().SetMaxIdleConns(10)
	// db.DB().SetMaxOpenConns(100)
	// db.DB().SetConnMaxLifetime(time.Hour)

	if !automigrate {
		return db, nil
	}

	err = autoMigrate(db)
	if err != nil {
		return nil, err
	}
	return db, nil
}

// AutoMigrate performs auto migration
func autoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&Domain{}, &Template{}, &SendingPool{}, &SendingPoolEmail{})
}
