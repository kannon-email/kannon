package e2e_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/pkg/api"
	"github.com/kannon-email/kannon/pkg/dispatcher"
	"github.com/kannon-email/kannon/pkg/smtpsender"
	"github.com/kannon-email/kannon/pkg/stats"
	"github.com/kannon-email/kannon/pkg/tracker"
	"github.com/kannon-email/kannon/pkg/validator"
	adminapiv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerapiv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	statstypes "github.com/kannon-email/kannon/proto/kannon/stats/types"
	trackingtypes "github.com/kannon-email/kannon/proto/kannon/tracking/types"
	"github.com/kannon-email/kannon/x/container"
	"github.com/spf13/viper"
)

// TestE2EEmailSending tests the entire email sending pipeline with real infrastructure
func TestE2EEmailSending(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	infra, err := setupTestInfrastructure(t.Context())
	t.Cleanup(infra.Cleanup)
	if err != nil {
		t.Fatalf("Failed to setup test infrastructure: %v", err)
	}

	senderMock := &senderMock{}

	runKannon(t, infra, senderMock)

	// Wait for API server to be ready before creating clients
	waitForAPIServer(t, infra)

	factory := makeFactory(infra)

	waitHZ(t, factory, infra)

	t.Run("SingleRecipientEmail", func(t *testing.T) {
		t.Parallel()
		testSingleRecipientEmail(t, factory, senderMock, infra)
	})

	t.Run("MultipleRecipientsEmail", func(t *testing.T) {
		t.Parallel()
		testMultipleRecipientsEmail(t, factory, senderMock, infra)
	})

	t.Run("MassiveSend", func(t *testing.T) {
		t.Parallel()
		testMassiveSend(t, factory, infra)
	})

	t.Run("EmailWithAttachments", func(t *testing.T) {
		t.Parallel()
		testEmailWithAttachments(t, factory, senderMock, infra)
	})

	t.Run("EmailWithHeaders", func(t *testing.T) {
		t.Parallel()
		testEmailWithHeaders(t, factory, senderMock, infra)
	})

	t.Run("AggregatedStats", func(t *testing.T) {
		t.Parallel()
		testAggregatedStats(t, factory, senderMock, infra)
	})

	t.Run("PermanentBounce", func(t *testing.T) {
		t.Parallel()
		testPermanentBounce(t, factory, senderMock, infra)
	})

	t.Run("TransientThenDeliver", func(t *testing.T) {
		t.Parallel()
		testTransientThenDeliver(t, factory, senderMock, infra)
	})

	t.Run("DispatchFailureRecovery", func(t *testing.T) {
		t.Parallel()
		testDispatchFailureRecovery(t, factory, senderMock, infra)
	})

	t.Run("Opened", func(t *testing.T) {
		t.Parallel()
		testOpened(t, factory, senderMock, infra)
	})

	t.Run("Clicked", func(t *testing.T) {
		t.Parallel()
		testClicked(t, factory, senderMock, infra)
	})

	t.Run("TrackingOff", func(t *testing.T) {
		t.Parallel()
		testTrackingOff(t, factory, senderMock, infra)
	})

	t.Run("BatchAboveDomainTrackingCeiling", func(t *testing.T) {
		t.Parallel()
		testBatchAboveDomainTrackingCeiling(t, factory, senderMock, infra)
	})
}

