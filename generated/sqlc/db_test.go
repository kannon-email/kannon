package sqlc

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

var q *Queries

func TestMain(m *testing.M) {
	// uses a sensible default on windows (tcp/http) and linux/osx (socket)
	pool, err := dockertest.NewPool("")
	if err != nil {
		logrus.Fatalf("Could not connect to docker: %s", err)
	}

	cdir, err := os.Getwd()
	if err != nil {
		logrus.Fatalf("Could not find current directory: %s", err)
	}

	fmt.Printf("%v/../../db/schema.sql:/docker-entrypoint-initdb.d/schema.sql", cdir)

	// pulls an image, creates a container based on it and runs it
	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "13-alpine",
		Env: []string{
			"POSTGRES_USER=test",
			"POSTGRES_PASSWORD=test",
			"listen_addresses = '*'",
		},
		Mounts: []string{
			fmt.Sprintf("%v/../../db/schema.sql:/docker-entrypoint-initdb.d/schema.sql", cdir),
		},
	}, func(config *docker.HostConfig) {
		// set AutoRemove to true so that stopped container goes away by itself
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	// resource, err := pool.Run("postgres", "9.6", []string{})
	if err != nil {
		logrus.Fatalf("Could not start resource: %s", err)
	}

	var db *sql.DB

	if err = pool.Retry(func() error {
		var err error
		db, err = initDb(resource.GetPort("5432/tcp"))
		if err != nil {
			logrus.Warnf("connection error: %v", err)
			return err
		}
		return db.Ping()
	}); err != nil {
		logrus.Fatalf("Could not connect to docker: %s", err)
	}
	q = New(db)

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := pool.Purge(resource); err != nil {
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
	_, err = q.db.ExecContext(context.Background(), "TRUNCATE domains")
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
	_, err = q.db.ExecContext(context.Background(), "TRUNCATE templates")
	assert.Nil(t, err)
	_, err = q.db.ExecContext(context.Background(), "TRUNCATE domains")
	assert.Nil(t, err)
}

func initDb(dbPort string) (*sql.DB, error) {
	url := fmt.Sprintf("postgresql://test:test@localhost:%v/test", dbPort)
	db, err := conn(url)
	if err != nil {
		return nil, err
	}
	return db, err
}
