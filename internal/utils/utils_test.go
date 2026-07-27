package utils_test

import (
	"context"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The deadline a consumer is really given is the one the server ends up
// holding, and the server rewrites AckWait to BackOff[0]. Asserting against
// the config read back is therefore the only assertion worth making: it is
// what caught #425, where the requested AckWait and the effective one were
// thirty seconds apart.
func TestAckPolicyIsTheDeadlineTheServerHolds(t *testing.T) {
	ctx := t.Context()
	js := tests.NatsJetStream(t)
	mustStream(ctx, t, js)

	policy := utils.AckPolicy{90 * time.Second, 3 * time.Minute}
	info := consumerInfo(ctx, t, js, "explicit-policy", utils.WithAckPolicy(policy))

	assert.Equal(t, policy.FirstDeadline(), info.Config.AckWait)
	assert.Equal(t, []time.Duration(policy), info.Config.BackOff)
}

// Without an explicit policy a consumer gets the fast curve, which is right
// for a handler that acks within milliseconds and wrong for anything else —
// the property that makes WithAckPolicy necessary rather than decorative.
func TestConsumerDefaultsToTheFastAckPolicy(t *testing.T) {
	ctx := t.Context()
	js := tests.NatsJetStream(t)
	mustStream(ctx, t, js)

	info := consumerInfo(ctx, t, js, "default-policy")

	assert.Equal(t, utils.DefaultAckPolicy.FirstDeadline(), info.Config.AckWait)
	assert.Equal(t, []time.Duration(utils.DefaultAckPolicy), info.Config.BackOff)
}

func consumerInfo(ctx context.Context, t *testing.T, js jetstream.JetStream, durable string, opts ...utils.ConsumerOption) *jetstream.ConsumerInfo {
	t.Helper()
	con := utils.MustGetPullSubscriber(ctx, js, "test-stream", "test.subject", durable, opts...)
	info, err := con.Info(ctx)
	require.NoError(t, err)
	return info
}

func mustStream(ctx context.Context, t *testing.T, js jetstream.JetStream) {
	t.Helper()
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "test-stream",
		Subjects: []string{"test.subject"},
	})
	require.NoError(t, err)
}
