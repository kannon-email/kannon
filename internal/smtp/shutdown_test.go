package smtp

import (
	"context"
	"errors"
	"testing"
)

func TestDeliveryHonorsExpiredSharedBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (&sender{Hostname: "example.com"}).deliver(ctx, "from@example.com", "to@example.com", nil, "127.0.0.1", false)
	if err == nil || !errors.Is(err.err, context.Canceled) || err.IsPermanent() {
		t.Fatalf("expired send budget must cause a transient failure: %v", err)
	}
}