func runKannon(t *testing.T, infra *TestInfrastructure, senderMock *senderMock) {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	viper.Reset()
	viper.Set("api.port", infra.apiPort)
	viper.Set("tracker.port", infra.trackerPort)
	viper.Set("stats.retention", "8760h")

	cnt := container.NewForTest(ctx,
		container.WithDBURL(infra.dbURL),
		container.WithNatsURL(infra.natsURL),
		// Collapse the production 2m/5m retry curve into milliseconds so the
		// transient-then-deliver path converges in CI wall time.
		container.WithBackoff(delivery.ExponentialBackoff{
			Base: 50 * time.Millisecond,
			Min:  50 * time.Millisecond,
		}),
	)
	t.Cleanup(func() {
		if err := cnt.CloseWithTimeout(30 * time.Second); err != nil {
			slog.Error("error closing container", "err", err)
		}
	})

	reg := &container.Registry{}
	reg.Register(api.New(cnt))
	reg.Register(dispatcher.New(cnt))
	reg.Register(validator.New(cnt))
	reg.Register(stats.New(cnt))
	reg.Register(tracker.New(cnt))

	// Custom SMTPSender wired against the test sender mock; the package's
	// New(c) builds a real SMTP sender from the container, which the e2e
	// suite can't use because it asserts on the captured payloads.
	// MaxJobs sized for the parallel subtest burst: the suite ships ~120+
	// messages concurrently (mostly from MassiveSend), and a too-small
	// worker pool blows the per-subtest EventuallyWithT windows.
	sender := smtpsender.NewSMTPSender(cnt.NatsPublisher(), cnt.NatsJetStream(), senderMock, smtpsender.Config{MaxJobs: 50})
	reg.Register(container.Runnable{Name: "smtpsender", Run: sender.Run})

	go func() {
		if err := reg.Run(ctx); err != nil {
			slog.Error("error in running kannon", "err", err)
		}
	}()
}

func testSingleRecipientEmail(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	testEmail := tests.FakeEmail(t)
	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: client.Sender(),
		Recipients: []*mailertypes.Recipient{
			{
				Email: testEmail,
				Fields: map[string]string{
					"name":    "Test User",
					"company": "Test Corp",
				},
			},
		},
		Subject:       "Test Email from E2E Test",
		Html:          "<h1>Hello {{name}}!</h1><p>This is a test email from {{company}}.</p>",
		ScheduledTime: timestamppb.Now(),
	}

	client.SendEmail(t, sendReq)

	msg := requireGetEmail(t, senderMock, testEmail)

	t.Run("EmailContent", func(t *testing.T) {
		assert.Contains(t, msg.Body, "Hello Test User!")
		assert.Contains(t, msg.Body, "This is a test email from Test Corp.")
		assert.Equal(t, client.SenderFrom(), msg.From)
		assert.Equal(t, testEmail, msg.To)
		assert.Equal(t, "Test Email from E2E Test", msg.Subject)
	})

	assert.EventuallyWithT(t, func(tt *assert.CollectT) {
		stats := client.GetStats(t)
		require.EqualValues(tt, 2, stats.Total)
		require.EqualValues(tt, 2, len(stats.Stats))

		require.EqualValues(tt, testEmail, stats.Stats[0].Email)
		require.Equal(tt, testEmail, stats.Stats[1].Email)
	}, 30*time.Second, 1*time.Second, "Stats should be available within 60 seconds")
}

// testMultipleRecipientsEmail tests sending to multiple recipients
func testMultipleRecipientsEmail(t *testing.T, clientFactory *clientFactory, smtpServer *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	// Send an email to multiple recipients
	testEmails := []string{
		tests.FakeEmail(t),
		tests.FakeEmail(t),
		tests.FakeEmail(t),
	}

	recipients := make([]*mailertypes.Recipient, len(testEmails))

	for i, email := range testEmails {
		recipients[i] = &mailertypes.Recipient{
			Email: email,
			Fields: map[string]string{
				"name": fmt.Sprintf("Test User %d", i+1),
				"id":   fmt.Sprintf("ID-%d", i+1),
			},
		}
	}

	sendReq := &mailerapiv1.SendHTMLReq{
		Sender:        client.Sender(),
		Recipients:    recipients,
		Subject:       "Bulk Email Test - {{name}}",
		Html:          "<h1>Hello {{name}}!</h1><p>Your ID is: {{id}}</p>",
		ScheduledTime: timestamppb.Now(),
	}

	client.SendEmail(t, sendReq)

	for id, email := range testEmails {
		t.Run(fmt.Sprintf("Email %d", id), func(t *testing.T) {
			t.Parallel()
			msg := requireGetEmail(t, smtpServer, email)
			assert.Contains(t, msg.Body, fmt.Sprintf("Hello Test User %d", id+1))
			assert.Contains(t, msg.Body, fmt.Sprintf("Your ID is: ID-%d", id+1))
			assert.Equal(t, client.SenderFrom(), msg.From)
			assert.Equal(t, email, msg.To)
			assert.Equal(t, fmt.Sprintf("Bulk Email Test - Test User %d", id+1), msg.Subject)
		})
	}

	assert.EventuallyWithT(t, func(tt *assert.CollectT) {
		stats := client.GetStats(t)
		require.EqualValues(tt, 6, stats.Total)
	}, 30*time.Second, 1*time.Second, "Stats should be available within 60 seconds")
}

