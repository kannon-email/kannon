package e2e_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	netsmtp "net/smtp"
	"regexp"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kannon-email/kannon/internal/delivery"
	"github.com/kannon-email/kannon/internal/tests"
	"github.com/kannon-email/kannon/pkg/api"
	"github.com/kannon-email/kannon/pkg/dispatcher"
	kannonsmtp "github.com/kannon-email/kannon/pkg/smtp"
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

	t.Run("AsyncSoftBounce", func(t *testing.T) {
		t.Parallel()
		testAsyncSoftBounce(t, factory, senderMock, infra)
	})

	t.Run("TransientThenDeliver", func(t *testing.T) {
		t.Parallel()
		testTransientThenDeliver(t, factory, senderMock, infra)
	})

	t.Run("DispatchFailureRecovery", func(t *testing.T) {
		t.Parallel()
		testDispatchFailureRecovery(t, factory, senderMock, infra)
	})

	t.Run("RetryBudgetExhausted", func(t *testing.T) {
		t.Parallel()
		testRetryBudgetExhausted(t, factory, infra)
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

	t.Run("TrackingIdentified", func(t *testing.T) {
		t.Parallel()
		testTrackingIdentified(t, factory, senderMock, infra)
	})

	t.Run("TrackingFull", func(t *testing.T) {
		t.Parallel()
		testTrackingFull(t, factory, senderMock, infra)
	})

	t.Run("BatchAboveDomainTrackingCeiling", func(t *testing.T) {
		t.Parallel()
		testBatchAboveDomainTrackingCeiling(t, factory, senderMock, infra)
	})

	t.Run("TrackingAnonymous", func(t *testing.T) {
		t.Parallel()
		testTrackingAnonymous(t, factory, senderMock, infra)
	})

	t.Run("MixedRecipientTrackingPolicies", func(t *testing.T) {
		t.Parallel()
		testMixedRecipientTrackingPolicies(t, factory, senderMock, infra)
	})

	t.Run("EveryRecipientRejected", func(t *testing.T) {
		t.Parallel()
		testEveryRecipientRejected(t, factory, senderMock, infra)
	})
}

func runKannon(t *testing.T, infra *TestInfrastructure, senderMock *senderMock) {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	viper.Reset()
	viper.Set("api.port", infra.apiPort)
	// Without this the API runnable refuses to start, which is the point of it: the suite
	// configures the credential the way an operator does and then presents it on every Admin
	// and Stats call, so the whole authenticated path is what these tests exercise.
	viper.Set("api.admin_token", adminToken)
	viper.Set("tracker.port", infra.trackerPort)
	viper.Set("stats.retention", "8760h")
	viper.Set("smtp.address", fmt.Sprintf(":%d", infra.smtpPort))

	cnt := container.NewForTest(ctx,
		container.WithDBURL(infra.dbURL),
		container.WithNatsURL(infra.natsURL),
		// Collapse the production 2m/5m retry curve into milliseconds so the
		// transient-then-deliver path converges in CI wall time.
		container.WithBackoff(delivery.ExponentialBackoff{
			Base: 50 * time.Millisecond,
			Min:  50 * time.Millisecond,
		}),
		// The Retry Budget has to be scaled by the same factor as the backoff
		// base, or a collapsed curve races through the whole budget and
		// terminates every Delivery in the suite. Production is 24h against
		// 2m·2ⁿ; this curve is 50ms·2ⁿ, i.e. 2400× faster, and 24h/2400 = 36s.
		//
		// That derivation keeps the boundary exactly where production has it:
		// 50ms·2⁹ = 25.6s is inside the window and 50ms·2¹⁰ = 51.2s is outside
		// it, so ten retries are admitted and the eleventh refused — identical
		// to the maxRetry = 10 constant this replaced. No subtest that relies on
		// being retried, DispatchFailureRecovery included, changes behaviour.
		container.WithRetryWindow(36*time.Second),
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
	// The inbound SMTP server: the leg that receives asynchronous DSNs. Unlike
	// the outbound SMTPSender it needs no test double — a subtest plays the
	// remote MTA by connecting to it and delivering a real DSN.
	reg.Register(kannonsmtp.New(cnt))

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

// dsnTemplate is a delivery status notification in the multipart/report shape
// of RFC 3464, the format a remote MTA uses to report a delivery it accepted
// and could not complete.
const dsnTemplate = `From: MAILER-DAEMON@mx.example.com
To: %s
Subject: Undelivered Mail Returned to Sender
Content-Type: multipart/report; report-type=delivery-status; boundary="B"

--B
Content-Type: text/plain; charset=us-ascii

This is the mail system at host mx.example.com.

--B
Content-Type: message/delivery-status

Reporting-MTA: dns; mx.example.com
Final-Recipient: rfc822; %s
Action: failed
Diagnostic-Code: SMTP; %d %s

--B--
`

// deliverDSN plays the remote MTA: it connects to Kannon's inbound SMTP server
// and delivers a bounce report addressed to the Envelope's return path, which
// is the only thing tying the report back to a Batch and a Recipient.
func deliverDSN(t *testing.T, infra *TestInfrastructure, returnPath, recipient string, code int, reason string) {
	t.Helper()

	// The registry starts every module concurrently, so the listener may not
	// be up yet when a parallel subtest gets here. Wait for the port rather
	// than retrying the SMTP exchange, which would double-report the bounce.
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", infra.smtpAddr(), time.Second)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}, 30*time.Second, 200*time.Millisecond, "inbound SMTP server never accepted connections")

	c, err := netsmtp.Dial(infra.smtpAddr())
	require.NoError(t, err)
	defer c.Close()

	require.NoError(t, c.Mail("MAILER-DAEMON@mx.example.com"))
	require.NoError(t, c.Rcpt(returnPath))

	w, err := c.Data()
	require.NoError(t, err)
	_, err = fmt.Fprintf(w, dsnTemplate, returnPath, recipient, code, reason)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, c.Quit())
}

// watchBounceSubject subscribes to kannon.stats.bounced and returns a function
// that waits for the bounce belonging to the given Recipient.
//
// Asserting through the stats API is not enough to pin the subject. The Stats
// worker consumes the kannon.stats.* wildcard and types each row off the
// payload, never the subject, so it records an asynchronous bounce even when it
// is published somewhere no consumer subscribes — which is precisely how #376
// went unnoticed. Watching the subject on the wire is the only assertion that
// fails when the two bounce paths drift apart again.
func watchBounceSubject(t *testing.T, infra *TestInfrastructure, email string) func(*testing.T) *statstypes.Stats {
	t.Helper()

	nc, err := nats.Connect(infra.natsURL)
	require.NoError(t, err)
	t.Cleanup(nc.Close)

	got := make(chan *statstypes.Stats, 8)
	// The subscription needs no explicit teardown: the deferred nc.Close above
	// drops it with the connection.
	_, err = nc.Subscribe("kannon.stats.bounced", func(m *nats.Msg) {
		s := &statstypes.Stats{}
		if err := proto.Unmarshal(m.Data, s); err != nil {
			return
		}
		if s.Email == email {
			got <- s
		}
	})
	require.NoError(t, err)

	// Flush so the subscription is registered on the server before the caller
	// triggers the event it means to observe.
	require.NoError(t, nc.Flush())

	return func(t *testing.T) *statstypes.Stats {
		t.Helper()
		select {
		case s := <-got:
			return s
		case <-time.After(20 * time.Second):
			t.Fatalf("no bounce for %s arrived on kannon.stats.bounced", email)
			return nil
		}
	}
}

// testAsyncSoftBounce closes the loop #376 left open: a Delivery is accepted by
// the relay and reported Delivered, and only later does a DSN come back saying
// it could not be completed.
//
// It is the asynchronous leg end to end — inbound SMTP server, NATS, Stats
// worker, stats API — and it pins down two things the fix decided. The event
// must land on kannon.stats.bounced, the same subject as a synchronous bounce
// (it used to go to kannon.stats.soft-bounce, which nothing consumed). And
// `permanent` must follow the SMTP reply class, not be asserted unconditionally:
// a 4xx DSN means the remote MTA gave up after its own retries, which is
// terminal for us but no evidence the address is dead.
//
// Note what does NOT happen: the Delivery is long gone from the Pool by now,
// dropped when Delivered arrived, so the Dispatcher terms the stat and nothing
// is rescheduled. The bounce is a record, not a state transition.
func testAsyncSoftBounce(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	// A plain address: the relay accepts it, so this Delivery reaches
	// Delivered before the DSN arrives — which is what makes it asynchronous.
	to := fmt.Sprintf("softbounce.%s@%s", tests.FakeUsername(t), client.domain)

	client.SendEmail(t, &mailerapiv1.SendHTMLReq{
		Sender:        client.Sender(),
		Recipients:    []*mailertypes.Recipient{{Email: to}},
		Subject:       "Async Soft Bounce Test",
		Html:          "<h1>Hello!</h1>",
		ScheduledTime: timestamppb.Now(),
	})

	requireStat(t, client, to, "delivered", 1)

	// The return path the SMTPSender used as envelope-from is where a real
	// MTA would send its report.
	sent := senderMock.GetEmail(to)
	require.NotNil(t, sent, "senderMock should have captured the delivered Envelope")
	require.Contains(t, sent.From, "bump_", "envelope-from should be a bounce return path")

	// Start watching the subject before provoking the event.
	awaitOnSubject := watchBounceSubject(t, infra, to)

	deliverDSN(t, infra, sent.From, to, 450, "Mailbox temporarily unavailable")

	// First half: the event travelled on kannon.stats.bounced.
	onWire := awaitOnSubject(t).Data.GetBounced()
	require.NotNil(t, onWire, "async bounce should carry typed Bounced data")
	assert.False(t, onWire.Permanent,
		"a 4xx DSN is terminal but not permanent: the address is not proven dead")
	assert.EqualValues(t, 450, onWire.Code)
	assert.Contains(t, onWire.Msg, "Mailbox temporarily unavailable")

	// Second half: it was persisted and is readable back through the API.
	matched := requireStat(t, client, to, "bounced", 1)
	bounced := matched[0].Data.GetBounced()
	require.NotNil(t, bounced, "persisted bounce should carry typed Bounced data")
	assert.False(t, bounced.Permanent)
	assert.EqualValues(t, 450, bounced.Code)

	// The Delivered stat stays: both events are true of this Delivery, in
	// order. The bounce does not retract or overwrite it.
	requireStat(t, client, to, "delivered", 1)

	// Nothing was rescheduled off the back of the DSN.
	assert.Len(t, senderMock.History(to), 1,
		"an asynchronous bounce must not trigger another send attempt")
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

// testRetryBudgetExhausted closes the loop testDispatchFailureRecovery opens.
// That test heals the Batch's Template and the Delivery goes out; this one never
// heals it, which before #378 meant the Delivery was rescheduled with a doubling
// backoff until its next attempt was years away — the sender's last stat being
// `accepted`, permanently. Now the Retry Budget ends it: a Failed stat the sender
// can read, and the Pool row gone (ADR 0007).
//
// The Template is broken with the same reversible SQL surgery the neighbouring
// test uses, and the Delivery is then jumped past its budget with a second piece
// of surgery: send_attempts_cnt is set to 10, which under this container's 36s
// window is the very first retry the budget refuses (50ms·2¹⁰ = 51.2s). Reaching
// that boundary honestly costs 50ms·2⁹ ≈ 26s of wall clock, too slow and
// too flaky for a subtest running alongside seventeen others; the surgery moves
// the Delivery to the boundary and leaves the real Dispatcher to decide what
// happens there, which is the part under test. Where the boundary itself sits is
// pinned away from here and for free: TestCanRetry/EquivalentToTheRetryCapItReplaced
// in internal/delivery pins that the tenth retry is admitted and the eleventh
// refused, and pkg/dispatcher/retry_budget_test.go walks a Delivery across it one
// dispatch cycle at a time.
//
// ADR 0001 rejected "test-only DB mutation of sending_pool_emails.scheduled_time"
// on two grounds, and only one of them reaches this test. Its load-bearing
// objection was that such a mutation bypasses the production path *under test* —
// there, the reschedule loop itself — and nothing under test here is bypassed:
// the real Dispatcher still claims this Delivery, still tries to build its
// Envelope, and still makes the termination decision, which is the behaviour #378
// added. The reschedule loop this jump skips is covered where it is cheap to
// cover honestly, in pkg/dispatcher/retry_budget_test.go: rescheduled at 0 and 1
// attempts, terminated at 2, no surgery. The other objection does apply and is
// accepted — writing send_attempts_cnt couples this test to a column name that is
// internal to the Pool.
func testRetryBudgetExhausted(t *testing.T, clientFactory *clientFactory, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	db, err := pgxpool.New(t.Context(), infra.dbURL)
	require.NoError(t, err)
	t.Cleanup(db.Close)

	to := fmt.Sprintf("spent.%s@%s", tests.FakeUsername(t), client.domain)

	// Scheduled slightly in the future, as in testDispatchFailureRecovery: the
	// gap is the window for both operations below, before the Dispatcher can
	// claim the Delivery. The Validator ignores scheduled_time, so validation
	// still happens now.
	msgID := client.SendEmail(t, &mailerapiv1.SendHTMLReq{
		Sender:        client.Sender(),
		Recipients:    []*mailertypes.Recipient{{Email: to}},
		Subject:       "Retry Budget Exhausted Test",
		Html:          "<h1>Hello!</h1>",
		ScheduledTime: timestamppb.New(time.Now().Add(3 * time.Second)),
	})

	var templateID string
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT template_id FROM messages WHERE message_id = $1`, msgID).Scan(&templateID))
	_, err = db.Exec(t.Context(),
		`UPDATE templates SET template_id = $1 WHERE template_id = $2`, templateID+".broken", templateID)
	require.NoError(t, err)

	_, err = db.Exec(t.Context(),
		`UPDATE sending_pool_emails SET send_attempts_cnt = 10 WHERE message_id = $1 AND email = $2`,
		msgID, to)
	require.NoError(t, err)

	// Half one: the sender is told, and told something it can act on.
	matched := requireStat(t, client, to, "failed", 1)
	require.NotNil(t, matched[0].Data)
	failed := matched[0].Data.GetFailed()
	require.NotNil(t, failed, "failed stat should carry typed Failed data")
	assert.Contains(t, failed.Reason, "retry budget",
		"a Failed stat states what ran out; unlike Bounced it carries no reply code")
	assert.NotContains(t, failed.Reason, to, "the reason is customer-visible and must name no address")

	// Half two: the Delivery is terminal, so it leaves the Pool — it is not
	// waiting for a retry that will never be allowed.
	require.EventuallyWithT(t, func(tt *assert.CollectT) {
		var n int
		require.NoError(tt, db.QueryRow(t.Context(),
			`SELECT count(*) FROM sending_pool_emails WHERE message_id = $1`, msgID).Scan(&n))
		require.Zero(tt, n, "a Delivery whose Retry Budget is spent must be dropped from the pool")
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

// What a recipient's mail client carries into a tracking request. Under Full the
// Tracker retains exactly these two values; under any lower Mode, neither.
const (
	engagementIP        = "203.0.113.9"
	engagementUserAgent = "kannon-e2e-agent/1.0"
)

// requireTrackerHit issues one tracking request the way a recipient's client
// would — with an IP address and a user agent for the Tracker to retain or drop —
// and returns the Location header, so the click path can assert the redirect.
// Redirect-following is disabled: the tests assert the 307 + Location directly,
// not that example.com is reachable from CI.
func requireTrackerHit(t *testing.T, url string, wantStatus int) string {
	t.Helper()

	httpClient := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	var location string
	require.EventuallyWithT(t, func(tt *assert.CollectT) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		require.NoError(tt, err)
		req.Header.Set("User-Agent", engagementUserAgent)
		req.Header.Set("X-Real-Ip", engagementIP)

		resp, err := httpClient.Do(req)
		require.NoError(tt, err)
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body) //nolint:errcheck // draining body before close
		require.Equal(tt, wantStatus, resp.StatusCode)
		location = resp.Header.Get("Location")
	}, 10*time.Second, 200*time.Millisecond, "Tracker endpoint %s should answer %d", url, wantStatus)

	return location
}

// trackEngagement sends one tracked message, retrieves its pixel and follows its
// tracked link, and returns the resulting Opened and Clicked stats — so a test can
// assert on both axes of the Policy from a single send. The Domain's Tracking
// Policy must already be set: it is resolved at intake and frozen on the Delivery.
func trackEngagement(t *testing.T, client *clientTest, senderMock *senderMock, infra *TestInfrastructure, subject string) (opened, clicked *statstypes.Stats) {
	t.Helper()

	const landingURL = "https://example.com/landing"
	to := tests.FakeEmail(t)

	client.SendEmail(t, &mailerapiv1.SendHTMLReq{
		Sender:        client.Sender(),
		Recipients:    []*mailertypes.Recipient{{Email: to}},
		Subject:       subject,
		Html:          fmt.Sprintf(`<html><body><h1>Hello!</h1><a href=%q>click</a></body></html>`, landingURL),
		ScheduledTime: timestamppb.Now(),
	})

	msg := requireGetEmail(t, senderMock, to)

	requireTrackerHit(t,
		fmt.Sprintf("http://localhost:%d/o/%s", infra.trackerPort, extractOpenToken(t, msg.Body)),
		http.StatusOK)
	location := requireTrackerHit(t,
		fmt.Sprintf("http://localhost:%d/c/%s", infra.trackerPort, extractClickToken(t, msg.Body)),
		http.StatusTemporaryRedirect)
	assert.Equal(t, landingURL, location, "click redirect must round-trip the originally-authored URL")

	openedStats := requireStat(t, client, to, "opened", 1)
	clickedStats := requireStat(t, client, to, "clicked", 1)

	assert.EqualValues(t, to, openedStats[0].Email, "an attributed open names the Recipient")
	assert.EqualValues(t, to, clickedStats[0].Email, "an attributed click names the Recipient")

	return openedStats[0], clickedStats[0]
}

// testTrackingIdentified is the default a fresh Domain resolves to, stated
// explicitly here: engagement events are attributed to the Recipient, and neither
// the IP address nor the user agent of the request survives into the stat.
func testTrackingIdentified(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	client.SetTrackingPolicy(t, &trackingtypes.TrackingPolicy{
		Opens: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
		Links: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
	})

	opened, clicked := trackEngagement(t, client, senderMock, infra, "Tracking Identified Test")

	openedData := opened.Data.GetOpened()
	require.NotNil(t, openedData)
	assert.Empty(t, openedData.Ip, "an Identified open must retain no IP address")
	assert.Empty(t, openedData.UserAgent, "an Identified open must retain no user agent")

	clickedData := clicked.Data.GetClicked()
	require.NotNil(t, clickedData)
	assert.Empty(t, clickedData.Ip, "an Identified click must retain no IP address")
	assert.Empty(t, clickedData.UserAgent, "an Identified click must retain no user agent")
}

// testTrackingAnonymous is the aggregate-statistics carve-out end to end: a Domain
// on Anonymous keeps its open and click rates while retaining nothing that could
// isolate one Recipient from another.
//
// One Batch is sent to two Recipients, because both halves of the claim are about
// the pair. The tokens they receive must be the *same* token — two independently
// minted ones would carry different iat/exp and so tell the two apart even while
// naming neither — and after both are exercised the Domain's aggregate counters
// must have moved with no per-recipient engagement row anywhere.
func testTrackingAnonymous(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	client.SetTrackingPolicy(t, &trackingtypes.TrackingPolicy{
		Opens: trackingtypes.TrackingMode_TRACKING_MODE_ANONYMOUS,
		Links: trackingtypes.TrackingMode_TRACKING_MODE_ANONYMOUS,
	})

	const landingURL = "https://example.com/landing"
	first, second := tests.FakeEmail(t), tests.FakeEmail(t)

	client.SendEmail(t, &mailerapiv1.SendHTMLReq{
		Sender:        client.Sender(),
		Recipients:    []*mailertypes.Recipient{{Email: first}, {Email: second}},
		Subject:       "Tracking Anonymous Test",
		Html:          fmt.Sprintf(`<html><body><h1>Hello!</h1><a href=%q>click</a></body></html>`, landingURL),
		ScheduledTime: timestamppb.Now(),
	})

	firstMsg := requireGetEmail(t, senderMock, first)
	secondMsg := requireGetEmail(t, senderMock, second)

	openToken := extractOpenToken(t, firstMsg.Body)
	clickToken := extractClickToken(t, firstMsg.Body)
	assert.Equal(t, openToken, extractOpenToken(t, secondMsg.Body),
		"an anonymous pixel names nobody, so both Recipients must receive the same token")
	assert.Equal(t, clickToken, extractClickToken(t, secondMsg.Body),
		"an anonymous tracked link names nobody, so both Recipients must receive the same token")

	requireTrackerHit(t,
		fmt.Sprintf("http://localhost:%d/o/%s", infra.trackerPort, openToken),
		http.StatusOK)
	location := requireTrackerHit(t,
		fmt.Sprintf("http://localhost:%d/c/%s", infra.trackerPort, clickToken),
		http.StatusTemporaryRedirect)
	assert.Equal(t, landingURL, location, "the redirect is owed to the recipient under every Mode")

	// The aggregate half: the Domain still gets its rates.
	require.EventuallyWithT(t, func(tt *assert.CollectT) {
		counts := make(map[string]int64)
		for _, s := range client.GetAggregatedStats(t).Stats {
			counts[s.Type] += s.Count
		}
		require.Positive(tt, counts["opened"], "an anonymous open must still be counted in aggregate")
		require.Positive(tt, counts["clicked"], "an anonymous click must still be counted in aggregate")
	}, 30*time.Second, 500*time.Millisecond, "aggregated engagement counters must move under Anonymous")

	// The per-recipient half. Two things make the absence meaningful rather than
	// merely early: the aggregate counter above proves the engagement events were
	// consumed on the sibling subscription, and both Deliveries already have their
	// own delivered row, so the per-recipient consumer is alive and current. The
	// absence is then re-checked over a window, because a row arriving late would
	// be just as much of a leak as one arriving at once.
	//
	// Polled by hand rather than with require.Never, which runs its condition in a
	// goroutine it does not wait for: when GetStats outlives the subtest that turns
	// into a "Fail in goroutine after the test has completed" panic.
	requireStat(t, client, first, "delivered", 1)
	requireStat(t, client, second, "delivered", 1)

	for range 6 {
		for _, s := range client.GetStats(t).Stats {
			require.NotEqual(t, "opened", s.Type, "an anonymous open must leave no per-recipient row")
			require.NotEqual(t, "clicked", s.Type, "an anonymous click must leave no per-recipient row")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// testTrackingFull is the only Mode under which Kannon keeps anything about the
// request itself: an operator who has selected Full gets the IP address and user
// agent on both axes.
func testTrackingFull(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	client.SetTrackingPolicy(t, &trackingtypes.TrackingPolicy{
		Opens: trackingtypes.TrackingMode_TRACKING_MODE_FULL,
		Links: trackingtypes.TrackingMode_TRACKING_MODE_FULL,
	})

	opened, clicked := trackEngagement(t, client, senderMock, infra, "Tracking Full Test")

	openedData := opened.Data.GetOpened()
	require.NotNil(t, openedData)
	assert.Equal(t, engagementIP, openedData.Ip, "a Full open must retain the IP address")
	assert.Equal(t, engagementUserAgent, openedData.UserAgent, "a Full open must retain the user agent")

	clickedData := clicked.Data.GetClicked()
	require.NotNil(t, clickedData)
	assert.Equal(t, engagementIP, clickedData.Ip, "a Full click must retain the IP address")
	assert.Equal(t, engagementUserAgent, clickedData.UserAgent, "a Full click must retain the user agent")
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

	requireTrackerHit(t, fmt.Sprintf("http://localhost:%d/o/%s", infra.trackerPort, token), http.StatusOK)

	matched := requireStat(t, client, to, "opened", 1)
	assert.EqualValues(t, to, matched[0].Email)

	// A fresh Domain resolves to Identified (ADR 0003), so the open is attributed
	// to the Recipient and nothing about the request itself is retained.
	opened := matched[0].Data.GetOpened()
	require.NotNil(t, opened, "opened stat should carry typed Opened data")
	assert.Empty(t, opened.Ip, "an Identified open must retain no IP address")
	assert.Empty(t, opened.UserAgent, "an Identified open must retain no user agent")
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

	location := requireTrackerHit(t,
		fmt.Sprintf("http://localhost:%d/c/%s", infra.trackerPort, token),
		http.StatusTemporaryRedirect)

	assert.Equal(t, landingURL, location, "click redirect must round-trip the originally-authored URL")

	matched := requireStat(t, client, to, "clicked", 1)
	assert.EqualValues(t, to, matched[0].Email)
	clicked := matched[0].Data.GetClicked()
	require.NotNil(t, clicked, "clicked stat should carry typed Clicked data")
	assert.Equal(t, landingURL, clicked.Url, "clicked stat URL must match the originally-authored URL")

	// As for opens: a fresh Domain is Identified, so the click is attributed and
	// nothing about the request itself is retained.
	assert.Empty(t, clicked.Ip, "an Identified click must retain no IP address")
	assert.Empty(t, clicked.UserAgent, "an Identified click must retain no user agent")
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

// testMixedRecipientTrackingPolicies is the Recipient level of the cascade seen
// from outside (#419): one Batch, three Recipients stating three different
// things, three different observable outcomes.
//
// The Domain allows Identified. The first Recipient states nothing and is
// tracked at the Domain's level. The second states Off — consent may always
// narrow — and receives a message with no tracking in it at all. The third asks
// for Full, above the Domain's ceiling; consent cannot widen (ADR 0003), so that
// one Recipient is Rejected with a reason in the send response while the other
// two are delivered normally.
func testMixedRecipientTrackingPolicies(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	client.SetTrackingPolicy(t, &trackingtypes.TrackingPolicy{
		Opens: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
		Links: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
	})

	const landingURL = "https://example.com/landing"
	tracked := tests.FakeEmail(t)
	untracked := tests.FakeEmail(t)
	greedy := tests.FakeEmail(t)

	res := client.SendEmailResponse(t, &mailerapiv1.SendHTMLReq{
		Sender: client.Sender(),
		Recipients: []*mailertypes.Recipient{
			{Email: tracked},
			{Email: untracked, Tracking: &trackingtypes.TrackingPolicy{
				Opens: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
				Links: trackingtypes.TrackingMode_TRACKING_MODE_OFF,
			}},
			{Email: greedy, Tracking: &trackingtypes.TrackingPolicy{
				Opens: trackingtypes.TrackingMode_TRACKING_MODE_FULL,
				Links: trackingtypes.TrackingMode_TRACKING_MODE_FULL,
			}},
		},
		Subject:       "Mixed Recipient Tracking Test",
		Html:          fmt.Sprintf(`<html><body><h1>Hello!</h1><a href=%q>click</a></body></html>`, landingURL),
		ScheduledTime: timestamppb.Now(),
	})

	assert.EqualValues(t, 2, res.AcceptedCount, "the rest of the Batch must proceed")
	assert.EqualValues(t, 1, res.RejectedCount)
	require.Len(t, res.RejectedRecipients, 1)
	assert.Equal(t, greedy, res.RejectedRecipients[0].Email)
	assert.Equal(t, "tracking_above_ceiling", res.RejectedRecipients[0].Reason,
		"the caller must be told why the Recipient was Rejected")

	// The Recipient that stated nothing is tracked at the Domain's level: it has
	// a pixel and a rewritten link, and retrieving the pixel produces an open
	// attributed to it.
	trackedMsg := requireGetEmail(t, senderMock, tracked)
	assert.NotContains(t, trackedMsg.Body, fmt.Sprintf("href=%q", landingURL),
		"a tracked Recipient's links must be rewritten")
	requireTrackerHit(t,
		fmt.Sprintf("http://localhost:%d/o/%s", infra.trackerPort, extractOpenToken(t, trackedMsg.Body)),
		http.StatusOK)
	opened := requireStat(t, client, tracked, "opened", 1)
	assert.EqualValues(t, tracked, opened[0].Email)

	// The Recipient that refused gets the message it asked for, in the same send.
	untrackedMsg := requireGetEmail(t, senderMock, untracked)
	assert.NotContains(t, untrackedMsg.Body, "<img", "no tracking pixel for a Recipient that refused")
	assert.Contains(t, untrackedMsg.Body, fmt.Sprintf("href=%q", landingURL),
		"the authored link must be delivered unrewritten")
	assert.NotContains(t, untrackedMsg.Body, "stats."+client.domain,
		"an untracked message must carry no tracking hostname")

	// A Rejected Recipient has no Delivery, so nothing is ever sent to it.
	assert.Nil(t, senderMock.GetEmail(greedy), "a Rejected Recipient must not be delivered to")
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

// testEveryRecipientRejected is the other half of #364: before it, a caller
// submitting a batch in which nothing was accepted got a Batch id and a 200, with
// no way to discover that the Pool was empty. The response must now account for
// every submitted Recipient.
func testEveryRecipientRejected(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	client.SetTrackingPolicy(t, &trackingtypes.TrackingPolicy{
		Opens: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
		Links: trackingtypes.TrackingMode_TRACKING_MODE_IDENTIFIED,
	})

	greedy := tests.FakeEmail(t)

	res := client.SendEmailResponse(t, &mailerapiv1.SendHTMLReq{
		Sender: client.Sender(),
		Recipients: []*mailertypes.Recipient{
			{Email: ""},
			{Email: greedy, Tracking: &trackingtypes.TrackingPolicy{
				Opens: trackingtypes.TrackingMode_TRACKING_MODE_FULL,
				Links: trackingtypes.TrackingMode_TRACKING_MODE_FULL,
			}},
		},
		Subject:       "Every Recipient Rejected Test",
		Html:          `<html><body><h1>Hello!</h1></body></html>`,
		ScheduledTime: timestamppb.Now(),
	})

	assert.EqualValues(t, 0, res.AcceptedCount, "nothing was queued and the response must say so")
	assert.EqualValues(t, 2, res.RejectedCount)
	require.Len(t, res.RejectedRecipients, 2)

	reasons := map[string]string{}
	for _, r := range res.RejectedRecipients {
		reasons[r.Email] = r.Reason
	}
	assert.Equal(t, "invalid_email", reasons[""])
	assert.Equal(t, "tracking_above_ceiling", reasons[greedy])

	assert.Nil(t, senderMock.GetEmail(greedy), "a Rejected Recipient must not be delivered to")
}
