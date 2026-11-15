package e2e_test

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kannon-email/kannon/internal/tests"
)

var bounceScenarios = map[string]func(*testing.T, *env){
	"PermanentBounce":         testPermanentBounce,
	"AsyncSoftBounce":         testAsyncSoftBounce,
	"TransientThenDelivered":  testTransientThenDelivered,
	"RetryBudgetEndsBounced":  testRetryBudgetEndsBounced,
	"ForgedDSNBouncesNothing": testForgedDSNBouncesNothing,
}

func testPermanentBounce(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	rcpt := fmt.Sprintf("bounce.%s@%s", tests.FakeUsername(t), tn.Domain)

	b := tn.Send(t, tn.Batch("Permanent Bounce Test", "<h1>Hello!</h1>", To(rcpt, nil)))

	bounced := tn.Await(t, rcpt, Bounced)
	assert.True(t, bounced.Data.Permanent, "a 5xx is a permanent bounce")
	assert.Equal(t, 550, bounced.Data.SMTPCode)

	done := tn.AwaitBatch(t, b, Completed)
	assert.Equal(t, 1, done.DeliveryCounts.Bounced)
	assert.Equal(t, 1, e.MX.Attempts(rcpt), "a permanent bounce is not retried")
}

// testAsyncSoftBounce is a Delivery reported Delivered, then a DSN saying it could not be completed.
// `permanent` follows the SMTP reply class, and nothing is rescheduled.
func testAsyncSoftBounce(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	rcpt := tests.FakeEmail(t)

	tn.Send(t, tn.Batch("Async Soft Bounce Test", "<h1>Hello!</h1>", To(rcpt, nil)))
	tn.Await(t, rcpt, Delivered)

	msg := e.MX.AwaitEnvelope(t, rcpt)
	require.NoError(t, e.DeliverDSN(t, msg.ReturnPath, rcpt, 450, "Mailbox temporarily unavailable"))

	bounced := tn.Await(t, rcpt, Bounced)
	assert.False(t, bounced.Data.Permanent,
		"a 4xx DSN is terminal but not permanent: the address is not proven dead")
	assert.Equal(t, 450, bounced.Data.SMTPCode)
	assert.Contains(t, bounced.Data.Detail, "Mailbox temporarily unavailable")

	assert.Len(t, tn.EventsOf(t, rcpt, Delivered), 1, "the bounce does not retract the Delivered event")
	tn.Holds(t, "no new attempt after the DSN", func(c *probe) {
		assert.Equal(c, 1, e.MX.Attempts(rcpt), "an asynchronous bounce must not trigger another send")
	})
}

func testTransientThenDelivered(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	const failures = 2
	rcpt := fmt.Sprintf("transient.x%d.%s@%s", failures, tests.FakeUsername(t), tn.Domain)

	b := tn.Send(t, tn.Batch("Transient Then Delivered Test", "<h1>Hello!</h1>", To(rcpt, nil)))

	tn.Await(t, rcpt, Delivered)
	assert.Equal(t, failures+1, e.MX.Attempts(rcpt), "every transient refusal is retried until accepted")
	assert.Empty(t, tn.EventsOf(t, rcpt, Bounced), "a transient refusal with retries left is not a bounce")

	done := tn.AwaitBatch(t, b, Completed)
	assert.Equal(t, 1, done.DeliveryCounts.Delivered)
}

// testRetryBudgetEndsBounced is the Retry Budget seen from outside: an MX that answers 4xx to every
// attempt is retried until the compressed Retry Window is spent, and the Delivery then ends Bounced
// with the last reply it got, not permanent, because nobody proved the address dead.
func testRetryBudgetEndsBounced(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	rcpt := fmt.Sprintf("deferred.%s@%s", tests.FakeUsername(t), tn.Domain)

	b := tn.Send(t, tn.Batch("Retry Budget Test", "<h1>Hello!</h1>", To(rcpt, nil)))

	bounced := tn.Await(t, rcpt, Bounced)
	assert.False(t, bounced.Data.Permanent, "running out the Retry Budget on 4xx is not a permanent bounce")
	assert.Equal(t, 451, bounced.Data.SMTPCode)
	assert.Greater(t, e.MX.Attempts(rcpt), 1, "a deferred Delivery is retried before the budget runs out")

	done := tn.AwaitBatch(t, b, Completed)
	assert.Equal(t, 1, done.DeliveryCounts.Bounced)
	attempts := e.MX.Attempts(rcpt)
	tn.Holds(t, "no attempt after the Retry Budget ran out", func(c *probe) {
		assert.Equal(c, attempts, e.MX.Attempts(rcpt))
	})
}

// testForgedDSNBouncesNothing proves the inbound server and the Dispatcher's signer together: a DSN
// on a return path Kannon did not sign names a real Delivery, and must not bounce it.
func testForgedDSNBouncesNothing(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	rcpt := tests.FakeEmail(t)

	tn.Send(t, tn.Batch("Forged DSN Test", "<h1>Hello!</h1>", To(rcpt, nil)))
	tn.Await(t, rcpt, Delivered)

	msg := e.MX.AwaitEnvelope(t, rcpt)
	_, batchID, ok := strings.Cut(msg.ReturnPath, "+")
	require.True(t, ok, "the return path names the Batch after '+': %s", msg.ReturnPath)
	unsigned := "bump_" + base64.URLEncoding.EncodeToString([]byte(rcpt)) + "+" + batchID
	forged := "bump_" + base64.URLEncoding.EncodeToString([]byte(rcpt)) + "." +
		base64.URLEncoding.EncodeToString([]byte("not-the-signature")) + "+" + batchID

	for _, path := range []string{unsigned, forged} {
		if err := e.DeliverDSN(t, path, rcpt, 550, "User unknown"); err != nil {
			t.Logf("Kannon refused the forged DSN on %s: %v", path, err)
		}
	}

	tn.Holds(t, "no bounce from a forged DSN", func(c *probe) {
		assert.Empty(c, tn.EventsOf(c, rcpt, Bounced))
	})

	// The genuine return path still works, and its bounce is the only one the Delivery carries.
	require.NoError(t, e.DeliverDSN(t, msg.ReturnPath, rcpt, 452, "Mailbox full"))
	assert.Equal(t, 452, tn.Await(t, rcpt, Bounced).Data.SMTPCode)
	tn.Holds(t, "only the genuine DSN bounced the Delivery", func(c *probe) {
		assert.Len(c, tn.EventsOf(c, rcpt, Bounced), 1)
	})
}
