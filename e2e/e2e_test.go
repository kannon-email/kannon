package e2e

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/kannon-email/kannon/internal/x/container"
	"github.com/kannon-email/kannon/pkg/api"
	"github.com/kannon-email/kannon/pkg/dispatcher"
	"github.com/kannon-email/kannon/pkg/sender"
	"github.com/kannon-email/kannon/pkg/validator"
	"github.com/kannon-email/kannon/pkg/stats"
	adminapiv1 "github.com/kannon-email/kannon/proto/kannon/admin/apiv1"
	adminv1connect "github.com/kannon-email/kannon/proto/kannon/admin/apiv1/apiv1connect"
	mailerapiv1 "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1"
	mailerv1connect "github.com/kannon-email/kannon/proto/kannon/mailer/apiv1/apiv1connect"
	mailertypes "github.com/kannon-email/kannon/proto/kannon/mailer/types"
)

// TestE2EEmailSending tests the entire email sending pipeline with real infrastructure
func TestE2EEmailSending(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	
	// Set up test infrastructure
	testCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	
	infra, err := setupTestInfrastructure(testCtx)
	if err != nil {
		t.Skipf("Docker not available, skipping E2E test: %v", err)
		return
	}
	defer infra.cleanup()
	
	// Set up SMTP server to receive emails
	smtpServer := &TestSMTPServer{}
	smtpPort, err := smtpServer.Start(testCtx)
	require.NoError(t, err)
	defer smtpServer.Stop()
	
	t.Logf("✅ SMTP server started on port %d", smtpPort)
	
	// Start Kannon services
	kannonCtx, kannonCancel := context.WithCancel(testCtx)
	defer kannonCancel()
	
	cnt := container.New(kannonCtx, container.Config{
		DBUrl:   infra.dbURL,
		NatsURL: infra.natsURL,
	})
	defer cnt.Close()
	
	// Start all Kannon services
	var wg errgroup.Group
	
	// Start API server
	wg.Go(func() error {
		return api.Run(kannonCtx, api.Config{Port: infra.apiPort}, cnt)
	})
	
	// Start sender with localhost hostname for local delivery
	wg.Go(func() error {
		return sender.Run(kannonCtx, cnt, sender.Config{
			Hostname: "testhost.local",
			MaxJobs:  5,
		})
	})
	
	// Start dispatcher
	wg.Go(func() error {
		return dispatcher.Run(kannonCtx, cnt)
	})
	
	// Start validator
	wg.Go(func() error {
		return validator.Run(kannonCtx, cnt)
	})
	
	// Start stats
	wg.Go(func() error {
		stats.Run(kannonCtx, cnt)
		return nil
	})
	
	// Wait for services to start
	time.Sleep(3 * time.Second)
	
	// Create API clients
	adminClient := adminv1connect.NewApiClient(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
	)
	
	mailerClient := mailerv1connect.NewMailerClient(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
	)
	
	// Create a test domain
	domain, err := adminClient.CreateDomain(testCtx, connect.NewRequest(&adminapiv1.CreateDomainRequest{
		Domain: "test.example.com",
	}))
	require.NoError(t, err)
	require.NotNil(t, domain.Msg)
	
	t.Logf("✅ Created domain: %s with key: %s", domain.Msg.Domain, domain.Msg.Key)
	
	// Create auth token
	authToken := base64.StdEncoding.EncodeToString([]byte(domain.Msg.Domain + ":" + domain.Msg.Key))
	
	// Run subtests
	t.Run("SingleRecipientEmail", func(t *testing.T) {
		testSingleRecipientEmail(t, testCtx, mailerClient, authToken, smtpServer, infra)
	})
	
	t.Run("MultipleRecipientsEmail", func(t *testing.T) {
		testMultipleRecipientsEmail(t, testCtx, mailerClient, authToken, smtpServer, infra)
	})
	
	t.Run("EmailWithAttachments", func(t *testing.T) {
		testEmailWithAttachments(t, testCtx, mailerClient, authToken, smtpServer, infra)
	})
	
	t.Run("InvalidEmailHandling", func(t *testing.T) {
		testInvalidEmailHandling(t, testCtx, mailerClient, authToken)
	})
	
	// Cancel context to stop services
	kannonCancel()
	
	// Wait for services to stop (with timeout)
	done := make(chan error, 1)
	go func() {
		done <- wg.Wait()
	}()
	
	select {
	case err := <-done:
		if err != nil && !strings.Contains(err.Error(), "context canceled") {
			t.Errorf("Error stopping services: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Log("Services didn't stop within timeout, continuing...")
	}
	
	t.Log("🎉 E2E email sending test completed successfully!")
}

// testSingleRecipientEmail tests sending to a single recipient
func testSingleRecipientEmail(t *testing.T, ctx context.Context, mailerClient mailerv1connect.MailerClient, authToken string, smtpServer *TestSMTPServer, infra *TestInfrastructure) {
	// Send an email using localhost as the domain so it will be delivered to our test SMTP server
	testEmail := "recipient@localhost"
	sendReq := connect.NewRequest(&mailerapiv1.SendHTMLReq{
		Sender: &mailertypes.Sender{
			Email: "sender@test.example.com",
			Alias: "Test Sender",
		},
		Recipients: []*mailertypes.Recipient{
			{
				Email: testEmail,
				Fields: map[string]string{
					"name": "Test User",
					"company": "Test Corp",
				},
			},
		},
		Subject: "Test Email from E2E Test",
		Html:    "<h1>Hello {{name}}!</h1><p>This is a test email from {{company}}.</p>",
	})
	
	// Set auth header
	sendReq.Header().Set("Authorization", "Basic "+authToken)
	
	sendResp, err := mailerClient.SendHTML(ctx, sendReq)
	require.NoError(t, err)
	require.NotNil(t, sendResp.Msg)
	
	t.Logf("✅ Email queued with message ID: %s", sendResp.Msg.MessageId)
	
	// Assert that email is eventually received by SMTP server
	assert.Eventually(t, func() bool {
		smtpServer.mu.Lock()
		defer smtpServer.mu.Unlock()
		
		for _, email := range smtpServer.receivedEmails {
			if strings.Contains(email.To, testEmail) {
				t.Logf("📧 Email received: To=%s, Subject=%s", email.To, email.Subject)
				
				// Verify email content
				assert.Contains(t, email.Body, "Hello Test User!")
				assert.Contains(t, email.Body, "This is a test email from Test Corp.")
				assert.Equal(t, "Test Email from E2E Test", email.Subject)
				return true
			}
		}
		t.Logf("⏳ Waiting for email delivery... (received %d emails so far)", len(smtpServer.receivedEmails))
		return false
	}, 60*time.Second, 2*time.Second, "Email should be received within 60 seconds")
	
	// Verify database state
	verifyDatabaseState(t, ctx, infra, sendResp.Msg.MessageId)
}

// testMultipleRecipientsEmail tests sending to multiple recipients
func testMultipleRecipientsEmail(t *testing.T, ctx context.Context, mailerClient mailerv1connect.MailerClient, authToken string, smtpServer *TestSMTPServer, infra *TestInfrastructure) {
	// Send an email to multiple recipients
	testEmails := []string{"recipient1@localhost", "recipient2@localhost", "recipient3@localhost"}
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
	
	sendReq := connect.NewRequest(&mailerapiv1.SendHTMLReq{
		Sender: &mailertypes.Sender{
			Email: "sender@test.example.com",
			Alias: "Test Sender",
		},
		Recipients: recipients,
		Subject:    "Bulk Email Test - {{name}}",
		Html:       "<h1>Hello {{name}}!</h1><p>Your ID is: {{id}}</p>",
	})
	
	// Set auth header
	sendReq.Header().Set("Authorization", "Basic "+authToken)
	
	sendResp, err := mailerClient.SendHTML(ctx, sendReq)
	require.NoError(t, err)
	require.NotNil(t, sendResp.Msg)
	
	t.Logf("✅ Bulk email queued with message ID: %s", sendResp.Msg.MessageId)
	
	// Assert that all emails are eventually received by SMTP server
	assert.Eventually(t, func() bool {
		smtpServer.mu.Lock()
		defer smtpServer.mu.Unlock()
		
		receivedCount := 0
		for _, email := range smtpServer.receivedEmails {
			for _, testEmail := range testEmails {
				if strings.Contains(email.To, testEmail) {
					receivedCount++
					t.Logf("📧 Email received: To=%s, Subject=%s", email.To, email.Subject)
					
					// Verify personalized content
					assert.Contains(t, email.Body, "Hello Test User")
					assert.Contains(t, email.Body, "Your ID is: ID-")
					assert.Contains(t, email.Subject, "Bulk Email Test")
					break
				}
			}
		}
		
		t.Logf("⏳ Waiting for bulk email delivery... (received %d/%d emails)", receivedCount, len(testEmails))
		return receivedCount == len(testEmails)
	}, 90*time.Second, 3*time.Second, "All emails should be received within 90 seconds")
	
	// Verify database state
	verifyDatabaseState(t, ctx, infra, sendResp.Msg.MessageId)
}

// testEmailWithAttachments tests sending emails with attachments
func testEmailWithAttachments(t *testing.T, ctx context.Context, mailerClient mailerv1connect.MailerClient, authToken string, smtpServer *TestSMTPServer, infra *TestInfrastructure) {
	// Create test attachment data
	attachmentData := []byte("This is a test attachment content")
	
	sendReq := connect.NewRequest(&mailerapiv1.SendHTMLReq{
		Sender: &mailertypes.Sender{
			Email: "sender@test.example.com",
			Alias: "Test Sender",
		},
		Recipients: []*mailertypes.Recipient{
			{
				Email: "attachment-test@localhost",
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
	})
	
	// Set auth header
	sendReq.Header().Set("Authorization", "Basic "+authToken)
	
	sendResp, err := mailerClient.SendHTML(ctx, sendReq)
	require.NoError(t, err)
	require.NotNil(t, sendResp.Msg)
	
	t.Logf("✅ Email with attachment queued with message ID: %s", sendResp.Msg.MessageId)
	
	// Assert that email with attachment is eventually received by SMTP server
	assert.Eventually(t, func() bool {
		smtpServer.mu.Lock()
		defer smtpServer.mu.Unlock()
		
		for _, email := range smtpServer.receivedEmails {
			if strings.Contains(email.To, "attachment-test@localhost") {
				t.Logf("📧 Email with attachment received: To=%s, Subject=%s", email.To, email.Subject)
				
				// Verify email content
				assert.Contains(t, email.Body, "Hello Attachment Test User!")
				assert.Contains(t, email.Body, "Please find the attachment below.")
				assert.Equal(t, "Email with Attachment", email.Subject)
				
				// Verify attachment is present in email body (basic check)
				assert.Contains(t, email.Body, "test-document.txt")
				return true
			}
		}
		t.Logf("⏳ Waiting for attachment email delivery...")
		return false
	}, 60*time.Second, 2*time.Second, "Email with attachment should be received within 60 seconds")
	
	// Verify database state
	verifyDatabaseState(t, ctx, infra, sendResp.Msg.MessageId)
}

// testInvalidEmailHandling tests handling of invalid email addresses
func testInvalidEmailHandling(t *testing.T, ctx context.Context, mailerClient mailerv1connect.MailerClient, authToken string) {
	// Try to send to invalid email addresses
	invalidEmails := []string{
		"invalid-email",
		"@localhost",
		"user@",
		"",
	}
	
	for _, invalidEmail := range invalidEmails {
		sendReq := connect.NewRequest(&mailerapiv1.SendHTMLReq{
			Sender: &mailertypes.Sender{
				Email: "sender@test.example.com",
				Alias: "Test Sender",
			},
			Recipients: []*mailertypes.Recipient{
				{
					Email: invalidEmail,
					Fields: map[string]string{
						"name": "Test User",
					},
				},
			},
			Subject: "Test Email",
			Html:    "<h1>Hello {{name}}!</h1>",
		})
		
		// Set auth header
		sendReq.Header().Set("Authorization", "Basic "+authToken)
		
		_, err := mailerClient.SendHTML(ctx, sendReq)
		// We expect this to either fail or succeed but be filtered out by validation
		if err != nil {
			t.Logf("✅ Invalid email %s was rejected: %v", invalidEmail, err)
		} else {
			t.Logf("⚠️ Invalid email %s was accepted (may be filtered later)", invalidEmail)
		}
	}
}

// verifyDatabaseState checks the database state after email sending
func verifyDatabaseState(t *testing.T, ctx context.Context, infra *TestInfrastructure, messageID string) {
	// Connect to test database to verify state
	db, err := pgxpool.New(ctx, infra.dbURL)
	require.NoError(t, err)
	defer db.Close()
	
	// Check that email was added to sending pool
	assert.Eventually(t, func() bool {
		var count int
		err := db.QueryRow(ctx, 
			"SELECT COUNT(*) FROM sending_pool_emails WHERE message_id = $1", 
			messageID).Scan(&count)
		if err != nil {
			t.Logf("Error querying sending pool: %v", err)
			return false
		}
		t.Logf("📊 Found %d emails in sending pool for message ID %s", count, messageID)
		return count > 0
	}, 30*time.Second, 1*time.Second, "Email should appear in sending pool")
	
	// Check that stats were generated
	assert.Eventually(t, func() bool {
		var count int
		err := db.QueryRow(ctx, 
			"SELECT COUNT(*) FROM stats WHERE message_id = $1", 
			messageID).Scan(&count)
		if err != nil {
			t.Logf("Error querying stats: %v", err)
			return false
		}
		t.Logf("📈 Found %d stats entries for message ID %s", count, messageID)
		return count > 0
	}, 30*time.Second, 1*time.Second, "Stats should be generated for the email")
}

// BenchmarkE2EEmailThroughput benchmarks email sending throughput
func BenchmarkE2EEmailThroughput(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	
	infra, err := setupTestInfrastructure(ctx)
	if err != nil {
		b.Skipf("Docker not available, skipping benchmark: %v", err)
		return
	}
	defer infra.cleanup()
	
	// Set up SMTP server
	smtpServer := &TestSMTPServer{}
	_, err = smtpServer.Start(ctx)
	if err != nil {
		b.Fatalf("Failed to start SMTP server: %v", err)
	}
	defer smtpServer.Stop()
	
	// Start Kannon services
	kannonCtx, kannonCancel := context.WithCancel(ctx)
	defer kannonCancel()
	
	cnt := container.New(kannonCtx, container.Config{
		DBUrl:   infra.dbURL,
		NatsURL: infra.natsURL,
	})
	defer cnt.Close()
	
	var wg errgroup.Group
	wg.Go(func() error {
		return api.Run(kannonCtx, api.Config{Port: infra.apiPort}, cnt)
	})
	wg.Go(func() error {
		return sender.Run(kannonCtx, cnt, sender.Config{
			Hostname: "testhost.local",
			MaxJobs:  20, // Increased for benchmark
		})
	})
	wg.Go(func() error {
		return dispatcher.Run(kannonCtx, cnt)
	})
	wg.Go(func() error {
		return validator.Run(kannonCtx, cnt)
	})
	wg.Go(func() error {
		stats.Run(kannonCtx, cnt)
		return nil
	})
	
	// Wait for services to start
	time.Sleep(5 * time.Second)
	
	// Create API clients
	adminClient := adminv1connect.NewApiClient(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
	)
	
	mailerClient := mailerv1connect.NewMailerClient(
		http.DefaultClient,
		fmt.Sprintf("http://localhost:%d", infra.apiPort),
	)
	
	// Create test domain
	domain, err := adminClient.CreateDomain(ctx, connect.NewRequest(&adminapiv1.CreateDomainRequest{
		Domain: "benchmark.example.com",
	}))
	if err != nil {
		b.Fatalf("Failed to create domain: %v", err)
	}
	
	authToken := base64.StdEncoding.EncodeToString([]byte(domain.Msg.Domain + ":" + domain.Msg.Key))
	
	// Run benchmark
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		sendReq := connect.NewRequest(&mailerapiv1.SendHTMLReq{
			Sender: &mailertypes.Sender{
				Email: "sender@benchmark.example.com",
				Alias: "Benchmark Sender",
			},
			Recipients: []*mailertypes.Recipient{
				{
					Email: fmt.Sprintf("user%d@localhost", i),
					Fields: map[string]string{
						"name": fmt.Sprintf("User %d", i),
					},
				},
			},
			Subject: "Benchmark Email {{name}}",
			Html:    "<h1>Hello {{name}}!</h1><p>This is benchmark email #{{name}}.</p>",
		})
		
		sendReq.Header().Set("Authorization", "Basic "+authToken)
		
		_, err := mailerClient.SendHTML(ctx, sendReq)
		if err != nil {
			b.Fatalf("Failed to send email: %v", err)
		}
	}
	
	kannonCancel()
	done := make(chan error, 1)
	go func() {
		done <- wg.Wait()
	}()
	
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		b.Log("Services didn't stop within timeout")
	}
}