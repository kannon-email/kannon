package audit

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/kannon-email/kannon/db"
	"github.com/kannon-email/kannon/internal/audit"
	"github.com/kannon-email/kannon/internal/authz"
	sq "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/kannon-email/kannon/x/container"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var db *pgxpool.Pool

func TestMain(m *testing.M) {
	var purge tests.PurgeFunc
	var err error

	db, purge, err = tests.TestPostgresInit(schema.Schema)
	if err != nil {
		slog.Error("Could not start resource", "err", err)
		os.Exit(1)
	}

	code := m.Run()

	if err := purge(); err != nil {
		slog.Error("Could not purge resource", "err", err)
		os.Exit(1)
	}

	os.Exit(code)
}

const testRetention = 720 * time.Hour

func newTestHandler() auditHandler {
	return auditHandler{
		repo:      sq.NewAuditRepository(db),
		retention: testRetention,
	}
}

// fakeMsg stands in for a JetStream message so a test can assert how the handler settles it: Ack,
// Term and Nak are the difference between finished with, abandoned on purpose, and straight back —
// and a permanent fault that comes back is the #396 hot loop. jetstream.Msg is embedded to panic.
type fakeMsg struct {
	jetstream.Msg
	data   []byte
	acked  int
	termed int
	naked  int
}

func (m *fakeMsg) Data() []byte { return m.data }
func (m *fakeMsg) Ack() error   { m.acked++; return nil }
func (m *fakeMsg) Term() error  { m.termed++; return nil }
func (m *fakeMsg) Nak() error   { m.naked++; return nil }

func (m *fakeMsg) NakWithDelay(time.Duration) error { m.naked++; return nil }

// decisionMsg is one Audit Record on the wire as the producer's Recorder publishes it. Built through
// Marshal rather than by hand-writing JSON, so a test asserts against the shape the producer really
// sends and not against a second spelling of it that could drift.
func decisionMsg(t *testing.T, r audit.Record) *fakeMsg {
	t.Helper()

	data, err := audit.Marshal(r)
	require.NoError(t, err)

	return &fakeMsg{data: data}
}

// aRecord is a permitted decision, complete enough to be worth a row. Every test that cares about
// one field overrides it and leaves the rest alone.
func aRecord(t *testing.T) audit.Record {
	t.Helper()

	return audit.Record{
		ID:         utils.NewID("audit"),
		OccurredAt: time.Now().UTC(),
		Action:     authz.Read,
		Outcome:    authz.Allowed,
		Principal:  "admin-token",
		Resource:   []string{"domains", tests.FakeDomain(t)},
		Details: audit.Details{
			Attribution: authz.Attribution("alice@operator.test"),
			Grants:      []string{"admin@*"},
		},
	}
}

// storedRecords counts what the writer left behind for one Record's identifier. Queried straight
// against Postgres because audit.Repository is deliberately write-only: nothing in Kannon reads this
// register, so there is no accessor for a test to reach for either.
func storedRecords(t *testing.T, id string) int {
	t.Helper()

	var count int
	err := db.QueryRow(t.Context(), "SELECT COUNT(*) FROM audit_records WHERE id = $1", id).Scan(&count)
	require.NoError(t, err)
	return count
}

func cleanDB(t *testing.T) {
	t.Helper()

	_, err := db.Exec(t.Context(), "DELETE FROM audit_records")
	require.NoError(t, err)
}

// errDatabaseGone is the transient failure a Nak exists for.
var errDatabaseGone = errors.New("connection refused")

// failingRepository is a database that will not take a write. Needed because both real repositories
// succeed, and the settlement a transient failure earns — back onto the stream rather than dropped —
// is the whole reason a Record survives a database that is briefly gone.
type failingRepository struct {
	err error
}

func (r failingRepository) Insert(context.Context, audit.Record) error {
	return r.err
}

func (r failingRepository) DeleteOlderThan(context.Context, time.Time) (int64, error) {
	return 0, r.err
}

// TestTheWriterRefusesToStartWhenCollectionIsOff is the flag and the key being two halves of one
// switch: the writer alone consumes nothing, because audit.enabled governs the producer. The worker
// stops rather than sitting against an empty stream, and it names the key so the operator can tell
// which half they are missing.
//
// Returning nil and not an error is what is asserted here: every runnable shares one errgroup, so an
// error would take the API down with it, and `kannon standalone` sets every flag while audit.enabled
// stays false — an error would make standalone refuse to boot over a feature nobody asked for.
func TestTheWriterRefusesToStartWhenCollectionIsOff(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	logged := captureLogs(t)

	// A container with no database URL and no NATS URL: if the runnable reached for either, the
	// test would fail rather than quietly connect to whatever happens to be running.
	cnt := container.NewForTest(t.Context())

	runnable := New(cnt)
	assert.Equal(t, "audit", runnable.Name)

	done := make(chan error, 1)
	go func() { done <- runnable.Run(t.Context()) }()

	select {
	case err := <-done:
		assert.NoError(t, err, "a feature that is off must not fail the process it shares an errgroup with")
	case <-time.After(5 * time.Second):
		t.Fatal("the writer did not stop: it is consuming a stream nothing publishes to")
	}

	out := logged()
	assert.Contains(t, out, `"level":"WARN"`, "the operator must be told, got %q", out)
	assert.Contains(t, out, "audit.enabled", "the warning must name the key to set, got %q", out)
}

// captureLogs redirects the default logger for the duration of one test and returns everything
// written to it, so a test can assert that an operator is in fact told.
func captureLogs(t *testing.T) func() string {
	t.Helper()

	buf := &bytes.Buffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return func() string {
		return strings.TrimSpace(buf.String())
	}
}
