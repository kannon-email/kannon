package stats

import (
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	schema "github.com/kannon-email/kannon/db"
	sq "github.com/kannon-email/kannon/internal/db"
	"github.com/kannon-email/kannon/internal/intake"
	"github.com/kannon-email/kannon/internal/runner"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/statssec"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var db *pgxpool.Pool
var q *sq.Queries

func TestMain(m *testing.M) {
	tests.Main(func() int {
		var purge tests.PurgeFunc
		var err error

		db, purge, err = tests.TestPostgresInit(schema.Schema)
		if err != nil {
			slog.Error("Could not start resource", "err", err)
			return 1
		}

		q = sq.New(db)

		code := m.Run()

		if err := purge(); err != nil {
			slog.Error("Could not purge resource", "err", err)
			return 1
		}

		return code
	})
}

const (
	testRetention           = 365 * 24 * time.Hour
	testAggregatedRetention = 2 * 365 * 24 * time.Hour
)

func newTestHandler() statsHandler {
	repo := sq.NewStatsRepository(db)
	service := stats.NewService(repo,
		stats.WithAggregatedStatsRepository(sq.NewAggregatedStatsRepository(db)))
	return statsHandler{
		service:             service,
		q:                   q,
		keys:                sq.NewStatsKeysRepository(db),
		retention:           testRetention,
		aggregatedRetention: testAggregatedRetention,
		counter:             sq.NewAggregatedStatsRepository(db),
	}
}

func TestCleanupCycle_DeletesOldStats(t *testing.T) {
	cleanDB(t)

	ctx := t.Context()

	// Insert an old stat (2 years ago)
	oldTime := time.Now().Add(-2 * 365 * 24 * time.Hour)
	err := q.InsertStat(ctx, sq.InsertStatParams{
		Email:     "old@test.com",
		MessageID: "msg-old",
		Timestamp: pgtype.Timestamp{Time: oldTime, Valid: true},
		Domain:    "test.com",
		Type:      sq.StatsTypeDelivered,
		Data:      sq.StatsDataFromOutcome(stats.Accepted()),
	})
	require.NoError(t, err)

	// Insert a recent stat (1 hour ago)
	recentTime := time.Now().Add(-1 * time.Hour)
	err = q.InsertStat(ctx, sq.InsertStatParams{
		Email:     "recent@test.com",
		MessageID: "msg-recent",
		Timestamp: pgtype.Timestamp{Time: recentTime, Valid: true},
		Domain:    "test.com",
		Type:      sq.StatsTypeDelivered,
		Data:      sq.StatsDataFromOutcome(stats.Accepted()),
	})
	require.NoError(t, err)

	h := newTestHandler() // 1 year

	err = runner.Run(ctx, h.cleanupCycle, runner.MaxLoop(1))
	require.NoError(t, err)

	// Only the recent stat should remain
	rows, err := q.CountQueryStats(ctx, sq.CountQueryStatsParams{
		Domain: "test.com",
		Start:  pgtype.Timestamp{Time: oldTime.Add(-time.Hour), Valid: true},
		Stop:   pgtype.Timestamp{Time: time.Now().Add(time.Hour), Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
}

func TestCleanupCycle_KeepsRecentStats(t *testing.T) {
	cleanDB(t)

	ctx := t.Context()

	// Insert two recent stats
	for _, email := range []string{"a@test.com", "b@test.com"} {
		err := q.InsertStat(ctx, sq.InsertStatParams{
			Email:     email,
			MessageID: "msg-" + email,
			Timestamp: pgtype.Timestamp{Time: time.Now().Add(-1 * time.Hour), Valid: true},
			Domain:    "test.com",
			Type:      sq.StatsTypeDelivered,
			Data:      sq.StatsDataFromOutcome(stats.Accepted()),
		})
		require.NoError(t, err)
	}

	h := newTestHandler()

	err := runner.Run(ctx, h.cleanupCycle, runner.MaxLoop(1))
	require.NoError(t, err)

	rows, err := q.CountQueryStats(ctx, sq.CountQueryStatsParams{
		Domain: "test.com",
		Start:  pgtype.Timestamp{Time: time.Now().Add(-2 * time.Hour), Valid: true},
		Stop:   pgtype.Timestamp{Time: time.Now().Add(time.Hour), Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), rows)
}

func TestCleanupCycle_DeletesExpiredStatsKeys(t *testing.T) {
	cleanDB(t)

	ctx := t.Context()

	// What an expired key is, and that sweeping spares a valid one, is the KeyRepository's
	// contract (RunKeyRepoSpec). Here the only question is whether the cycle sweeps at all.
	keys := statssec.NewInMemKeyRepository()
	require.NoError(t, keys.Create(ctx, statssec.Key{ID: "expired-key", ExpiresAt: time.Now().Add(-24 * time.Hour)}))
	require.NoError(t, keys.Create(ctx, statssec.Key{ID: "valid-key", PublicKey: "pub2", ExpiresAt: time.Now().Add(24 * time.Hour)}))

	h := newTestHandler()
	h.keys = keys

	err := runner.Run(ctx, h.cleanupCycle, runner.MaxLoop(1))
	require.NoError(t, err)

	assert.Equal(t, 1, keys.Len(), "the expired key should be swept and the valid one kept")
	public, err := keys.PublicKey(ctx, "valid-key")
	require.NoError(t, err)
	assert.Equal(t, "pub2", public)
}

func TestCleanupCycle_NoRowsToDelete(t *testing.T) {
	cleanDB(t)

	ctx := t.Context()

	h := newTestHandler()

	// Should not error on empty tables
	err := runner.Run(ctx, h.cleanupCycle, runner.MaxLoop(1))
	assert.NoError(t, err)
}

func TestCleanupCycle_DeletesDrainedMailerBatches(t *testing.T) {
	cleanDB(t)

	ctx := t.Context()
	old := time.Now().Add(-intake.HistoryRetention - time.Hour)
	recent := time.Now().Add(-intake.HistoryRetention + time.Hour)

	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := db.Exec(ctx, sql, args...)
		require.NoError(t, err)
	}
	template := func(id, kind string) {
		exec("INSERT INTO templates (template_id, html, domain, type) VALUES ($1, 'html', 'test.com', $2)", id, kind)
	}
	message := func(id, templateID string, createdAt time.Time) {
		exec(`INSERT INTO messages (message_id, subject, sender_email, sender_alias, template_id, domain, created_at)
			VALUES ($1, 'subject', 'from@test.com', 'From', $2, 'test.com', $3)`, id, templateID, createdAt)
	}

	template("tpl-drained", "transient")
	message("drained", "tpl-drained", old)
	template("tpl-shared", "transient")
	message("drained-shared", "tpl-shared", old)
	message("recent-shared", "tpl-shared", recent)
	template("tpl-persistent", "template")
	message("drained-persistent", "tpl-persistent", old)
	template("tpl-pending", "transient")
	message("pending", "tpl-pending", old)
	exec(`INSERT INTO sending_pool_emails (email, status, original_scheduled_time, message_id, domain)
		VALUES ('to@test.com', 'scheduled', now(), 'pending', 'test.com')`)
	template("tpl-rest", "transient")
	message("rest", "tpl-rest", old)
	exec(`INSERT INTO batch_intake_receipts (domain, idempotency_key, fingerprint, batch_id, created_at, accepted_count, rejected_count, scheduled_at)
		VALUES ('test.com', 'key', 'fp', 'rest', $1, 1, 0, $1)`, old)

	h := newTestHandler()
	require.NoError(t, runner.Run(ctx, h.cleanupCycle, runner.MaxLoop(1)))

	exists := func(table, column, id string) bool {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRow(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+column+" = $1", id).Scan(&n))
		return n > 0
	}
	assert.False(t, exists("messages", "message_id", "drained"))
	assert.False(t, exists("templates", "template_id", "tpl-drained"))
	assert.False(t, exists("messages", "message_id", "drained-shared"))
	assert.True(t, exists("messages", "message_id", "recent-shared"), "a Batch inside the window is kept")
	assert.True(t, exists("templates", "template_id", "tpl-shared"), "a Template another Batch uses is kept")
	assert.False(t, exists("messages", "message_id", "drained-persistent"))
	assert.True(t, exists("templates", "template_id", "tpl-persistent"), "a persistent Template is not the Batch's to delete")
	assert.True(t, exists("messages", "message_id", "pending"), "a Batch with Deliveries in the Pool is kept")
	assert.True(t, exists("templates", "template_id", "tpl-pending"))
	assert.True(t, exists("messages", "message_id", "rest"), "a REST Batch expires through its receipt")
	assert.True(t, exists("templates", "template_id", "tpl-rest"))
}

func TestCleanupCycle_DeletesOldAggregatedStats(t *testing.T) {
	cleanDB(t)

	ctx := t.Context()
	repo := sq.NewAggregatedStatsRepository(db)
	domain := values.MustParse("test.com")
	now := time.Now().UTC()
	expired := now.Add(-testAggregatedRetention - time.Hour)
	// Older than stats.retention, which must not govern the counters.
	kept := now.Add(-testRetention - time.Hour)
	require.NoError(t, repo.Increment(ctx, domain, expired, stats.TypeDelivered))
	require.NoError(t, repo.Increment(ctx, domain, kept, stats.TypeDelivered))

	h := newTestHandler()
	require.NoError(t, runner.Run(ctx, h.cleanupCycle, runner.MaxLoop(1)))

	rows, err := repo.Query(ctx, domain, stats.TimeRange{Start: expired.Add(-time.Hour), Stop: now})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, kept.Truncate(time.Hour), rows[0].Timestamp.Truncate(time.Hour))
}

func TestCleanupCycle_DeletesExpiredIntakeReceipts(t *testing.T) {
	cleanDB(t)

	ctx := t.Context()
	receipt := func(key string, completedAt *time.Time) {
		t.Helper()
		_, err := db.Exec(ctx, `INSERT INTO batch_intake_receipts (domain, idempotency_key, fingerprint, batch_id, created_at, accepted_count, rejected_count, scheduled_at, completed_at)
			VALUES ('test.com', $1, 'fp', $1, now() - interval '1 year', 1, 0, now() - interval '1 year', $2)`, key, completedAt)
		require.NoError(t, err)
	}
	expired := time.Now().Add(-intake.ReceiptRetention - time.Hour)
	recent := time.Now().Add(-intake.ReceiptRetention + time.Hour)
	receipt("expired", &expired)
	receipt("recent", &recent)
	receipt("open", nil)

	h := newTestHandler()
	require.NoError(t, runner.Run(ctx, h.cleanupCycle, runner.MaxLoop(1)))

	rows, err := db.Query(ctx, "SELECT idempotency_key FROM batch_intake_receipts ORDER BY idempotency_key")
	require.NoError(t, err)
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	assert.Equal(t, []string{"open", "recent"}, keys)
}

func TestCleanupCycle_PrunesCountedStatEvents(t *testing.T) {
	cleanDB(t)

	ctx := t.Context()
	repo := sq.NewAggregatedStatsRepository(db)
	domain := values.MustParse("test.com")
	now := time.Now()
	expired := stats.StoredEventID{Stream: stats.StreamName, StoredAt: now.Add(-stats.CountedEventRetention - time.Hour), Sequence: 1}
	live := stats.StoredEventID{Stream: stats.StreamName, StoredAt: now.Add(-stats.CountedEventRetention + time.Hour), Sequence: 2}
	require.NoError(t, repo.Count(ctx, expired, domain, now, stats.TypeOpened))
	require.NoError(t, repo.Count(ctx, live, domain, now, stats.TypeOpened))

	h := newTestHandler()
	require.NoError(t, runner.Run(ctx, h.cleanupCycle, runner.MaxLoop(1)))

	assert.Equal(t, []int64{2}, countedSequences(t))
	// The live marker still deduplicates a redelivery.
	require.NoError(t, repo.Count(ctx, live, domain, now, stats.TypeOpened))
	assert.Equal(t, int64(2), aggregatedCount(t, domain.String(), stats.TypeOpened))
}

// stored_at is RFC3339Nano text, whose trailing fractional zeros are trimmed: as text,
// "10:00:00.5Z" sorts before "10:00:00Z". The prune must compare instants, never delete a marker
// stored after the cutoff, and only ever keep one stored before it for less than a second.
func TestPruneCountedStatEvents_ComparesInstantsNotText(t *testing.T) {
	cleanDB(t)

	ctx := t.Context()
	repo := sq.NewAggregatedStatsRepository(db)
	domain := values.MustParse("test.com")
	cutoff := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	for i, at := range []time.Time{
		cutoff.Add(-500 * time.Millisecond), // 09:59:59.5Z, before the cutoff second
		cutoff,                              // 10:00:00Z
		cutoff.Add(500 * time.Millisecond),  // 10:00:00.5Z, after the cutoff
		cutoff.Add(time.Hour),
	} {
		id := stats.StoredEventID{Stream: stats.StreamName, StoredAt: at, Sequence: uint64(i + 1)}
		require.NoError(t, repo.Count(ctx, id, domain, at, stats.TypeOpened))
	}

	deleted, err := q.PruneCountedStatEvents(ctx, cutoff.Add(stats.CountedEventRetention))
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
	assert.Equal(t, []int64{2, 3, 4}, countedSequences(t))
}

func countedSequences(t *testing.T) []int64 {
	t.Helper()
	rows, err := db.Query(t.Context(), "SELECT sequence::bigint FROM counted_stat_events ORDER BY sequence")
	require.NoError(t, err)
	seqs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	require.NoError(t, err)
	return seqs
}

func cleanDB(t *testing.T) {
	t.Helper()
	ctx := t.Context()
	for _, table := range []string{"stats", "stats_keys", "aggregated_stats", "counted_stat_events", "sending_pool_emails", "batch_intake_receipts", "messages", "templates"} {
		_, err := db.Exec(ctx, "DELETE FROM "+table)
		require.NoError(t, err)
	}
}
