package apikeys

import (
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

// Domain errors
var (
	ErrKeyNotFound      = errors.New("api key not found")
	ErrKeyInactive      = errors.New("api key is inactive")
	ErrKeyExpired       = errors.New("api key has expired")
	ErrInvalidKey       = errors.New("invalid api key format")
	ErrKeyAlreadyExists = errors.New("api key already exists")
)

const (
	// keyPrefix is the required prefix for all API keys
	keyPrefix = "k_"

	// keyLength is the length of the random part of the key
	keyLength = 64

	// maskedKeyLength is the number of characters shown in list operations
	maskedKeyLength = 8
)

// APIKey represents a domain API key in the system
type APIKey struct {
	id            ID
	key           string
	name          string
	domain        string
	createdAt     time.Time
	expiresAt     *time.Time
	isActive      bool
	deactivatedAt *time.Time
}

// Getters

// ID returns the API key ID
func (k *APIKey) ID() ID {
	return k.id
}

// Key returns the full API key value
func (k *APIKey) Key() string {
	return k.key
}

// Name returns the API key name
func (k *APIKey) Name() string {
	return k.name
}

// Domain returns the domain the key belongs to
func (k *APIKey) Domain() string {
	return k.domain
}

// KeyID returns the key ID as a string (implements KeyRef interface)
func (k *APIKey) KeyID() ID {
	return k.id
}

// CreatedAt returns when the key was created
func (k *APIKey) CreatedAt() time.Time {
	return k.createdAt
}

// ExpiresAt returns when the key expires (nil means never)
func (k *APIKey) ExpiresAt() *time.Time {
	return k.expiresAt
}

// IsActiveStatus returns whether the key is active
func (k *APIKey) IsActiveStatus() bool {
	return k.isActive
}

// DeactivatedAt returns when the key was deactivated
func (k *APIKey) DeactivatedAt() *time.Time {
	return k.deactivatedAt
}

// NewAPIKey creates a new API key with generated key value and creation time set
func NewAPIKey(domain, name string, expiresAt *time.Time) (*APIKey, error) {
	if err := validateDomain(domain); err != nil {
		return nil, err
	}
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := validateExpiresAt(expiresAt); err != nil {
		return nil, err
	}

	id, err := NewID()
	if err != nil {
		return nil, err
	}

	keyValue, err := generateKey()
	if err != nil {
		return nil, err
	}

	return &APIKey{
		id:        id,
		key:       keyValue,
		name:      name,
		domain:    domain,
		createdAt: time.Now(),
		expiresAt: expiresAt,
		isActive:  true,
	}, nil
}

// LoadAPIKeyParams contains all parameters needed to load an APIKey from storage
type LoadAPIKeyParams struct {
	ID            ID
	Key           string
	Name          string
	Domain        string
	CreatedAt     time.Time
	ExpiresAt     *time.Time
	IsActive      bool
	DeactivatedAt *time.Time
}

// LoadAPIKey creates an APIKey from stored data (used by repository)
func LoadAPIKey(p LoadAPIKeyParams) *APIKey {
	return &APIKey{
		id:            p.ID,
		key:           p.Key,
		name:          p.Name,
		domain:        p.Domain,
		createdAt:     p.CreatedAt,
		expiresAt:     p.ExpiresAt,
		isActive:      p.IsActive,
		deactivatedAt: p.DeactivatedAt,
	}
}

// Methods

// MaskedKey returns the key with only the first 8 characters visible
// Example: "k_abc123..." from "k_abc123defghij..."
func (k *APIKey) MaskedKey() string {
	if len(k.key) <= maskedKeyLength {
		return k.key
	}
	return k.key[:maskedKeyLength] + "..."
}

// IsValid checks if the key is both active and not expired
func (k *APIKey) IsValid() bool {
	if !k.isActive {
		return false
	}
	if k.expiresAt != nil && time.Now().After(*k.expiresAt) {
		return false
	}
	return true
}

// Deactivate marks the key as inactive (irreversible)
func (k *APIKey) Deactivate() {
	if k.isActive {
		k.isActive = false
		now := time.Now()
		k.deactivatedAt = &now
	}
}

// IsExpired checks if the key has expired
func (k *APIKey) IsExpired() bool {
	return k.expiresAt != nil && time.Now().After(*k.expiresAt)
}

// generateKey creates a new API key with the k_ prefix
func generateKey() (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, keyLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate key: %w", err)
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return keyPrefix + string(b), nil
}

// validateName validates a key name
func validateName(name string) error {
	if name == "" {
		return errors.New("key name is required")
	}
	if len(name) > 100 {
		return errors.New("key name must be 100 characters or less")
	}
	return nil
}

// validateDomain validates a domain name
func validateDomain(domain string) error {
	if domain == "" {
		return errors.New("domain is required")
	}
	if len(domain) > 254 {
		return errors.New("domain must be 254 characters or less")
	}
	return nil
}

// validateExpiresAt validates expiration time
func validateExpiresAt(expiresAt *time.Time) error {
	if expiresAt != nil && expiresAt.Before(time.Now()) {
		return errors.New("expiration time must be in the future")
	}
	return nil
}
