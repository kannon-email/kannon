package apikeys

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/kannon-email/kannon/internal/values"
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

// HashKey computes the SHA-256 hash of a plaintext API key and returns it as a hex string.
func HashKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

// APIKey represents a domain API key in the system
type APIKey struct {
	id            ID
	keyHash       string
	keyPrefix     string
	name          string
	domain        values.DomainName
	createdAt     time.Time
	expiresAt     *time.Time
	isActive      bool
	deactivatedAt *time.Time
}

// CreateResult holds the result of creating a new API key.
// PlaintextKey is the raw key value, available only at creation time.
type CreateResult struct {
	Key          *APIKey
	PlaintextKey string
}

func (k *APIKey) ID() ID {
	return k.id
}

// KeyHash returns the SHA-256 hash of the API key
func (k *APIKey) KeyHash() string {
	return k.keyHash
}

// KeyPrefix returns the first 8 characters of the original key
func (k *APIKey) KeyPrefix() string {
	return k.keyPrefix
}

func (k *APIKey) Name() string {
	return k.name
}

// DomainName returns the Domain the key belongs to, in the form a Repository is
// addressed with (implements KeyRef interface)
func (k *APIKey) DomainName() values.DomainName {
	return k.domain
}

// Domain renders that domain name for the wire and for logs
func (k *APIKey) Domain() string {
	return k.domain.String()
}

// KeyID returns the key ID as a string (implements KeyRef interface)
func (k *APIKey) KeyID() ID {
	return k.id
}

func (k *APIKey) CreatedAt() time.Time {
	return k.createdAt
}

// ExpiresAt returns when the key expires (nil means never)
func (k *APIKey) ExpiresAt() *time.Time {
	return k.expiresAt
}

func (k *APIKey) IsActiveStatus() bool {
	return k.isActive
}

func (k *APIKey) DeactivatedAt() *time.Time {
	return k.deactivatedAt
}

// NewAPIKey creates a new API key with a generated value and creation time, returning a
// CreateResult with the hashed key and the plaintext. The Domain needs no validation: Parse has
// already refused an empty name and one longer than the narrowest column it lands in.
func NewAPIKey(domain values.DomainName, name string, expiresAt *time.Time) (*CreateResult, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := validateExpiresAt(expiresAt); err != nil {
		return nil, err
	}

	id := NewID()

	plaintext, err := generateKey()
	if err != nil {
		return nil, err
	}

	return &CreateResult{
		Key: &APIKey{
			id:        id,
			keyHash:   HashKey(plaintext),
			keyPrefix: plaintext[:maskedKeyLength],
			name:      name,
			domain:    domain,
			createdAt: time.Now(),
			expiresAt: expiresAt,
			isActive:  true,
		},
		PlaintextKey: plaintext,
	}, nil
}

// LoadAPIKeyParams contains all parameters needed to load an APIKey from storage
type LoadAPIKeyParams struct {
	ID            ID
	KeyHash       string
	KeyPrefix     string
	Name          string
	Domain        values.DomainName
	CreatedAt     time.Time
	ExpiresAt     *time.Time
	IsActive      bool
	DeactivatedAt *time.Time
}

// LoadAPIKey creates an APIKey from stored data (used by repository)
func LoadAPIKey(p LoadAPIKeyParams) *APIKey {
	return &APIKey{
		id:            p.ID,
		keyHash:       p.KeyHash,
		keyPrefix:     p.KeyPrefix,
		name:          p.Name,
		domain:        p.Domain,
		createdAt:     p.CreatedAt,
		expiresAt:     p.ExpiresAt,
		isActive:      p.IsActive,
		deactivatedAt: p.DeactivatedAt,
	}
}

// MaskedKey returns the key prefix with ellipsis
// Example: "k_abc123..." from prefix "k_abc123"
func (k *APIKey) MaskedKey() string {
	return k.keyPrefix + "..."
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

func validateName(name string) error {
	if name == "" {
		return errors.New("key name is required")
	}
	if len(name) > 100 {
		return errors.New("key name must be 100 characters or less")
	}
	return nil
}

func validateExpiresAt(expiresAt *time.Time) error {
	if expiresAt != nil && expiresAt.Before(time.Now()) {
		return errors.New("expiration time must be in the future")
	}
	return nil
}
