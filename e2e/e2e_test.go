package e2e_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/go-faker/faker/v4"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kannon-email/kannon/internal/delivery"
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
	"github.com/kannon-email/kannon/x/container"
	"github.com/spf13/viper"
)

// TestE2EEmailSending tests the entire email sending pipeline with real infrastructure
func TestE2EEmailSending(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	infra, err := setupTestInfrastructure(t.Context())
	defer infra.Cleanup()
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
		testSingleRecipientEmail(t, factory, senderMock, infra)
	})

	t.Run("MultipleRecipientsEmail", func(t *testing.T) {
		testMultipleRecipientsEmail(t, factory, senderMock, infra)
	})

	t.Run("MassiveSend", func(t *testing.T) {
		testMassiveSend(t, factory, infra)
	})

	t.Run("EmailWithAttachments", func(t *testing.T) {
		testEmailWithAttachments(t, factory, senderMock, infra)
	})

	t.Run("EmailWithHeaders", func(t *testing.T) {
		testEmailWithHeaders(t, factory, senderMock, infra)
	})

	t.Run("AggregatedStats", func(t *testing.T) {
		testAggregatedStats(t, factory, senderMock, infra)
	})

	t.Run("PermanentBounce", func(t *testing.T) {
		testPermanentBounce(t, factory, senderMock, infra)
	})

	t.Run("TransientThenDeliver", func(t *testing.T) {
		testTransientThenDeliver(t, factory, senderMock, infra)
	})

	t.Run("Opened", func(t *testing.T) {
		testOpened(t, factory, senderMock, infra)
	})

	t.Log("🎉 E2E email sending test completed successfully!")
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
			logrus.Errorf("error closing container: %v", err)
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
	sender := smtpsender.NewSMTPSender(cnt.NatsPublisher(), cnt.NatsJetStream(), senderMock, smtpsender.Config{MaxJobs: 5})
	reg.Register(container.Runnable{Name: "smtpsender", Run: sender.Run})

	go func() {
		if err := reg.Run(ctx); err != nil {
			logrus.Errorf("error in running kannon: %v", err)
		}
	}()
}

func testSingleRecipientEmail(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	testEmail := makeFakeEmail()
	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: &mailertypes.Sender{
			Email: "sender@test.example.com",
			Alias: "Test Sender",
		},
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
		assert.Equal(t, "Test Sender <sender@test.example.com>", msg.From)
		assert.Equal(t, testEmail, msg.To)
		assert.Equal(t, "Test Email from E2E Test", msg.Subject)
	})

	assert.EventuallyWithT(t, func(tt *assert.CollectT) {
		stats := client.GetStats(t)
		require.EqualValues(tt, 2, stats.Total)
		require.EqualValues(tt, 2, len(stats.Stats))

		require.EqualValues(tt, testEmail, stats.Stats[0].Email)
		require.Equal(tt, testEmail, stats.Stats[1].Email)
	}, 10*time.Second, 1*time.Second, "Stats should be available within 60 seconds")
}

// testMultipleRecipientsEmail tests sending to multiple recipients
func testMultipleRecipientsEmail(t *testing.T, clientFactory *clientFactory, smtpServer *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	// Send an email to multiple recipients
	testEmails := []string{
		makeFakeEmail(),
		makeFakeEmail(),
		makeFakeEmail(),
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
		Sender: &mailertypes.Sender{
			Email: "sender@test.example.com",
			Alias: "Test Sender",
		},
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
			assert.Equal(t, "Test Sender <sender@test.example.com>", msg.From)
			assert.Equal(t, email, msg.To)
			assert.Equal(t, fmt.Sprintf("Bulk Email Test - Test User %d", id+1), msg.Subject)
		})
	}

	assert.EventuallyWithT(t, func(tt *assert.CollectT) {
		stats := client.GetStats(t)
		require.EqualValues(tt, 6, stats.Total)
	}, 10*time.Second, 1*time.Second, "Stats should be available within 60 seconds")
}

