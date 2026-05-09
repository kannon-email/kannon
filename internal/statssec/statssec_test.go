package statssec_test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/kannon-email/kannon/db"
	sqlc "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/stretchr/testify/assert"
)

var db *pgxpool.Pool
var q *sqlc.Queries
var s statssec.StatsService

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		slog.Error("Could not start resource", "err", err)
		os.Exit(1)
	}

	q = sqlc.New(db)
	s = statssec.NewStatsService(q)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		slog.Error("Could not purge resource", "err", err)
		os.Exit(1)
	}

	os.Exit(code)
}

func TestCreateOpenToken(t *testing.T) {
	// when user create a domain
	token, err := s.CreateOpenToken(t.Context(), "<xxxx/test@test.com>", "test@test.com")
	assert.Nil(t, err)

	assert.NotEmpty(t, token)

	c, err := s.VerifyOpenToken(t.Context(), token)
	assert.Nil(t, err)

	assert.Equal(t, "<xxxx/test@test.com>", c.MessageID)
	assert.Equal(t, "test@test.com", c.Email)
}

func TestCreateLinkToken(t *testing.T) {
	// when user create a domain
	token, err := s.CreateLinkToken(t.Context(), "<xxxx/test@test.com>", "test@test.com", "https://test.com")
	assert.Nil(t, err)

	assert.NotEmpty(t, token)

	c, err := s.VerifyLinkToken(t.Context(), token)
	assert.Nil(t, err)

	assert.Equal(t, "<xxxx/test@test.com>", c.MessageID)
	assert.Equal(t, "test@test.com", c.Email)
	assert.Equal(t, "https://test.com", c.URL)
}
