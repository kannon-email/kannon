package e2e

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// TestSMTPServer implements a comprehensive SMTP server for testing
type TestSMTPServer struct {
	listener       net.Listener
	mu             sync.RWMutex
	receivedEmails []ReceivedEmail
	connections    int
	errors         []error
	ctx            context.Context
	cancel         context.CancelFunc
}

// ReceivedEmail represents an email received by the test SMTP server
type ReceivedEmail struct {
	From        string
	To          string
	Subject     string
	Body        string
	Headers     map[string]string
	Attachments []EmailAttachment
	ReceivedAt  time.Time
	Size        int
}

// EmailAttachment represents an attachment in a received email
type EmailAttachment struct {
	Filename    string
	ContentType string
	Size        int
	Data        []byte
}

// SMTPSession represents an SMTP session
type SMTPSession struct {
	conn       net.Conn
	reader     *textproto.Reader
	writer     *textproto.Writer
	from       string
	to         []string
	data       []byte
	server     *TestSMTPServer
	sessionID  string
	startTime  time.Time
}

// Start starts the SMTP server on an available port
func (s *TestSMTPServer) Start(ctx context.Context) (int, error) {
	s.ctx, s.cancel = context.WithCancel(ctx)
	
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
			return 0, fmt.Errorf("failed to bind to any port: %w", err)
		}
	}
	
	s.listener = listener
	port := listener.Addr().(*net.TCPAddr).Port

	// Start accepting connections
	go s.acceptConnections()

	logrus.Infof("Test SMTP server started on port %d", port)
	return port, nil
}

// Stop stops the SMTP server
func (s *TestSMTPServer) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// acceptConnections accepts incoming connections
func (s *TestSMTPServer) acceptConnections() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				select {
				case <-s.ctx.Done():
					return
				default:
					logrus.Errorf("Error accepting connection: %v", err)
					s.addError(err)
					continue
				}
			}

			s.mu.Lock()
			s.connections++
			connID := s.connections
			s.mu.Unlock()

			go s.handleConnection(conn, fmt.Sprintf("session-%d", connID))
		}
	}
}

// handleConnection handles a single SMTP connection
func (s *TestSMTPServer) handleConnection(conn net.Conn, sessionID string) {
	defer conn.Close()

	session := &SMTPSession{
		conn:      conn,
		reader:    textproto.NewReader(bufio.NewReader(conn)),
		writer:    textproto.NewWriter(bufio.NewWriter(conn)),
		server:    s,
		sessionID: sessionID,
		startTime: time.Now(),
	}

	logrus.Debugf("SMTP session %s started from %s", sessionID, conn.RemoteAddr())

	// Send greeting
	if err := session.writer.PrintfLine("220 %s ESMTP Test Server Ready", "localhost"); err != nil {
		logrus.Errorf("Error sending greeting: %v", err)
		return
	}

	// Process commands
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			// Set read deadline to avoid hanging connections
			conn.SetReadDeadline(time.Now().Add(30 * time.Second))
			
			line, err := session.reader.ReadLine()
			if err != nil {
				if err == io.EOF {
					logrus.Debugf("SMTP session %s closed by client", sessionID)
				} else {
					logrus.Errorf("Error reading line in session %s: %v", sessionID, err)
				}
				return
			}

			if err := session.processCommand(line); err != nil {
				logrus.Errorf("Error processing command in session %s: %v", sessionID, err)
				return
			}
		}
	}
}

// processCommand processes a single SMTP command
func (session *SMTPSession) processCommand(line string) error {
	parts := strings.SplitN(line, " ", 2)
	if len(parts) == 0 {
		return session.writer.PrintfLine("500 Command not recognized")
	}

	command := strings.ToUpper(parts[0])
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	logrus.Debugf("SMTP session %s: %s %s", session.sessionID, command, args)

	switch command {
	case "HELO", "EHLO":
		return session.handleHelo(args)
	case "MAIL":
		return session.handleMail(args)
	case "RCPT":
		return session.handleRcpt(args)
	case "DATA":
		return session.handleData()
	case "RSET":
		return session.handleRset()
	case "QUIT":
		return session.handleQuit()
	case "NOOP":
		return session.writer.PrintfLine("250 OK")
	default:
		return session.writer.PrintfLine("500 Command not recognized")
	}
}