func testMassiveSend(t *testing.T, clientFactory *clientFactory, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	numRecipients := 100

	recipients := make([]*mailertypes.Recipient, numRecipients)

	for i := range recipients {
		recipients[i] = &mailertypes.Recipient{
			Email: tests.FakeEmail(t),
		}
	}

	sendReq := &mailerapiv1.SendHTMLReq{
		Sender:        client.Sender(),
		Recipients:    recipients,
		Subject:       "Bulk Email Test - {{name}}",
		Html:          "<h1>Hello {{name}}!</h1><p>Your ID is: {{id}}</p>",
		ScheduledTime: timestamppb.Now(),
	}

	client.SendEmail(t, sendReq)

	assert.EventuallyWithT(t, func(tt *assert.CollectT) {
		stats := client.GetStats(t)
		require.EqualValues(tt, 2*numRecipients, stats.Total)
	}, 30*time.Second, 1*time.Second, "Stats should be available within 60 seconds")
}

// testEmailWithAttachments tests sending emails with attachments
func testEmailWithAttachments(t *testing.T, clientFactory *clientFactory, smtpServer *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	// Create test attachment data
	attachmentData := []byte("This is a test attachment content")
	email := tests.FakeEmail(t)

	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: client.Sender(),
		Recipients: []*mailertypes.Recipient{
			{
				Email: email,
				Fields: map[string]string{
					"name": "Attachment Test User",
				},
			},
		},
		Subject: "Email with Attachment",
		Html:    "<h1>Hello {{name}}!</h1><p>Please find the attachment below.</p>",
		Attachments: []*mailerapiv1.Attachment{
			{
				Filename: "test-document.txt",
				Content:  attachmentData,
			},
		},
		ScheduledTime: timestamppb.Now(),
	}

	client.SendEmail(t, sendReq)

	msg := requireGetEmail(t, smtpServer, email)

	t.Run("EmailContent", func(t *testing.T) {
		assert.Contains(t, msg.Body, "Hello Attachment Test User!")
		assert.Contains(t, msg.Body, "Please find the attachment below.")
	})

	t.Run("Attachment", func(t *testing.T) {
		assert.Equal(t, 1, len(msg.Attachments))

		att := msg.Attachments[0]
		assert.Equal(t, "test-document.txt", att.Filename)
		assert.Equal(t, attachmentData, att.Content)
	})
}

func testEmailWithHeaders(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	testEmail := tests.FakeEmail(t)
	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: client.Sender(),
		Recipients: []*mailertypes.Recipient{
			{
				Email: testEmail,
				Fields: map[string]string{
					"name":    "Test User",
					"company": "Test Corp",
				},
			},
		},
		Subject:       "Test Email with Headers",
		Html:          "<h1>Hello {{name}}!</h1><p>This is a test email from {{company}}.</p>",
		ScheduledTime: timestamppb.Now(),
		Headers: &mailertypes.Headers{
			To: []string{"visible-to@example.com"},
			Cc: []string{"cc1@example.com", "cc2@example.com"},
		},
	}

	client.SendEmail(t, sendReq)

	// The email should still be delivered to the actual recipient
	msg := requireGetEmail(t, senderMock, testEmail)

	t.Run("EmailContent", func(t *testing.T) {
		assert.Contains(t, msg.Body, "Hello Test User!")
		assert.Contains(t, msg.Body, "This is a test email from Test Corp.")
		assert.Equal(t, client.SenderFrom(), msg.From)
		// The visible To header should be the control header value, not the actual recipient
		assert.Equal(t, "visible-to@example.com", msg.To)
		// The Cc header should contain the control header cc values
		assert.Equal(t, "cc1@example.com, cc2@example.com", msg.Cc)
		assert.Equal(t, "Test Email with Headers", msg.Subject)
	})

	assert.EventuallyWithT(t, func(tt *assert.CollectT) {
		stats := client.GetStats(t)
		require.EqualValues(tt, 2, stats.Total)
		require.EqualValues(tt, 2, len(stats.Stats))

		require.EqualValues(tt, testEmail, stats.Stats[0].Email)
		require.Equal(tt, testEmail, stats.Stats[1].Email)
	}, 30*time.Second, 1*time.Second, "Stats should be available within 60 seconds")
}

