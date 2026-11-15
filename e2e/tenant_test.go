package e2e_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/mail"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/require"

	"github.com/kannon-email/kannon/e2e/restclient"
	"github.com/kannon-email/kannon/internal/tests"
)

// outcome is a public event type a Recipient's Delivery can produce (CONTEXT.md, outcome states).
type outcome string

const (
	Validated outcome = "validated"
	Delivered outcome = "delivered"
	Bounced   outcome = "bounced"
	Failed    outcome = "failed"
	Opened    outcome = "opened"
	Clicked   outcome = "clicked"
)

// Completed is the Batch History status of a Batch none of whose Deliveries has an attempt left.
const Completed = "completed"

const senderAlias = "Test Sender"

// tenant is one isolated Domain with its own API Key, driven through REST. Every scenario gets its
// own, so nothing it sends or reads can be confused with another scenario's running in parallel.
type tenant struct {
	Domain string

	env   *env
	ctx   context.Context
	api   *restclient.ClientWithResponses
	since time.Time
}

// NewTenant provisions a Domain and an API Key for it through REST, as an operator holding the Admin
// Token would. The key may send, read Batch History and read stats; administrative acts on the
// Domain stay with the operator.
func (e *env) NewTenant(t *testing.T) *tenant {
	t.Helper()
	domain := tests.FakeDomain(t)

	created, err := e.admin.CreateDomainWithResponse(t.Context(), &restclient.CreateDomainParams{},
		restclient.DomainCreate{Domain: domain})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))

	key, err := e.admin.CreateAPIKeyWithResponse(t.Context(), domain, &restclient.CreateAPIKeyParams{},
		restclient.APIKeyCreate{
			Name:  "e2e",
			Roles: []restclient.Role{restclient.Sender, restclient.BatchReader, restclient.Analyst},
		})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, key.StatusCode(), string(key.Body))

	return &tenant{
		Domain: domain,
		env:    e,
		ctx:    t.Context(),
		api:    e.client(t, key.JSON201.Secret),
		since:  time.Now().Add(-time.Hour),
	}
}

// From is the Tenant's default sender.
func (tn *tenant) From() restclient.From {
	name := senderAlias
	return restclient.From{Email: openapi_types.Email("sender@" + tn.Domain), Name: &name}
}

// FromHeader is the From header Kannon renders for From().
func (tn *tenant) FromHeader() string {
	return (&mail.Address{Name: senderAlias, Address: "sender@" + tn.Domain}).String()
}

// StatsHost is the host the Tenant's tracking links point at.
func (tn *tenant) StatsHost() string {
	return "stats." + tn.Domain
}

// Batch builds an HTML Batch from the Tenant's default sender, to be adjusted before sending.
func (tn *tenant) Batch(subject, html string, rcpts ...restclient.RecipientInput) restclient.BatchCreate {
	var content restclient.ContentInput
	if err := content.FromContentInput0(restclient.ContentInput0{Type: "html", Html: html}); err != nil {
		panic(err)
	}
	return restclient.BatchCreate{From: tn.From(), Subject: subject, Content: content, Recipients: rcpts}
}

// To is a Recipient with personalization fields.
func To(email string, fields map[string]string) restclient.RecipientInput {
	r := restclient.RecipientInput{Email: &email}
	if fields != nil {
		f := restclient.Fields(fields)
		r.Fields = &f
	}
	return r
}

// TrySend submits the Batch and returns the response whatever it is, for scenarios asserting an
// intake refusal.
func (tn *tenant) TrySend(t *testing.T, b restclient.BatchCreate) *restclient.CreateBatchResponse {
	t.Helper()
	res, err := tn.api.CreateBatchWithResponse(t.Context(), tn.Domain,
		&restclient.CreateBatchParams{ParameterIdempotency: rand.Text()}, b)
	require.NoError(t, err)
	return res
}

// Send submits the Batch, requires it to be created, and returns its creation receipt.
func (tn *tenant) Send(t *testing.T, b restclient.BatchCreate) *restclient.BatchCreated {
	t.Helper()
	res := tn.TrySend(t, b)
	require.Equal(t, http.StatusCreated, res.StatusCode(), string(res.Body))
	return res.JSON201
}

// SetTracking replaces the Domain's Tracking Policy, as the operator. A Policy is resolved at intake
// and frozen on each Delivery, so it must be set before the send it should govern.
func (tn *tenant) SetTracking(t *testing.T, opens, links restclient.TrackingMode) {
	t.Helper()
	tn.setTracking(t, tn.env.admin, &restclient.UpdateDomainParams{}, opens, links)
}

func (tn *tenant) setTracking(t *testing.T, c *restclient.ClientWithResponses, params *restclient.UpdateDomainParams, opens, links restclient.TrackingMode) {
	t.Helper()
	res, err := c.UpdateDomainWithResponse(t.Context(), tn.Domain, params,
		restclient.DomainPatch{Tracking: restclient.TrackingPolicy{Opens: opens, Links: links}})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
}