// handleHelo handles HELO/EHLO commands
func (session *SMTPSession) handleHelo(args string) error {
	if args == "" {
		return session.writer.PrintfLine("501 HELO requires domain argument")
	}

	return session.writer.PrintfLine("250 Hello %s", args)
}

// handleMail handles MAIL FROM command
func (session *SMTPSession) handleMail(args string) error {
	if !strings.HasPrefix(strings.ToUpper(args), "FROM:") {
		return session.writer.PrintfLine("501 Syntax error in MAIL command")
	}

	email := extractEmail(args)
	if email == "" {
		return session.writer.PrintfLine("501 Invalid sender address")
	}

	session.from = email
	session.to = nil // Reset recipients
	
	logrus.Debugf("SMTP session %s: Mail from: %s", session.sessionID, email)
	return session.writer.PrintfLine("250 OK")
}

// handleRcpt handles RCPT TO command
func (session *SMTPSession) handleRcpt(args string) error {
	if session.from == "" {
		return session.writer.PrintfLine("503 Need MAIL command first")
	}

	if !strings.HasPrefix(strings.ToUpper(args), "TO:") {
		return session.writer.PrintfLine("501 Syntax error in RCPT command")
	}

	email := extractEmail(args)
	if email == "" {
		return session.writer.PrintfLine("501 Invalid recipient address")
	}

	session.to = append(session.to, email)
	
	logrus.Debugf("SMTP session %s: Rcpt to: %s", session.sessionID, email)
	return session.writer.PrintfLine("250 OK")
}

// handleData handles DATA command
func (session *SMTPSession) handleData() error {
	if session.from == "" {
		return session.writer.PrintfLine("503 Need MAIL command first")
	}
	if len(session.to) == 0 {
		return session.writer.PrintfLine("503 Need RCPT command first")
	}

	if err := session.writer.PrintfLine("354 End data with <CR><LF>.<CR><LF>"); err != nil {
		return err
	}

	// Read email data until "."
	var data []byte
	for {
		line, err := session.reader.ReadLine()
		if err != nil {
			return err
		}

		if line == "." {
			break
		}

		// Handle dot-stuffing (remove leading dot if present)
		if strings.HasPrefix(line, ".") {
			line = line[1:]
		}

		data = append(data, []byte(line+"\r\n")...)
	}

	// Parse and store the email
	email := session.parseEmail(data)
	session.server.addReceivedEmail(email)

	logrus.Debugf("SMTP session %s: Received email from %s to %v (size: %d bytes)", 
		session.sessionID, session.from, session.to, len(data))

	return session.writer.PrintfLine("250 OK Message accepted for delivery")
}

// handleRset handles RSET command
func (session *SMTPSession) handleRset() error {
	session.from = ""
	session.to = nil
	return session.writer.PrintfLine("250 OK")
}

// handleQuit handles QUIT command
func (session *SMTPSession) handleQuit() error {
	session.writer.PrintfLine("221 Bye")
	return fmt.Errorf("quit") // This will close the connection
}

// parseEmail parses the email data and creates a ReceivedEmail
func (session *SMTPSession) parseEmail(data []byte) ReceivedEmail {
	email := ReceivedEmail{
		From:       session.from,
		To:         strings.Join(session.to, ", "),
		Headers:    make(map[string]string),
		ReceivedAt: time.Now(),
		Size:       len(data),
	}

	// Simple email parsing - split headers and body
	emailStr := string(data)
	parts := strings.Split(emailStr, "\r\n\r\n")
	if len(parts) < 2 {
		parts = strings.Split(emailStr, "\n\n")
	}

	if len(parts) >= 2 {
		headers := parts[0]
		body := strings.Join(parts[1:], "\n\n")

		// Parse headers
		headerLines := strings.Split(headers, "\n")
		for _, line := range headerLines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Handle folded headers
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				continue // Skip folded lines for simplicity
			}

			colonIndex := strings.Index(line, ":")
			if colonIndex > 0 {
				key := strings.TrimSpace(line[:colonIndex])
				value := strings.TrimSpace(line[colonIndex+1:])
				
				email.Headers[key] = value
				
				// Extract subject
				if strings.ToLower(key) == "subject" {
					email.Subject = value
				}
			}
		}

		email.Body = body
	} else {
		email.Body = emailStr
	}

	// Simple attachment detection (look for Content-Type: multipart)
	if strings.Contains(strings.ToLower(emailStr), "content-type: multipart") {
		email.Attachments = session.parseAttachments(emailStr)
	}

	return email
}

