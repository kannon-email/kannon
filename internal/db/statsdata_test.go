package sqlc

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/values"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStatsDataGoldenShape pins the exact document each outcome is stored as.
//
// ADR 0012 concedes that the on-disk format stays protojson-shaped and calls
// that concession an intention unless something asserts it. This is the
// assertion. The literals below are what the protojson path wrote, so a change
// to any of them is a change to the format of a live column and has to arrive
// with a migration — which is the prompt nobody got while a .proto defined the
// column.
//
// Bytes rather than semantic equality on purpose: jsonb would forgive key order
// and whitespace, but a golden test that forgives them cannot fail loudly enough
// to be noticed, and encoding/json is deterministic so there is nothing to gain
// by being lenient here.
func TestStatsDataGoldenShape(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome stats.Outcome
		want    string
	}{
		{"accepted", stats.Accepted(), `{"accepted":{}}`},
		{"delivered", stats.Delivered(), `{"delivered":{}}`},
		{"rejected", stats.Rejected("bad addr"), `{"rejected":{"reason":"bad addr"}}`},
		{"failed", stats.Failed("budget spent"), `{"failed":{"reason":"budget spent"}}`},
		{"bounced", stats.Bounced(true, 550, "no such user"), `{"bounced":{"permanent":true,"code":550,"msg":"no such user"}}`},
		{"error", stats.Errored(421, "try later"), `{"error":{"code":421,"msg":"try later"}}`},
		{"opened", stats.Opened("curl/8", "1.2.3.4"), `{"opened":{"userAgent":"curl/8","ip":"1.2.3.4"}}`},
		{"clicked", stats.Clicked("curl/8", "1.2.3.4", "https://example.com/a"), `{"clicked":{"userAgent":"curl/8","ip":"1.2.3.4","url":"https://example.com/a"}}`},

		// Zero-valued scalars are omitted rather than written out. That is
		// protojson's default and therefore what the rows already on disk look
		// like: {"bounced":{}} is a real stored bounce, and an engagement event
		// under any Mode below Full retains nothing and is stored as {"opened":{}}.
		{"rejected/no reason", stats.Rejected(""), `{"rejected":{}}`},
		{"bounced/all zero", stats.Bounced(false, 0, ""), `{"bounced":{}}`},
		{"opened/nothing retained", stats.Opened("", ""), `{"opened":{}}`},
		{"clicked/url only", stats.Clicked("", "", "https://example.com/"), `{"clicked":{"url":"https://example.com/"}}`},

		// An outcome no build can name is the empty document, which is what an
		// unset protobuf oneof rendered as and what the NOT NULL column accepts.
		{"unknown", stats.Outcome{}, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(StatsDataFromOutcome(tc.outcome))
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))

			// The same literal must read back as the outcome it was written from,
			// so the golden set doubles as the decoder's fixture.
			var back StatsData
			require.NoError(t, json.Unmarshal([]byte(tc.want), &back))
			assert.Equal(t, tc.outcome, back.Outcome())
		})
	}
}

// TestStatsDataToleratesUnknownKeys documents the one behaviour that is
// deliberately not a reproduction of the protojson reader. protojson.Unmarshal
// was called with no options and so refused unknown fields outright: a variant
// added by a newer build would have failed the whole query, not just the row.
// encoding/json ignores what it does not know, and that is the wanted answer —
// the kind of an event is recorded separately in stats.type, so a row this build
// cannot decode still reports what it was (ADR 0012).
func TestStatsDataToleratesUnknownKeys(t *testing.T) {
	var unknownVariant StatsData
	require.NoError(t, json.Unmarshal([]byte(`{"complained":{"reason":"spam"}}`), &unknownVariant))
	assert.Equal(t, stats.TypeUnknown, unknownVariant.Outcome().Type())

	var unknownField StatsData
	require.NoError(t, json.Unmarshal([]byte(`{"bounced":{"code":550,"retryable":false}}`), &unknownField))
	assert.Equal(t, stats.Bounced(false, 550, ""), unknownField.Outcome())
}

