package db

import (
	"math/rand"

	"gorm.io/gorm"
	"kannon.gyozatech.dev/internal/dkim"
)

// Domain represent a sender domain
type Domain struct {
	gorm.Model
	Domain   string `gorm:"index,unique"`
	Key      string
	DKIMKeys DKIMKeys `gorm:"embedded;embeddedPrefix:dkim_"`
}

// DKIMKeys contains DKIM public and private keys of a domain
type DKIMKeys struct {
	PrivateKey string
	PublicKey  string
}

// BeforeCreate hooks creates dkim keys and private access key
// for the domain
func (d *Domain) BeforeCreate(tx *gorm.DB) error {
	keys, err := dkim.GenerateDKIMKeysPair()
	if err != nil {
		return err
	}
	d.DKIMKeys = DKIMKeys{
		PrivateKey: keys.PrivateKey,
		PublicKey:  keys.PublicKey,
	}
	d.Key = generateRandomKey(20)

	return nil
}

func generateRandomKey(size uint) string {
	letterRunes := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	b := make([]rune, size)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}
