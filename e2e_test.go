package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	schema "github.com/kannon-email/kannon/db"
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
	"github.com/sirupsen/logrus"
)

// TestE2EEmailSending tests the entire email sending pipeline with real infrastructure
func TestE2EEmailSending(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	
	t.Parallel()
	
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
	
	// Wait a bit for services to start
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
	
	sendResp, err := mailerClient.SendHTML(testCtx, sendReq)
	require.NoError(t, err)
	require.NotNil(t, sendResp.Msg)
	
	t.Logf("✅ Email queued with message ID: %s", sendResp.Msg.MessageId)
	
	// Assert that email is eventually received by SMTP server
	t.Run("EmailDeliveryVerification", func(t *testing.T) {
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
	})
	
	// Test database state verification
	t.Run("DatabaseStateVerification", func(t *testing.T) {
		// Connect to test database to verify state
		db, err := pgxpool.New(testCtx, infra.dbURL)
		require.NoError(t, err)
		defer db.Close()
		
		// Check that email was added to sending pool
		assert.Eventually(t, func() bool {
			var count int
			err := db.QueryRow(testCtx, 
				"SELECT COUNT(*) FROM sending_pool_emails WHERE message_id = $1", 
				sendResp.Msg.MessageId).Scan(&count)
			if err != nil {
				t.Logf("Error querying sending pool: %v", err)
				return false
			}
			t.Logf("📊 Found %d emails in sending pool for message ID %s", count, sendResp.Msg.MessageId)
			return count > 0
		}, 30*time.Second, 1*time.Second, "Email should appear in sending pool")
		
		// Check that stats were generated
		assert.Eventually(t, func() bool {
			var count int
			err := db.QueryRow(testCtx, 
				"SELECT COUNT(*) FROM stats WHERE message_id = $1", 
				sendResp.Msg.MessageId).Scan(&count)
			if err != nil {
				t.Logf("Error querying stats: %v", err)
				return false
			}
			t.Logf("📈 Found %d stats entries for message ID %s", count, sendResp.Msg.MessageId)
			return count > 0
		}, 30*time.Second, 1*time.Second, "Stats should be generated for the email")
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

// TestInfrastructure holds the test infrastructure resources
type TestInfrastructure struct {
	pool     *dockertest.Pool
	pgRes    *dockertest.Resource
	natsRes  *dockertest.Resource
	dbURL    string
	natsURL  string
	apiPort  uint
	cleanup  func()
}

// setupTestInfrastructure sets up PostgreSQL and NATS using dockertest
func setupTestInfrastructure(ctx context.Context) (*TestInfrastructure, error) {
	pool, err := dockertest.NewPool("")
	if err != nil {
		return nil, fmt.Errorf("could not connect to docker: %w", err)
	}

	// Start PostgreSQL
	pgRes, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "13-alpine",
		Env: []string{
			"POSTGRES_USER=test",
			"POSTGRES_PASSWORD=test",
			"POSTGRES_DB=test",
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		return nil, fmt.Errorf("could not start postgres: %w", err)
	}

	// Start NATS
	natsRes, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "nats",
		Tag:        "2.9-alpine",
		Cmd:        []string{"-js"},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		pool.Purge(pgRes)
		return nil, fmt.Errorf("could not start nats: %w", err)
	}

	// Set expiration
	pgRes.Expire(300)
	natsRes.Expire(300)

	// Get connection URLs
	dbURL := fmt.Sprintf("postgresql://test:test@localhost:%s/test?sslmode=disable", pgRes.GetPort("5432/tcp"))
	natsURL := fmt.Sprintf("nats://localhost:%s", natsRes.GetPort("4222/tcp"))

	// Wait for PostgreSQL to be ready
	var db *pgxpool.Pool
	err = pool.Retry(func() error {
		var err error
		db, err = pgxpool.New(ctx, dbURL)
		if err != nil {
			return err
		}
		return db.Ping(ctx)
	})
	if err != nil {
		pool.Purge(pgRes)
		pool.Purge(natsRes)
		return nil, fmt.Errorf("could not connect to postgres: %w", err)
	}

	// Apply schema
	_, err = db.Exec(ctx, schema.Schema)
	if err != nil {
		db.Close()
		pool.Purge(pgRes)
		pool.Purge(natsRes)
		return nil, fmt.Errorf("could not apply schema: %w", err)
	}
	db.Close()

	// Wait for NATS to be ready
	err = pool.Retry(func() error {
		nc, err := nats.Connect(natsURL)
		if err != nil {
			return err
		}
		defer nc.Close()
		return nil
	})
	if err != nil {
		pool.Purge(pgRes)
		pool.Purge(natsRes)
		return nil, fmt.Errorf("could not connect to nats: %w", err)
	}

	// Find available port for API
	apiPort, err := findAvailablePort()
	if err != nil {
		pool.Purge(pgRes)
		pool.Purge(natsRes)
		return nil, fmt.Errorf("could not find available port: %w", err)
	}

	return &TestInfrastructure{
		pool:    pool,
		pgRes:   pgRes,
		natsRes: natsRes,
		dbURL:   dbURL,
		natsURL: natsURL,
		apiPort: apiPort,
		cleanup: func() {
			pool.Purge(pgRes)
			pool.Purge(natsRes)
		},
	}, nil
}

// TestSMTPServer implements a simple SMTP server for testing
type TestSMTPServer struct {
	listener       net.Listener
	mu            sync.Mutex
	receivedEmails []ReceivedEmail
}

type ReceivedEmail struct {
	From    string
	To      string
	Subject string
	Body    string
}

func (s *TestSMTPServer) Start(ctx context.Context) (int, error) {
	// Try to bind to port 25 first, but if that fails (due to permissions), use a random port
	var listener net.Listener
	var err error
	
	// Try port 25 first
	listener, err = net.Listen("tcp", ":25")
	if err != nil {
		// If port 25 fails, try a random port starting from 2525
		for port := 2525; port < 3000; port++ {
			listener, err = net.Listen("tcp", fmt.Sprintf(":%d", port))
			if err == nil {
				break
			}
		}
		if err != nil {
			return 0, err
		}
	}
	
	s.listener = listener
	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					logrus.Errorf("Error accepting connection: %v", err)
					continue
				}
			}

			go s.handleConnection(conn)
		}
	}()

	return port, nil
}

