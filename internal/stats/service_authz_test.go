package stats_test

import (
	"context"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/authz"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/proto/kannon/stats/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Principals, one Grant each, named for the authority they hold. exampleCom is the
// Domain being read; aCom stands in for a Domain the caller genuinely administers and
// did not ask about, which is what separates "wrong place" from "no authority".
var (
	rootAdmin        = authz.MustNewPrincipal("root-admin", authz.MustNewGrant(authz.RoleAdmin, authz.RootAnchor()))
	everyDomainAdmin = authz.MustNewPrincipal("every-domain-admin", authz.MustNewGrant(authz.RoleAdmin, authz.AllDomainsAnchor()))
	homeDomainAdmin  = authz.MustNewPrincipal("home-domain-admin", authz.MustNewGrant(authz.RoleAdmin, authz.DomainAnchor(exampleCom)))
	otherDomainAdmin = authz.MustNewPrincipal("other-domain-admin", authz.MustNewGrant(authz.RoleAdmin, authz.DomainAnchor(aCom)))
	senderOnly       = authz.MustNewPrincipal("sender-only", authz.MustNewGrant(authz.RoleSender, authz.DomainAnchor(exampleCom)))
	noGrants         = authz.MustNewPrincipal("no-grants")
)

// TestServiceAuthorization is the table that says what each read demands. The sender-only Principal
// is refused by all three, which is the disclosure this slice closes on the statistics side: a row
// carries the Recipient's address and, under Full, an IP and user agent.
func TestServiceAuthorization(t *testing.T) {
	tr := stats.TimeRange{
		Start: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Stop:  time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC),
	}

	ops := []struct {
		name  string
		call  func(context.Context, *stats.Service) error
		allow []authz.Principal
		deny  []authz.Principal
	}{
		{
			// List on the Domain's Stats: it enumerates the per-Delivery rows.
			name: "QueryStats",
			call: func(ctx context.Context, s *stats.Service) error {
				_, _, err := s.QueryStats(ctx, exampleCom, tr, stats.Pagination{Limit: 10})
				return err
			},
			allow: []authz.Principal{rootAdmin, everyDomainAdmin, homeDomainAdmin},
			deny:  []authz.Principal{otherDomainAdmin, senderOnly, noGrants},
		},
		{
			// Read on the Domain's Stats, not on the counters beneath them: v1's
			// timeline is computed by grouping the per-Delivery rows, so it requires
			// authority over what it is computed from.
			name: "QueryTimeline",
			call: func(ctx context.Context, s *stats.Service) error {
				_, err := s.QueryTimeline(ctx, exampleCom, tr)
				return err
			},
			allow: []authz.Principal{rootAdmin, everyDomainAdmin, homeDomainAdmin},
			deny:  []authz.Principal{otherDomainAdmin, senderOnly, noGrants},
		},
		{
			// Read on the Domain's AggregatedStats, which carry no personal data and so
			// have their own node beneath Stats.
			name: "QueryAggregatedStats",
			call: func(ctx context.Context, s *stats.Service) error {
				_, err := s.QueryAggregatedStats(ctx, exampleCom, tr)
				return err
			},
			allow: []authz.Principal{rootAdmin, everyDomainAdmin, homeDomainAdmin},
			deny:  []authz.Principal{otherDomainAdmin, senderOnly, noGrants},
		},
	}

	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			for _, p := range op.allow {
				t.Run("proceeds for "+p.ID(), func(t *testing.T) {
					err := op.call(authz.NewContext(t.Context(), p), seededService(t))
					require.NoError(t, err)
				})
			}

			for _, p := range op.deny {
				t.Run("refuses "+p.ID(), func(t *testing.T) {
					err := op.call(authz.NewContext(t.Context(), p), seededService(t))
					assert.ErrorIs(t, err, authz.ErrForbidden)
				})
			}

			// Nothing authenticated the request, which is what both Stats APIs are left
			// with when the admin token interceptor refuses one.
			t.Run("refuses a request with no Principal", func(t *testing.T) {
				err := op.call(t.Context(), seededService(t))
				assert.ErrorIs(t, err, authz.ErrNoPrincipal)
			})
		})
	}
}

// A refusal must not leak the rows it refused. Asserted apart from the table because
// the error alone does not say what came back with it: a guard returning both a
// refusal and the data would satisfy every assertion above.
func TestRefusedReadsReturnNothing(t *testing.T) {
	service := seededService(t)
	refused := authz.NewContext(context.Background(), senderOnly)
	tr := stats.TimeRange{
		Start: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Stop:  time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC),
	}

	rows, total, err := service.QueryStats(refused, exampleCom, tr, stats.Pagination{Limit: 10})
	assert.ErrorIs(t, err, authz.ErrForbidden)
	assert.Empty(t, rows, "a refused read must disclose no Recipient")
	assert.Zero(t, total, "a refused read must not even disclose how many there are")

	timeline, err := service.QueryTimeline(refused, exampleCom, tr)
	assert.ErrorIs(t, err, authz.ErrForbidden)
	assert.Empty(t, timeline)

	counters, err := service.QueryAggregatedStats(refused, exampleCom, tr)
	assert.ErrorIs(t, err, authz.ErrForbidden)
	assert.Empty(t, counters)
}

// Recording an event is unguarded, because its callers are Kannon's own workers consuming NATS and
// there is no request to authorize. This pins that: a guard added there would stop the Stats worker
// dead, and only in production, where nothing puts a Principal in a worker's context.
func TestWritesNeedNoPrincipal(t *testing.T) {
	service := stats.NewService(stats.NewInMemRepository(),
		stats.WithAggregatedStatsRepository(stats.NewInMemAggregatedStatsRepository()))
	ts := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	stat := stats.NewStat("u@example.com", "msg-worker", exampleCom, ts, &types.StatsData{
		Data: &types.StatsData_Delivered{},
	})
	require.NoError(t, service.InsertStat(context.Background(), stat))
	require.NoError(t, service.IncrementAggregatedStat(context.Background(), exampleCom, ts, stats.TypeDelivered))

	_, err := service.Cleanup(context.Background(), time.Hour)
	require.NoError(t, err)
}

// seededService returns a Service holding one stat and one counter for exampleCom, so
// that an authorized read has something to return and a refused one has something it
// could have leaked.
func seededService(t *testing.T) *stats.Service {
	t.Helper()

	service := stats.NewService(stats.NewInMemRepository(),
		stats.WithAggregatedStatsRepository(stats.NewInMemAggregatedStatsRepository()))
	ts := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	stat := stats.NewStat("u@example.com", "msg-seeded", exampleCom, ts, &types.StatsData{
		Data: &types.StatsData_Delivered{},
	})
	require.NoError(t, service.InsertStat(t.Context(), stat))
	require.NoError(t, service.IncrementAggregatedStat(t.Context(), exampleCom, ts, stats.TypeDelivered))

	return service
}