func testMassiveSend(t *testing.T, clientFactory *clientFactory, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	numRecipients := 100

	recipients := make([]*mailertypes.Recipient, numRecipients)

	for i := 0; i < numRecipients; i++ {
		recipients[i] = &mailertypes.Recipient{
			Email: makeFakeEmail(),
		}
	}

	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: &mailertypes.Sender{
			Email: "sender@test.example.com",
			Alias: "Test Sender",
		},
		Recipients:    recipients,
		Subject:       "Bulk Email Test - {{name}}",
		Html:          "<h1>Hello {{name}}!</h1><p>Your ID is: {{id}}</p>",
		ScheduledTime: timestamppb.Now(),
	}

	client.SendEmail(t, sendReq)

	assert.EventuallyWithT(t, func(tt *assert.CollectT) {
		stats := client.GetStats(t)
		require.EqualValues(tt, 2*numRecipients, stats.Total)
	}, 10*time.Second, 1*time.Second, "Stats should be available within 60 seconds")
}

// testEmailWithAttachments tests sending emails with attachments
func testEmailWithAttachments(t *testing.T, clientFactory *clientFactory, smtpServer *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	// Create test attachment data
	attachmentData := []byte("This is a test attachment content")
	email := makeFakeEmail()

	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: &mailertypes.Sender{
			Email: "sender@test.example.com",
			Alias: "Test Sender",
		},
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

	testEmail := makeFakeEmail()
	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: &mailertypes.Sender{
			Email: "sender@test.example.com",
			Alias: "Test Sender",
		},
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
		assert.Equal(t, "Test Sender <sender@test.example.com>", msg.From)
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
	}, 10*time.Second, 1*time.Second, "Stats should be available within 60 seconds")
}

func waitHZ(t *testing.T, clientFactory *clientFactory, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)
	ctx := t.Context()

	require.EventuallyWithT(t, func(t *assert.CollectT) {
		hzResp, err := client.hzClient.HZ(ctx, connect.NewRequest(&adminapiv1.HZRequest{}))
		require.NoError(t, err)
		require.NotNil(t, hzResp.Msg)

		results := hzResp.Msg.Result

		logrus.Infof("HZ results: %+v", results)

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
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := hzClient.HZ(ctx, connect.NewRequest(&adminapiv1.HZRequest{}))
		if err != nil {
			tt.Errorf("Failed to connect to API server: %v", err)
			return
		}
	}, 30*time.Second, 500*time.Millisecond, "API server should be ready within 30 seconds")
}

func makeFakeEmail() string {
	return strings.ToLower(faker.Email())
}

func testAggregatedStats(t *testing.T, clientFactory *clientFactory, _ *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	testEmail := makeFakeEmail()
	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: &mailertypes.Sender{
			Email: "sender@test.example.com",
			Alias: "Test Sender",
		},
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
	}, 10*time.Second, 1*time.Second, "Raw stats should be available")

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
	}, 10*time.Second, 1*time.Second, "Aggregated stats should be available")
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
	to := fmt.Sprintf("bounce.%s@%s", strings.ToLower(faker.Username()), client.domain)

	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: &mailertypes.Sender{
			Email: "sender@test.example.com",
			Alias: "Test Sender",
		},
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
	to := fmt.Sprintf("transient.x%d.%s@%s", transientFailures, strings.ToLower(faker.Username()), client.domain)

	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: &mailertypes.Sender{
			Email: "sender@test.example.com",
			Alias: "Test Sender",
		},
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

func testOpened(t *testing.T, clientFactory *clientFactory, senderMock *senderMock, infra *TestInfrastructure) {
	client := clientFactory.NewClient(t, infra)

	to := makeFakeEmail()
	sendReq := &mailerapiv1.SendHTMLReq{
		Sender: &mailertypes.Sender{
			Email: "sender@test.example.com",
			Alias: "Test Sender",
		},
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
		_, _ = io.Copy(io.Discard, resp.Body)
		require.Equal(tt, http.StatusOK, resp.StatusCode)
	}, 10*time.Second, 200*time.Millisecond, "Tracker open endpoint should be reachable")

	matched := requireStat(t, client, to, "opened", 1)
	assert.EqualValues(t, to, matched[0].Email)
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
