package sq

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/ludusrusso/kannon/internal/tests"
	schema "github.com/ludusrusso/kannon/stats_db"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

var db *sql.DB
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

func TestPrepareQueries(t *testing.T) {
	q, err := Prepare(context.Background(), db)
	assert.Nil(t, err)
	defer q.Close()
}
