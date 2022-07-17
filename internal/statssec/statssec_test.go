package statssec_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	schema "github.com/ludusrusso/kannon/db"
	sqlc "github.com/ludusrusso/kannon/internal/db"
	"github.com/ludusrusso/kannon/internal/statssec"
	"github.com/ludusrusso/kannon/internal/tests"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

var db *sql.DB
var q *sqlc.Queries
var s statssec.StatsService

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		logrus.Fatalf("Could not start resource: %s", err)
	}

	q = sqlc.New(db)
	s = statssec.NewStatsService(q)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := purge(); err != nil {
		logrus.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

func TestCreateOpenToken(t *testing.T) {
	// when user create a domain
	token, err := s.CreateOpenToken(context.Background(), "<xxxx/test@test.com>", "test@test.com")
	assert.Nil(t, err)

	assert.NotEmpty(t, token)

	c, err := s.VertifyOpenToken(context.Background(), token)
	assert.Nil(t, err)

	assert.Equal(t, "<xxxx/test@test.com>", c.MessageID)
	assert.Equal(t, "test@test.com", c.Email)
}

func TestCreateLinkToken(t *testing.T) {
	// when user create a domain
	token, err := s.CreateLinkToken(context.Background(), "<xxxx/test@test.com>", "test@test.com", "https://test.com")
	assert.Nil(t, err)

	assert.NotEmpty(t, token)

	c, err := s.VertifyLinkToken(context.Background(), token)
	assert.Nil(t, err)

	assert.Equal(t, "<xxxx/test@test.com>", c.MessageID)
	assert.Equal(t, "test@test.com", c.Email)
	assert.Equal(t, "https://test.com", c.Url)
}
