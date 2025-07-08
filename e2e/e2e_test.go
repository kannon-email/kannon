package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
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
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerapiv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
	statsv1connect "github.com/kannon-email/kannon/proto/kannon/stats/apiv1/apiv1connect"
)

// TestE2EEmailSending tests the entire email sending pipeline with real infrastructure
func TestE2EEmailSending(t *testing.T) {
	infra, err := setupTestInfrastructure(t.Context())
	if err != nil {
		t.Skipf("Docker not available, skipping E2E test: %v", err)
		return
	}
	defer infra.cleanup()

	// Start Kannon services
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	cnt := container.New(ctx, container.Config{
		DBUrl:   infra.dbURL,
		NatsURL: infra.natsURL,
	})
	defer cnt.Close()

	// Start all Kannon services
	var wg errgroup.Group

	// Start API server
	wg.Go(func() error {
		return api.Run(ctx, api.Config{Port: infra.apiPort}, cnt)
	})

	senderMock := &senderMock{}

	// Start sender with localhost hostname for local delivery
	wg.Go(func() error {
		cfg := sender.Config{
			Hostname: "testhost.local",
			MaxJobs:  5,
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
		stats.Run(ctx, cnt)
		return nil
	})

	// Create API clients
	adminClient := adminv1connect.NewApiClient(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
	)

	mailerClient := mailerv1connect.NewMailerClient(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
	)

	statsClient := statsv1connect.NewStatsApiV1Client(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
	)

	factory := &clientFactory{
		mailerClient: mailerClient,
		adminClient:  adminClient,
		statsClient:  statsClient,
	}

	// Run subtests
	t.Run("SingleRecipientEmail", func(t *testing.T) {
		testSingleRecipientEmail(t, factory, senderMock, infra)
	})

	t.Run("MultipleRecipientsEmail", func(t *testing.T) {
		testMultipleRecipientsEmail(t, factory, senderMock, infra)
	})

	t.Run("EmailWithAttachments", func(t *testing.T) {
		testEmailWithAttachments(t, factory, senderMock, infra)
	})

	t.Log("🎉 E2E email sending test completed successfully!")
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

	// Assert that email is eventually received by SMTP server
	assert.EventuallyWithT(t, func(t *assert.CollectT) {
		email := senderMock.GetEmail(testEmail)
		require.NotNil(t, email)

		assert.Contains(t, string(email.Body), "Hello Test User!")
		assert.Contains(t, string(email.Body), "This is a test email from Test Corp.")
	}, 60*time.Second, 2*time.Second, "Email should be received within 60 seconds")

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

	// Assert that all emails are eventually received by SMTP server
	assert.EventuallyWithT(t, func(t *assert.CollectT) {
		for _, email := range testEmails {
			msg := smtpServer.GetEmail(email)
			require.NotNil(t, msg)

			assert.Contains(t, string(msg.Body), "Hello Test User")
			assert.Contains(t, string(msg.Body), "Your ID is: ID-")
		}
	}, 90*time.Second, 3*time.Second, "All emails should be received within 90 seconds")

	assert.EventuallyWithT(t, func(tt *assert.CollectT) {
		stats := client.GetStats(t)
		require.EqualValues(tt, 6, stats.Total)
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

	// Assert that email with attachment is eventually received by SMTP server
	assert.EventuallyWithT(t, func(t *assert.CollectT) {
		msg := smtpServer.GetEmail(email)
		require.NotNil(t, msg)

		assert.Contains(t, string(msg.Body), "Hello Attachment Test User!")
		assert.Contains(t, string(msg.Body), "Please find the attachment below.")
		assert.Contains(t, string(msg.Body), "test-document.txt")
	}, 60*time.Second, 2*time.Second, "Email with attachment")
}

func makeFakeEmail() string {
	return strings.ToLower(faker.Email())
}