func waitHZ(t *testing.T, clientFactory *clientFactory, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)
	ctx := t.Context()

	require.EventuallyWithT(t, func(t *assert.CollectT) {
		hzResp, err := client.hzClient.HZ(ctx, connect.NewRequest(&adminapiv1.HZRequest{}))
		require.NoError(t, err)
		require.NotNil(t, hzResp.Msg)

		results := hzResp.Msg.Result

		slog.Info(fmt.Sprintf("HZ results: %+v", results))

		assert.Equal(t, "", results["db"])
		assert.Equal(t, "", results["nats"])
	}, 60*time.Second, 2*time.Second, "HZ should be ready within 60 seconds")
}

func waitForAPIServer(t *testing.T, infra *TestInfrastructure) {
	// Create a direct HZ client that doesn't require domain creation
	hzClient := adminv1connect.NewHZServiceClient(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
	)

	require.EventuallyWithT(t, func(tt *assert.CollectT) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		_, err := hzClient.HZ(ctx, connect.NewRequest(&adminapiv1.HZRequest{}))
		if err != nil {
			tt.Errorf("Failed to connect to API server: %v", err)
			return
		}
	}, 30*time.Second, 500*time.Millisecond, "API server should be ready within 30 seconds")
}

func testAggregatedStats(t *testing.T, clientFactory *clientFactory, _ *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	testEmail := tests.FakeEmail(t)
	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: client.Sender(),
		Recipients: []*mailertypes.Recipient{
			{
				Email: testEmail,
			},
		},
		Subject:       "Aggregated Stats Test",
		Html:          "<h1>Hello!</h1>",
		ScheduledTime: timestamppb.Now(),
	}

	client.SendEmail(t, sendReq)

	// Wait for raw stats to appear first
	assert.EventuallyWithT(t, func(tt *assert.CollectT) {
		stats := client.GetStats(t)
		require.EqualValues(tt, 2, stats.Total)
	}, 30*time.Second, 1*time.Second, "Raw stats should be available")

	// Then check aggregated stats
	assert.EventuallyWithT(t, func(tt *assert.CollectT) {
		aggStats := client.GetAggregatedStats(t)
		require.NotEmpty(tt, aggStats.Stats)

		typeMap := make(map[string]int64)
		for _, s := range aggStats.Stats {
			typeMap[s.Type] += s.Count
		}

		require.Greater(tt, typeMap["accepted"], int64(0))
		require.Greater(tt, typeMap["delivered"], int64(0))
	}, 30*time.Second, 1*time.Second, "Aggregated stats should be available")
}

// requireStat polls the Stats API until at least `count` events of
// `statType` exist for `email`, then returns the matching stats so the
// caller can introspect typed Data. Mirrors the EventuallyWithT shape
// the existing happy-path assertions use, scoped to a (Type, Email) pair.
func requireStat(t *testing.T, client *clientTest, email, statType string, count int) []*statstypes.Stats {
	t.Helper()
	var matched []*statstypes.Stats
	require.EventuallyWithT(t, func(tt *assert.CollectT) {
		matched = matched[:0]
		stats := client.GetStats(t)
		for _, s := range stats.Stats {
			if s.Type == statType && s.Email == email {
				matched = append(matched, s)
			}
		}
		require.GreaterOrEqual(tt, len(matched), count,
			"expected at least %d %q stats for %s, got %d", count, statType, email, len(matched))
	}, 15*time.Second, 500*time.Millisecond,
		"Stats of type %q for %s should be available", statType, email)
	return matched
}

