package sqlc

import (
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/kannon-email/kannon/db"
	"github.com/kannon-email/kannon/internal/apikeys"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var db *pgxpool.Pool
var q *Queries

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		slog.Error("Could not start resource", "err", err)
		os.Exit(1)
	}

	q = New(db)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		slog.Error("Could not purge resource", "err", err)
		os.Exit(1)
	}

	os.Exit(code)
}

func TestDomains(t *testing.T) {
	// when user create a domain
	domain, err := q.CreateDomain(t.Context(), CreateDomainParams{
		Domain:         "test@test.com",
		DkimPrivateKey: "test",
		DkimPublicKey:  "test",
	})
	assert.Nil(t, err)
	assert.Equal(t, domain.Domain, "test@test.com")

	// can list all domains present
	domains, err := q.GetDomains(t.Context())
	assert.Nil(t, err)
	assert.Equal(t, len(domains), 1)

	// can search a domain for domain
	d, err := q.FindDomain(t.Context(), "test@test.com")
	assert.Nil(t, err)
	assert.Equal(t, d.ID, domain.ID)

	// cleanup
	_, err = db.Exec(t.Context(), "DELETE FROM domains")
	assert.Nil(t, err)
}

func TestHashKeyMatchesPostgres(t *testing.T) {
	inputs := []string{
		"k_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789abcdefghijklmnopqrstuvwxyz01",
		"hello-world",
		"",
	}

	for _, input := range inputs {
		t.Run(fmt.Sprintf("input=%q", input), func(t *testing.T) {
			goHash := apikeys.HashKey(input)

			var pgHash string
			err := db.QueryRow(t.Context(),
				"SELECT encode(digest($1, 'sha256'), 'hex')", input,
			).Scan(&pgHash)
			require.NoError(t, err)

			assert.Equal(t, goHash, pgHash)
		})
	}
}

func TestTemplates(t *testing.T) {
	domain, err := q.CreateDomain(t.Context(), CreateDomainParams{
		Domain:         "test@test.com",
		DkimPrivateKey: "test",
		DkimPublicKey:  "test",
	})
	assert.Nil(t, err)
	assert.Equal(t, domain.Domain, "test@test.com")

	template, err := q.CreateTemplate(t.Context(), CreateTemplateParams{
		TemplateID: "template id",
		Html:       "template",
		Domain:     domain.Domain,
		Type:       TemplateTypeTransient,
	})
	assert.Nil(t, err)
	assert.Equal(t, template.Html, "template")

	tmp, err := q.FindTemplate(t.Context(), FindTemplateParams{
		TemplateID: "template id",
		Domain:     domain.Domain,
	})
	assert.Nil(t, err)
	assert.Equal(t, template, tmp)

	// cleanup
	_, err = db.Exec(t.Context(), "DELETE FROM templates")
	assert.Nil(t, err)
	_, err = db.Exec(t.Context(), "DELETE FROM domains")
	assert.Nil(t, err)
}
