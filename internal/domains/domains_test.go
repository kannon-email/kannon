package domains_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/kannon-email/kannon/db"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/domains"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/lib/pq"
)

var db *pgxpool.Pool
var q *sqlc.Queries
var dm domains.DomainManager

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		logrus.Fatalf("Could not start resource: %s", err)
	}

	q = sqlc.New(db)
	dm = domains.NewDomainManager(q)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		logrus.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

func cleanDB(t *testing.T) {
	_, err := db.Exec(context.Background(), "DELETE FROM domains CASCADE")
	require.NoError(t, err)
}

func TestCreateDomain(t *testing.T) {
	defer cleanDB(t)

	ctx := context.Background()
	domain, err := dm.CreateDomain(ctx, "example.com")

	require.NoError(t, err)
	assert.Equal(t, "example.com", domain.Domain)
	assert.NotEmpty(t, domain.Key, "API key should be generated")
	assert.NotEmpty(t, domain.DkimPrivateKey, "DKIM private key should be generated")
	assert.NotEmpty(t, domain.DkimPublicKey, "DKIM public key should be generated")
	assert.Equal(t, 30, len(domain.Key), "API key should be 30 characters")
}

func TestCreateDomain_GeneratesRandomKeys(t *testing.T) {
	defer cleanDB(t)

	ctx := context.Background()

	// Create two domains and verify they have different keys
	domain1, err := dm.CreateDomain(ctx, "example1.com")
	require.NoError(t, err)

	domain2, err := dm.CreateDomain(ctx, "example2.com")
	require.NoError(t, err)

	assert.NotEqual(t, domain1.Key, domain2.Key, "API keys should be randomly generated")
	assert.NotEqual(t, domain1.DkimPrivateKey, domain2.DkimPrivateKey, "DKIM keys should be unique")
}

func TestFindDomain(t *testing.T) {
	defer cleanDB(t)

	ctx := context.Background()

	// Create a domain
	created, err := dm.CreateDomain(ctx, "test.com")
	require.NoError(t, err)

	// Find it
	found, err := dm.FindDomain(ctx, "test.com")
	require.NoError(t, err)

	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, created.Domain, found.Domain)
	assert.Equal(t, created.Key, found.Key)
}

func TestFindDomain_NotFound(t *testing.T) {
	defer cleanDB(t)

	ctx := context.Background()

	_, err := dm.FindDomain(ctx, "nonexistent.com")
	assert.Error(t, err, "Should return error for nonexistent domain")
}

func TestFindDomainWithKey(t *testing.T) {
	defer cleanDB(t)

	ctx := context.Background()

	// Create a domain
	created, err := dm.CreateDomain(ctx, "auth-test.com")
	require.NoError(t, err)

	// Find with correct key
	found, err := dm.FindDomainWithKey(ctx, "auth-test.com", created.Key)
	require.NoError(t, err)

	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, created.Domain, found.Domain)
}

func TestFindDomainWithKey_WrongKey(t *testing.T) {
	defer cleanDB(t)

	ctx := context.Background()

	// Create a domain
	_, err := dm.CreateDomain(ctx, "auth-test.com")
	require.NoError(t, err)

	// Try to find with wrong key
	_, err = dm.FindDomainWithKey(ctx, "auth-test.com", "wrong-key")
	assert.Error(t, err, "Should return error for wrong API key")
}

func TestGetAllDomains(t *testing.T) {
	defer cleanDB(t)

	ctx := context.Background()

	// Create multiple domains
	_, err := dm.CreateDomain(ctx, "domain1.com")
	require.NoError(t, err)

	_, err = dm.CreateDomain(ctx, "domain2.com")
	require.NoError(t, err)

	_, err = dm.CreateDomain(ctx, "domain3.com")
	require.NoError(t, err)

	// Get all
	domains, err := dm.GetAllDomains(ctx)
	require.NoError(t, err)

	assert.Equal(t, 3, len(domains), "Should have 3 domains")
}

func TestClose(t *testing.T) {
	// Close should not return an error
	err := dm.Close()
	assert.NoError(t, err)
}