func testPermanentBounce(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	// Unique random suffix so this subtest's per-Recipient counters cannot
	// collide with anything else running in parallel.
	to := fmt.Sprintf("bounce.%s@%s", tests.FakeUsername(t), client.domain)

	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: client.Sender(),
		Recipients: []*mailertypes.Recipient{
			{Email: to},
		},
		Subject:       "Permanent Bounce Test",
		Html:          "<h1>Hello!</h1>",
		ScheduledTime: timestamppb.Now(),
	}

	client.SendEmail(t, sendReq)

	matched := requireStat(t, client, to, "bounced", 1)
	require.NotNil(t, matched[0].Data)
	bounced := matched[0].Data.GetBounced()
	require.NotNil(t, bounced, "bounced stat should carry typed Bounced data")
	assert.True(t, bounced.Permanent, "bounce should be classified permanent")
	assert.EqualValues(t, 550, bounced.Code, "permanent bounce should carry SMTP code 550")

	// senderMock should have observed exactly one attempt — permanent
	// bounces are not retried.
	assert.Len(t, senderMock.History(to), 1, "permanent bounce should not be retried")
}

func testTransientThenDeliver(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	// Unique random suffix per subtest run keeps the senderMock's
	// per-Recipient attempt counter and History isolated from anything
	// else exercising the harness.
	const transientFailures = 2
	to := fmt.Sprintf("transient.x%d.%s@%s", transientFailures, tests.FakeUsername(t), client.domain)

	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: client.Sender(),
		Recipients: []*mailertypes.Recipient{
			{Email: to},
		},
		Subject:       "Transient Then Deliver Test",
		Html:          "<h1>Hello!</h1>",
		ScheduledTime: timestamppb.Now(),
	}

	client.SendEmail(t, sendReq)

	// First the transient errors, then the eventual delivered. Polling on
	// delivered alone is enough to assert the loop converged.
	requireStat(t, client, to, "delivered", 1)

	errs := requireStat(t, client, to, "error", transientFailures)
	assert.Len(t, errs, transientFailures, "expected exactly %d error stats", transientFailures)

	// Bounced/Errored boundary: a transient SenderError must not be
	// reclassified as a permanent bounce.
	allStats := client.GetStats(t)
	for _, s := range allStats.Stats {
		if s.Email == to {
			assert.NotEqual(t, "bounced", s.Type, "transient failure should not produce a bounced stat")
		}
	}

	assert.Len(t, senderMock.History(to), transientFailures+1,
		"senderMock should have observed %d transient attempts plus one success", transientFailures)
}