// event is one public stats event, decoded from the StatsEvent union of the contract.
type event struct {
	Type         outcome `json:"type"`
	Email        string  `json:"email"`
	BatchID      string  `json:"batch_id"`
	TrackingMode string  `json:"tracking_mode"`
	Data         struct {
		SMTPCode  int    `json:"smtp_code"`
		Detail    string `json:"detail"`
		Permanent bool   `json:"permanent"`
		Reason    string `json:"reason"`
		IP        string `json:"ip"`
		UserAgent string `json:"user_agent"`
		URL       string `json:"url"`
	} `json:"data"`
}

// Events reads every stats event of the Tenant's Domain, all pages.
func (tn *tenant) Events(c require.TestingT) []event {
	var out []event
	params := &restclient.ListStatsEventsParams{
		ParameterFrom: tn.since,
		ParameterTo:   time.Now().Add(time.Hour),
	}
	for {
		res, err := tn.api.ListStatsEventsWithResponse(tn.ctx, tn.Domain, params)
		require.NoError(c, err)
		require.Equal(c, http.StatusOK, res.StatusCode(), string(res.Body))
		for _, item := range res.JSON200.Items {
			raw, err := item.MarshalJSON()
			require.NoError(c, err)
			var e event
			require.NoError(c, json.Unmarshal(raw, &e))
			out = append(out, e)
		}
		if res.JSON200.NextCursor == nil {
			return out
		}
		params.ParameterCursor = res.JSON200.NextCursor
	}
}

// EventsOf is the Tenant's events of one type for one Recipient.
func (tn *tenant) EventsOf(c require.TestingT, rcpt string, o outcome) []event {
	var out []event
	for _, e := range tn.Events(c) {
		if e.Email == rcpt && e.Type == o {
			out = append(out, e)
		}
	}
	return out
}

// Await waits until the Recipient has an event of the given outcome and returns it.
func (tn *tenant) Await(t *testing.T, rcpt string, o outcome) event {
	t.Helper()
	var found event
	eventually(t, string(o)+" for "+rcpt, func(c *probe) {
		got := tn.EventsOf(c, rcpt, o)
		require.NotEmpty(c, got, "no %s event for %s", o, rcpt)
		found = got[0]
	})
	return found
}

// BatchHistory reads the Batch as Batch History reports it now.
func (tn *tenant) BatchHistory(c require.TestingT, id string) *restclient.Batch {
	res, err := tn.api.GetBatchWithResponse(tn.ctx, tn.Domain, id, &restclient.GetBatchParams{})
	require.NoError(c, err)
	require.Equal(c, http.StatusOK, res.StatusCode(), string(res.Body))
	return res.JSON200
}

// AwaitBatch waits until the Batch reaches the given status in its Batch History and returns it.
func (tn *tenant) AwaitBatch(t *testing.T, b *restclient.BatchCreated, status string) *restclient.Batch {
	t.Helper()
	var found *restclient.Batch
	eventually(t, "Batch "+b.Id+" "+status, func(c *probe) {
		found = tn.BatchHistory(c, b.Id)
		require.EqualValues(c, status, found.Status)
	})
	return found
}

// Deliveries reads the Batch's Delivery history, all pages.
func (tn *tenant) Deliveries(c require.TestingT, id string) []restclient.DeliverySummary {
	var out []restclient.DeliverySummary
	params := &restclient.ListDeliveriesParams{}
	for {
		res, err := tn.api.ListDeliveriesWithResponse(tn.ctx, tn.Domain, id, params)
		require.NoError(c, err)
		require.Equal(c, http.StatusOK, res.StatusCode(), string(res.Body))
		out = append(out, res.JSON200.Items...)
		if res.JSON200.NextCursor == nil {
			return out
		}
		params.ParameterCursor = res.JSON200.NextCursor
	}
}

// Aggregated sums the Domain's aggregate counters over the day around now, by event type.
func (tn *tenant) Aggregated(c require.TestingT) map[outcome]int {
	from := time.Now().UTC().Truncate(time.Hour).Add(-12 * time.Hour)
	res, err := tn.api.GetAggregatedStatsWithResponse(tn.ctx, tn.Domain, &restclient.GetAggregatedStatsParams{
		ParameterFrom:    from,
		ParameterTo:      from.Add(24 * time.Hour),
		ParameterGroupBy: "1h",
	})
	require.NoError(c, err)
	require.Equal(c, http.StatusOK, res.StatusCode(), string(res.Body))
	sum := map[outcome]int{}
	for _, b := range res.JSON200.Buckets {
		sum[Validated] += b.Counts.Validated
		sum[Delivered] += b.Counts.Delivered
		sum[Bounced] += b.Counts.Bounced
		sum[Failed] += b.Counts.Failed
		sum[Opened] += b.Counts.Opened
		sum[Clicked] += b.Counts.Clicked
	}
	return sum
}

// Holds asserts that cond stays true over the hold window.
func (tn *tenant) Holds(t *testing.T, what string, cond func(c *probe)) {
	t.Helper()
	holds(t, what, cond)
}
