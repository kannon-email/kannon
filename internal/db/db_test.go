package sqlc

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/ludusrusso/kannon/db"
	"github.com/ludusrusso/kannon/internal/tests"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	_ "github.com/lib/pq"
)

var db *pgxpool.Pool
var q *Queries

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		logrus.Fatalf("Could not start resource: %s", err)
	}

	q = New(db)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		logrus.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

func TestDomains(t *testing.T) {
	// when user create a domain
	domain, err := q.CreateDomain(context.Background(), CreateDomainParams{
		Domain:         "test@test.com",
		Key:            "test",
		DkimPrivateKey: "test",
		DkimPublicKey:  "test",
	})
	assert.Nil(t, err)
	assert.Equal(t, domain.Domain, "test@test.com")

	// can list all domains present
	domains, err := q.GetDomains(context.Background())
	assert.Nil(t, err)
	assert.Equal(t, len(domains), 1)

	// can search a domain for domain
	d, err := q.FindDomain(context.Background(), "test@test.com")
	assert.Nil(t, err)
	assert.Equal(t, d.ID, domain.ID)

	// can search a domain for domain key
	d, err = q.FindDomainWithKey(context.Background(), FindDomainWithKeyParams{
		Domain: "test@test.com",
		Key:    "test",
	})
	assert.Nil(t, err)
	assert.Equal(t, d.ID, domain.ID)

	// cleanup
	_, err = db.Exec(context.Background(), "TRUNCATE domains")
	assert.Nil(t, err)
}

func TestTemplates(t *testing.T) {
	domain, err := q.CreateDomain(context.Background(), CreateDomainParams{
		Domain:         "test@test.com",
		Key:            "test",
		DkimPrivateKey: "test",
		DkimPublicKey:  "test",
	})
	assert.Nil(t, err)
	assert.Equal(t, domain.Domain, "test@test.com")

	template, err := q.CreateTemplate(context.Background(), CreateTemplateParams{
		TemplateID: "template id",
		Html:       "template",
		Domain:     domain.Domain,
		Type:       TemplateTypeTransient,
	})
	assert.Nil(t, err)
	assert.Equal(t, template.Html, "template")

	tmp, err := q.FindTemplate(context.Background(), FindTemplateParams{
		TemplateID: "template id",
		Domain:     domain.Domain,
	})
	assert.Nil(t, err)
	assert.Equal(t, template, tmp)

	// cleanup
	_, err = db.Exec(context.Background(), "TRUNCATE templates")
	assert.Nil(t, err)
	_, err = db.Exec(context.Background(), "TRUNCATE domains")
	assert.Nil(t, err)
}