// testDispatchFailureRecovery reproduces the failure mode of #400 through
// the whole stack: a Delivery whose Envelope build fails after the claim
// must be handed back to the pool (NOT stranded in Pool status 'sending')
// and must be delivered once the failure clears.
//
// The build failure is injected with reversible SQL surgery: renaming the
// Batch's template makes GetSendingData's messages×templates join come up
// empty, so the real Dispatcher's Build genuinely errors; renaming it back
// heals the path. Before the fix this test stalls at the anti-stranding
// assertion: the Delivery sits in 'sending' with zero attempts forever.
func testDispatchFailureRecovery(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	db, err := pgxpool.New(t.Context(), infra.dbURL)
	require.NoError(t, err)
	t.Cleanup(db.Close)

	to := fmt.Sprintf("recovery.%s@%s", tests.FakeUsername(t), client.domain)

	// Schedule slightly in the future: the gap is the window for breaking
	// the template BEFORE the Dispatcher can claim the Delivery. The
	// Validator ignores scheduled_time, so validation still happens now.
	sendReq := &mailerapiv1.SendHTMLReq{
		Sender:        client.Sender(),
		Recipients:    []*mailertypes.Recipient{{Email: to}},
		Subject:       "Dispatch Failure Recovery Test",
		Html:          "<h1>Hello!</h1>",
		ScheduledTime: timestamppb.New(time.Now().Add(3 * time.Second)),
	}
	msgID := client.SendEmail(t, sendReq)

	var templateID string
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT template_id FROM messages WHERE message_id = $1`, msgID).Scan(&templateID))
	_, err = db.Exec(t.Context(),
		`UPDATE templates SET template_id = $1 WHERE template_id = $2`, templateID+".broken", templateID)
	require.NoError(t, err)

	// The #400 anti-stranding assertion: the claimed-then-failed Delivery
	// must come back to 'scheduled' with a bumped attempt counter instead
	// of dying silently in 'sending'.
	require.EventuallyWithT(t, func(tt *assert.CollectT) {
		var status string
		var attempts int
		require.NoError(tt, db.QueryRow(t.Context(),
			`SELECT status, send_attempts_cnt FROM sending_pool_emails WHERE message_id = $1 AND email = $2`,
			msgID, to).Scan(&status, &attempts))
		require.GreaterOrEqual(tt, attempts, 1, "the failed dispatch must bump the attempt counter")
		require.Equal(tt, "scheduled", status, "the failed Delivery must be handed back to the pool")
	}, 30*time.Second, 100*time.Millisecond,
		"Delivery must be rescheduled after a dispatch failure, not stranded in 'sending'")

	// Heal the template; the next dispatch cycles must pick the Delivery
	// up again and the email must actually go out.
	_, err = db.Exec(t.Context(),
		`UPDATE templates SET template_id = $1 WHERE template_id = $2`, templateID, templateID+".broken")
	require.NoError(t, err)

	msg := requireGetEmail(t, senderMock, to)
	assert.Equal(t, "Dispatch Failure Recovery Test", msg.Subject)

	requireStat(t, client, to, "delivered", 1)

	// Terminal outcome: the Delivery leaves the Pool entirely.
	require.EventuallyWithT(t, func(tt *assert.CollectT) {
		var n int
		require.NoError(tt, db.QueryRow(t.Context(),
			`SELECT count(*) FROM sending_pool_emails WHERE message_id = $1`, msgID).Scan(&n))
		require.Zero(tt, n, "delivered Deliveries must be cleaned from the pool")
	}, 30*time.Second, 500*time.Millisecond, "pool must drain for the batch")
}

// openTokenRe extracts the JWT-style token from a `/o/<token>` tracking
// pixel URL produced by the Envelope builder. JWT tokens are
// base64url-encoded (`[A-Za-z0-9_-]`) with `.` separating the three
// segments — no `=` padding, no other punctuation.
var openTokenRe = regexp.MustCompile(`/o/([A-Za-z0-9._-]+)`)

func extractOpenToken(t *testing.T, body string) string {
	t.Helper()
	m := openTokenRe.FindStringSubmatch(body)
	require.Len(t, m, 2, "open token not found in body: %q", body)
	return m[1]
}

// clickTokenRe extracts the JWT-style token from a `/c/<token>` tracked
// link URL produced by the Envelope builder. The Envelope builder
// rewrites every `<a href="...">` to `https://stats.<fqdn>/c/<token>`.
var clickTokenRe = regexp.MustCompile(`/c/([A-Za-z0-9._-]+)`)

func extractClickToken(t *testing.T, body string) string {
	t.Helper()
	m := clickTokenRe.FindStringSubmatch(body)
	require.Len(t, m, 2, "click token not found in body: %q", body)
	return m[1]
}

func testOpened(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	to := tests.FakeEmail(t)
	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: client.Sender(),
		Recipients: []*mailertypes.Recipient{
			{Email: to},
		},
		Subject:       "Opened Test",
		Html:          "<html><body><h1>Hello!</h1></body></html>",
		ScheduledTime: timestamppb.Now(),
	}

	client.SendEmail(t, sendReq)

	msg := requireGetEmail(t, senderMock, to)
	token := extractOpenToken(t, msg.Body)

	url := fmt.Sprintf("http://localhost:%d/o/%s", infra.trackerPort, token)
	require.EventuallyWithT(t, func(tt *assert.CollectT) {
		resp, err := http.Get(url)
		require.NoError(tt, err)
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body) //nolint:errcheck // draining body before close
		require.Equal(tt, http.StatusOK, resp.StatusCode)
	}, 10*time.Second, 200*time.Millisecond, "Tracker open endpoint should be reachable")

	matched := requireStat(t, client, to, "opened", 1)
	assert.EqualValues(t, to, matched[0].Email)
}