func (s *TestSMTPServer) Stop() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

func (s *TestSMTPServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Simple SMTP server implementation
	fmt.Fprintf(conn, "220 localhost ESMTP test-server\r\n")

	var from, to, subject, body string
	var inData bool

	for {
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		lines := strings.Split(string(buf[:n]), "\r\n")
		for _, line := range lines {
			if line == "" {
				continue
			}

			if inData {
				if line == "." {
					// End of data
					s.mu.Lock()
					s.receivedEmails = append(s.receivedEmails, ReceivedEmail{
						From:    from,
						To:      to,
						Subject: subject,
						Body:    body,
					})
					s.mu.Unlock()
					
					fmt.Fprintf(conn, "250 OK\r\n")
					inData = false
					from, to, subject, body = "", "", "", ""
				} else {
					if strings.HasPrefix(line, "Subject:") {
						subject = strings.TrimPrefix(line, "Subject:")
						subject = strings.TrimSpace(subject)
					}
					body += line + "\n"
				}
				continue
			}

			switch {
			case strings.HasPrefix(line, "EHLO") || strings.HasPrefix(line, "HELO"):
				fmt.Fprintf(conn, "250 Hello\r\n")
			case strings.HasPrefix(line, "MAIL FROM:"):
				from = extractEmail(line)
				fmt.Fprintf(conn, "250 OK\r\n")
			case strings.HasPrefix(line, "RCPT TO:"):
				to = extractEmail(line)
				fmt.Fprintf(conn, "250 OK\r\n")
			case line == "DATA":
				fmt.Fprintf(conn, "354 End data with <CR><LF>.<CR><LF>\r\n")
				inData = true
			case line == "QUIT":
				fmt.Fprintf(conn, "221 Bye\r\n")
				return
			default:
				fmt.Fprintf(conn, "250 OK\r\n")
			}
		}
	}
}

func extractEmail(line string) string {
	// Extract email from "MAIL FROM:<email>" or "RCPT TO:<email>"
	start := strings.Index(line, "<")
	end := strings.Index(line, ">")
	if start != -1 && end != -1 && end > start {
		return line[start+1 : end]
	}
	return ""
}

func findAvailablePort() (uint, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	return uint(port), nil
}