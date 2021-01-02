package db

import (
	"gorm.io/driver/postgres"

	"gorm.io/gorm"
)

var db *gorm.DB

// GetDB initializes a database instance
func GetDB() (*gorm.DB, error) {
	if db != nil {
		return db, nil
	}
	psqlStr := "host=localhost user=postgres password=postgres dbname=smtp port=5432 sslmode=disable"
	newDb, err := gorm.Open(postgres.Open(psqlStr), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	db = newDb
	return db, nil

}