func testClicked(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	const landingURL = "https://example.com/landing"
	to := tests.FakeEmail(t)
	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: client.Sender(),
		Recipients: []*mailertypes.Recipient{
			{Email: to},
		},
		Subject:       "Clicked Test",
		Html:          fmt.Sprintf(`<html><body><h1>Hello!</h1><a href=%q>click</a></body></html>`, landingURL),
		ScheduledTime: timestamppb.Now(),
	}

	client.SendEmail(t, sendReq)

	msg := requireGetEmail(t, senderMock, to)
	token := extractClickToken(t, msg.Body)

	// Redirect-following disabled: the test asserts the 307 + Location
	// directly, not that example.com is reachable from CI.
	httpClient := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	url := fmt.Sprintf("http://localhost:%d/c/%s", infra.trackerPort, token)
	var location string
	require.EventuallyWithT(t, func(tt *assert.CollectT) {
		resp, err := httpClient.Get(url)
		require.NoError(tt, err)
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body) //nolint:errcheck // draining body before close
		require.Equal(tt, http.StatusTemporaryRedirect, resp.StatusCode)
		location = resp.Header.Get("Location")
	}, 10*time.Second, 200*time.Millisecond, "Tracker click endpoint should be reachable")

	assert.Equal(t, landingURL, location, "click redirect must round-trip the originally-authored URL")

	matched := requireStat(t, client, to, "clicked", 1)
	assert.EqualValues(t, to, matched[0].Email)
	clicked := matched[0].Data.GetClicked()
	require.NotNil(t, clicked, "clicked stat should carry typed Clicked data")
	assert.Equal(t, landingURL, clicked.Url, "clicked stat URL must match the originally-authored URL")
}

// testTrackingOff is the counterpart of Opened and Clicked: a Domain whose
// Tracking Policy is Off on both axes must send mail with no tracking in it at
// all. The Policy is set through the Admin API before the send, because it is
// resolved at intake and frozen on each Delivery (ADR 0003).
func testTrackingOff(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	client.SetTrackingPolicy(t, &trackingtypes.TrackingPolicy{
		Opens: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
		Links: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
	})

	const landingURL = "https://example.com/landing"
	to := tests.FakeEmail(t)
	sendReq := &mailerapiv1.SendHTMLReq{
		Sender:        client.Sender(),
		Recipients:    []*mailertypes.Recipient{{Email: to}},
		Subject:       "Tracking Off Test",
		Html:          fmt.Sprintf(`<html><body><h1>Hello!</h1><a href=%q>click</a></body></html>`, landingURL),
		ScheduledTime: timestamppb.Now(),
	}

	client.SendEmail(t, sendReq)

	msg := requireGetEmail(t, senderMock, to)

	assert.NotContains(t, msg.Body, "<img", "no tracking pixel must be injected")
	assert.Contains(t, msg.Body, fmt.Sprintf("href=%q", landingURL),
		"the authored link must be delivered unrewritten")
	assert.NotContains(t, msg.Body, "stats."+client.domain,
		"an untracked message must carry no tracking hostname")
}

// testBatchAboveDomainTrackingCeiling is the ceiling counterpart of
// testTrackingOff: a Batch stating a Tracking Mode above its Domain's ceiling
// must fail the send call outright rather than being silently clamped to the
// ceiling (ADR 0003 — "exceeding the ceiling is an error, not a silent
// clamp"). The Domain is set to Off on both axes; the Batch asks for Full on
// opens, which is strictly above it.
func testBatchAboveDomainTrackingCeiling(t *testing.T, clientFactory *clientFactory, _ *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	client.SetTrackingPolicy(t, &trackingtypes.TrackingPolicy{
		Opens: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
		Links: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
	})

	to := tests.FakeEmail(t)
	sendReq := &mailerapiv1.SendHTMLReq{
		Sender:        client.Sender(),
		Recipients:    []*mailertypes.Recipient{{Email: to}},
		Subject:       "Batch Above Domain Ceiling Test",
		Html:          "<h1>Hello!</h1>",
		ScheduledTime: timestamppb.Now(),
		Tracking: &trackingtypes.TrackingPolicy{
			Opens: trackingtypes.TrackingMode_TRACKING_MODE_FULL,
		},
	}

	err := client.SendEmailExpectingFailure(t, sendReq)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "opens", "the error must name the violating axis")
}

func requireGetEmail(t *testing.T, s *senderMock, email string) ParsedEmail {
	var msg ParsedEmail
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		email := s.GetEmail(email)
		require.NotNil(t, email)

		msg = parseEmail(t, email.Body)
	}, 60*time.Second, 2*time.Second, "Email should be received within 60 seconds")

	return msg
}