// parseAttachments parses email attachments (basic implementation)
func (session *SMTPSession) parseAttachments(emailStr string) []EmailAttachment {
	var attachments []EmailAttachment
	
	// Very basic multipart parsing - look for attachment indicators
	if strings.Contains(strings.ToLower(emailStr), "content-disposition: attachment") {
		// This is a simplified implementation
		// In a real parser, we'd properly handle MIME boundaries
		lines := strings.Split(emailStr, "\n")
		for i, line := range lines {
			if strings.Contains(strings.ToLower(line), "content-disposition: attachment") {
				// Try to extract filename
				filename := "unknown"
				if strings.Contains(line, "filename=") {
					parts := strings.Split(line, "filename=")
					if len(parts) > 1 {
						filename = strings.Trim(parts[1], "\"")
					}
				}
				
				// Try to find content type
				contentType := "application/octet-stream"
				for j := i - 5; j < i+5 && j < len(lines) && j >= 0; j++ {
					if strings.HasPrefix(strings.ToLower(lines[j]), "content-type:") {
						contentType = strings.TrimSpace(strings.Split(lines[j], ":")[1])
						break
					}
				}
				
				attachments = append(attachments, EmailAttachment{
					Filename:    filename,
					ContentType: contentType,
					Size:        0, // We don't calculate size in this simple implementation
				})
			}
		}
	}
	
	return attachments
}

// addReceivedEmail adds a received email to the collection
func (s *TestSMTPServer) addReceivedEmail(email ReceivedEmail) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.receivedEmails = append(s.receivedEmails, email)
}

// GetReceivedEmails returns all received emails
func (s *TestSMTPServer) GetReceivedEmails() []ReceivedEmail {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	emails := make([]ReceivedEmail, len(s.receivedEmails))
	copy(emails, s.receivedEmails)
	return emails
}

// GetReceivedEmailCount returns the number of received emails
func (s *TestSMTPServer) GetReceivedEmailCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.receivedEmails)
}

// ClearReceivedEmails clears all received emails
func (s *TestSMTPServer) ClearReceivedEmails() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.receivedEmails = nil
}

// GetConnectionCount returns the number of connections handled
func (s *TestSMTPServer) GetConnectionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connections
}

// addError adds an error to the error collection
func (s *TestSMTPServer) addError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errors = append(s.errors, err)
}

// GetErrors returns all errors encountered
func (s *TestSMTPServer) GetErrors() []error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	errors := make([]error, len(s.errors))
	copy(errors, s.errors)
	return errors
}

// extractEmail extracts email address from MAIL FROM: or RCPT TO: command
func extractEmail(line string) string {
	// Handle both formats: "FROM:<email>" and "FROM: <email>"
	line = strings.TrimSpace(line)
	
	// Remove the FROM: or TO: prefix
	if strings.HasPrefix(strings.ToUpper(line), "FROM:") {
		line = strings.TrimSpace(line[5:])
	} else if strings.HasPrefix(strings.ToUpper(line), "TO:") {
		line = strings.TrimSpace(line[3:])
	}
	
	// Extract email from angle brackets
	start := strings.Index(line, "<")
	end := strings.Index(line, ">")
	if start != -1 && end != -1 && end > start {
		return line[start+1 : end]
	}
	
	// If no angle brackets, return as is (after trimming)
	return strings.TrimSpace(line)
}

// WaitForEmails waits for a specific number of emails to be received
func (s *TestSMTPServer) WaitForEmails(count int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	
	for time.Now().Before(deadline) {
		if s.GetReceivedEmailCount() >= count {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	
	return false
}

// FindEmailByRecipient finds an email by recipient address
func (s *TestSMTPServer) FindEmailByRecipient(recipient string) *ReceivedEmail {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	for _, email := range s.receivedEmails {
		if strings.Contains(email.To, recipient) {
			return &email
		}
	}
	return nil
}

// FindEmailsBySubject finds emails by subject
func (s *TestSMTPServer) FindEmailsBySubject(subject string) []ReceivedEmail {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	var matches []ReceivedEmail
	for _, email := range s.receivedEmails {
		if strings.Contains(email.Subject, subject) {
			matches = append(matches, email)
		}
	}
	return matches
}