// TestStatsDataReadsRowsWrittenByTheProtojsonPath is the check that says no
// migration is needed, against a real Postgres rather than against Go's opinion
// of what the column holds.
//
// The documents below are written into the column as raw JSON exactly as the
// build that used protojson wrote them, not produced by StatsData, so the test
// keeps proving something after the type it is protecting has been refactored:
// it is a fixture of the past, and the past cannot be regenerated. They then
// come back out through the repository, which is the path every reader of these
// rows actually takes.
func TestStatsDataReadsRowsWrittenByTheProtojsonPath(t *testing.T) {
	ctx := t.Context()
	domain := values.MustParse(fmt.Sprintf("protojson-rows-%d.test", time.Now().UnixNano()))
	at := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)

	rows := []struct {
		name string
		// stored is the literal jsonb document, as the protojson path emitted it.
		stored string
		// statsType is what the type column of such a row holds; it is written
		// from the outcome at insert time and read straight back out, so the two
		// columns are checked together.
		statsType StatsType
		want      stats.Outcome
	}{
		{"accepted", `{"accepted":{}}`, StatsTypeAccepted, stats.Accepted()},
		{"delivered", `{"delivered":{}}`, StatsTypeDelivered, stats.Delivered()},
		{"rejected", `{"rejected":{"reason":"bad addr"}}`, StatsTypeRejected, stats.Rejected("bad addr")},
		{"rejected/no reason", `{"rejected":{}}`, StatsTypeRejected, stats.Rejected("")},
		{"failed", `{"failed":{"reason":"budget spent"}}`, StatsTypeFailed, stats.Failed("budget spent")},
		{"bounced", `{"bounced":{"permanent":true,"code":550,"msg":"no such user"}}`, StatsTypeBounce, stats.Bounced(true, 550, "no such user")},
		{"bounced/all zero", `{"bounced":{}}`, StatsTypeBounce, stats.Bounced(false, 0, "")},
		{"error", `{"error":{"code":421,"msg":"try later"}}`, StatsTypeError, stats.Errored(421, "try later")},
		{"opened", `{"opened":{"userAgent":"curl/8","ip":"1.2.3.4"}}`, StatsTypeOpened, stats.Opened("curl/8", "1.2.3.4")},
		{"opened/nothing retained", `{"opened":{}}`, StatsTypeOpened, stats.Opened("", "")},
		{"clicked", `{"clicked":{"userAgent":"curl/8","ip":"1.2.3.4","url":"https://example.com/a?b=c&d=e"}}`, StatsTypeClicked, stats.Clicked("curl/8", "1.2.3.4", "https://example.com/a?b=c&d=e")},
		// Not something Kannon wrote, but something Postgres may hand back: jsonb
		// reparses and re-serialises every document, so nothing about the bytes
		// that went in is guaranteed to survive except their meaning.
		{"reordered and padded", `{  "bounced" : { "msg" : "reordered" , "code" : 451 , "permanent" : false }  }`, StatsTypeBounce, stats.Bounced(false, 451, "reordered")},
	}

	for i, row := range rows {
		_, err := db.Exec(ctx,
			`INSERT INTO stats (email, message_id, type, timestamp, domain, data) VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
			fmt.Sprintf("r%d@example.com", i), "batch-1", string(row.statsType), at, domain.String(), row.stored,
		)
		require.NoError(t, err, "inserting %s", row.name)
	}

	repo := NewStatsRepository(db)
	got, err := repo.Query(ctx, domain,
		stats.TimeRange{Start: at.Add(-time.Hour), Stop: at.Add(time.Hour)},
		stats.Pagination{Limit: len(rows) + 1, Offset: 0},
	)
	require.NoError(t, err)
	require.Len(t, got, len(rows))

	byEmail := make(map[string]*stats.Stat, len(got))
	for _, s := range got {
		byEmail[s.Email] = s
	}

	for i, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			s := byEmail[fmt.Sprintf("r%d@example.com", i)]
			require.NotNil(t, s)
			assert.Equal(t, row.want, s.Outcome)
			assert.Equal(t, stats.Type(row.statsType), s.Type)
		})
	}
}
