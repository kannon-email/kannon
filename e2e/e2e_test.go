package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/go-faker/faker/v4"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kannon-email/kannon/internal/x/container"
	"github.com/kannon-email/kannon/pkg/api"
	"github.com/kannon-email/kannon/pkg/dispatcher"
	"github.com/kannon-email/kannon/pkg/sender"
	"github.com/kannon-email/kannon/pkg/stats"
	"github.com/kannon-email/kannon/pkg/validator"
	adminapiv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerapiv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
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

	t.Log("🎉 E2E email sending test completed successfully!")
}

func runKannon(t *testing.T, infra *TestInfrastructure, senderMock *senderMock) {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	cnt := container.New(ctx, container.Config{
		DBUrl:   infra.dbURL,
		NatsURL: infra.natsURL,
	})
	t.Cleanup(func() {
		cnt.Close()
	})

	wg, ctx := errgroup.WithContext(ctx)

	// Start API server
	wg.Go(func() error {
		return api.Run(ctx, api.Config{Port: infra.apiPort}, cnt)
	})

	// Start sender with localhost hostname for local delivery
	wg.Go(func() error {
		cfg := sender.Config{
			MaxJobs: 5,
		}

		sender := sender.NewSender(cnt.Nats(), cnt.NatsJetStream(), senderMock, cfg)
		return sender.Run(ctx)
	})

	// Start dispatcher
	wg.Go(func() error {
		return dispatcher.Run(ctx, cnt)
	})

	// Start validator
	wg.Go(func() error {
		return validator.Run(ctx, cnt)
	})

	// Start stats
	wg.Go(func() error {
		return stats.Run(ctx, cnt, stats.Config{Retention: 8760 * time.Hour})
	})

	go func() {
		err := wg.Wait()
		if err != nil {
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

func requireGetEmail(t *testing.T, s *senderMock, email string) ParsedEmail {
	var msg ParsedEmail
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		email := s.GetEmail(email)
		require.NotNil(t, email)

		msg = parseEmail(t, email.Body)
	}, 60*time.Second, 2*time.Second, "Email should be received within 60 seconds")

	return msg
}
