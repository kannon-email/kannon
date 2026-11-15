package e2e_test

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kannon-email/kannon/e2e/restclient"
	"github.com/kannon-email/kannon/internal/tests"
)

var sendingScenarios = map[string]func(*testing.T, *env){
	"SingleRecipient":    testSingleRecipient,
	"MultipleRecipients": testMultipleRecipients,
	"LargeSend":          testLargeSend,
	"Attachments":        testAttachments,
	"DisplayHeaders":     testDisplayHeaders,
	"AggregatedStats":    testAggregatedStats,
}

func testSingleRecipient(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	rcpt := tests.FakeEmail(t)

	b := tn.Send(t, tn.Batch("Test Email from E2E Test",
		"<h1>Hello {{name}}!</h1><p>This is a test email from {{company}}.</p>",
		To(rcpt, map[string]string{"name": "Test User", "company": "Test Corp"})))
	assert.Equal(t, 1, b.AcceptedCount)

	msg := e.MX.AwaitEnvelope(t, rcpt)
	assert.Contains(t, msg.Body, "Hello Test User!")
	assert.Contains(t, msg.Body, "This is a test email from Test Corp.")
	assert.Equal(t, tn.FromHeader(), msg.From)
	assert.Equal(t, rcpt, msg.To)
	assert.Equal(t, "Test Email from E2E Test", msg.Subject)

	tn.Await(t, rcpt, Validated)
	delivered := tn.Await(t, rcpt, Delivered)
	assert.Equal(t, b.Id, delivered.BatchID)

	done := tn.AwaitBatch(t, b, Completed)
	assert.Equal(t, 1, done.DeliveryCounts.Delivered)
}

func testMultipleRecipients(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	rcpts := []string{tests.FakeEmail(t), tests.FakeEmail(t), tests.FakeEmail(t)}

	var inputs []restclient.RecipientInput
	for i, r := range rcpts {
		inputs = append(inputs, To(r, map[string]string{
			"name": fmt.Sprintf("Test User %d", i+1),
			"id":   fmt.Sprintf("ID-%d", i+1),
		}))
	}
	b := tn.Send(t, tn.Batch("Bulk Email Test - {{name}}", "<h1>Hello {{name}}!</h1><p>Your ID is: {{id}}</p>", inputs...))

	for i, r := range rcpts {
		msg := e.MX.AwaitEnvelope(t, r)
		assert.Contains(t, msg.Body, fmt.Sprintf("Hello Test User %d", i+1))
		assert.Contains(t, msg.Body, fmt.Sprintf("Your ID is: ID-%d", i+1))
		assert.Equal(t, tn.FromHeader(), msg.From)
		assert.Equal(t, r, msg.To)
		assert.Equal(t, fmt.Sprintf("Bulk Email Test - Test User %d", i+1), msg.Subject)
	}

	done := tn.AwaitBatch(t, b, Completed)
	assert.Equal(t, len(rcpts), done.DeliveryCounts.Delivered)
	for _, r := range rcpts {
		tn.Await(t, r, Delivered)
	}
}

func testLargeSend(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	const n = 100

	inputs := make([]restclient.RecipientInput, n)
	for i := range inputs {
		inputs[i] = To(tests.FakeEmail(t), map[string]string{"name": fmt.Sprintf("User %d", i)})
	}
	b := tn.Send(t, tn.Batch("Large Send - {{name}}", "<h1>Hello {{name}}!</h1>", inputs...))
	require.Equal(t, n, b.AcceptedCount)

	done := tn.AwaitBatch(t, b, Completed)
	assert.Equal(t, n, done.DeliveryCounts.Delivered)

	deliveries := tn.Deliveries(t, b.Id)
	assert.Len(t, deliveries, n, "Batch History lists every Delivery")
	for _, d := range deliveries {
		assert.EqualValues(t, "delivered", d.Status, d.Email)
	}
}

func testAttachments(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	rcpt := tests.FakeEmail(t)
	content := []byte("This is a test attachment content")

	batch := tn.Batch("Email with Attachment", "<h1>Hello {{name}}!</h1><p>Please find the attachment below.</p>",
		To(rcpt, map[string]string{"name": "Attachment Test User"}))
	batch.Attachments = &[]restclient.AttachmentInput{{
		Filename:      "test-document.txt",
		ContentBase64: base64.StdEncoding.EncodeToString(content),
	}}
	b := tn.Send(t, batch)

	msg := e.MX.AwaitEnvelope(t, rcpt)
	assert.Contains(t, msg.Body, "Hello Attachment Test User!")
	assert.Contains(t, msg.Body, "Please find the attachment below.")
	require.Len(t, msg.Attachments, 1)
	assert.Equal(t, "test-document.txt", msg.Attachments[0].Filename)
	assert.Equal(t, content, msg.Attachments[0].Content)

	history := tn.BatchHistory(t, b.Id)
	require.Len(t, history.Attachments, 1)
	assert.Equal(t, "test-document.txt", history.Attachments[0].Filename)
	assert.Equal(t, len(content), history.Attachments[0].SizeBytes)
}

func testDisplayHeaders(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	rcpt := tests.FakeEmail(t)

	batch := tn.Batch("Test Email with Headers", "<h1>Hello {{name}}!</h1>",
		To(rcpt, map[string]string{"name": "Test User"}))
	batch.Headers = &restclient.Headers{
		To: &[]string{"visible-to@example.com"},
		Cc: &[]string{"cc1@example.com", "cc2@example.com"},
	}
	tn.Send(t, batch)

	msg := e.MX.AwaitEnvelope(t, rcpt)
	assert.Contains(t, msg.Body, "Hello Test User!")
	assert.Equal(t, tn.FromHeader(), msg.From)
	assert.Equal(t, "visible-to@example.com", msg.To, "the To header is the display value, not the Recipient")
	assert.Equal(t, "cc1@example.com, cc2@example.com", msg.Cc)
	assert.Equal(t, "Test Email with Headers", msg.Subject)
	tn.Await(t, rcpt, Delivered)
}

func testAggregatedStats(t *testing.T, e *env) {
	tn := e.NewTenant(t)
	rcpt := tests.FakeEmail(t)

	tn.Send(t, tn.Batch("Aggregated Stats Test", "<h1>Hello!</h1>", To(rcpt, nil)))
	tn.Await(t, rcpt, Delivered)

	eventually(t, "aggregate counters for the Domain", func(c *probe) {
		counts := tn.Aggregated(c)
		assert.Equal(c, 1, counts[Validated])
		assert.Equal(c, 1, counts[Delivered])
	})
}
