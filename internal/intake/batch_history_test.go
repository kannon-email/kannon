package intake_test

import (
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/intake"
)

func TestBatchHistoryStatus(t *testing.T) {
	done := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		b    intake.BatchHistory
		want intake.BatchStatus
	}{
		{"nothing started", intake.BatchHistory{Counts: intake.DeliveryCounts{intake.DeliveryPending: 2, intake.DeliveryScheduled: 1}}, intake.BatchPending},
		{"no counts", intake.BatchHistory{}, intake.BatchPending},
		{"sending", intake.BatchHistory{Counts: intake.DeliveryCounts{intake.DeliveryPending: 1, intake.DeliverySending: 1}}, intake.BatchProcessing},
		{"some outcomes", intake.BatchHistory{Counts: intake.DeliveryCounts{intake.DeliveryPending: 1, intake.DeliveryBounced: 1}}, intake.BatchProcessing},
		{"zero entries ignored", intake.BatchHistory{Counts: intake.DeliveryCounts{intake.DeliveryPending: 1, intake.DeliveryFailed: 0}}, intake.BatchPending},
		{"completed", intake.BatchHistory{CompletedAt: &done, Counts: intake.DeliveryCounts{intake.DeliveryDelivered: 1}}, intake.BatchCompleted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.b.Status(); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestBatchHistoryExpiresAfterRetention(t *testing.T) {
	if (intake.BatchHistory{}).ExpiresAt() != nil {
		t.Fatal("open Batch must not expire")
	}
	done := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := intake.BatchHistory{CompletedAt: &done}.ExpiresAt()
	if got == nil || !got.Equal(done.Add(30*24*time.Hour)) {
		t.Fatalf("got %v", got)
	}
	if !intake.HistoryCutoff(*got).Equal(done) {
		t.Fatal("cutoff must mirror expiry")
	}
}

func TestParseDeliveryStatus(t *testing.T) {
	for _, s := range []string{"pending", "scheduled", "sending", "delivered", "rejected", "bounced", "failed"} {
		if got, ok := intake.ParseDeliveryStatus(s); !ok || string(got) != s {
			t.Fatalf("%s rejected", s)
		}
	}
	if _, ok := intake.ParseDeliveryStatus("completed"); ok {
		t.Fatal("unknown status accepted")
	}
}
