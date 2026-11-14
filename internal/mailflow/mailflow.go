// Package mailflow counts the flow of mail where each decision is taken: Intake, the outcome of
// each Delivery, SMTP transactions, dispatches and engagement events. Every label is a bounded
// enum and none names a Domain (ADR 0018). A nil *Metrics counts nothing.
package mailflow

import (
	"strconv"
	"time"

	"github.com/kannon-email/kannon/x/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// Intake names the API a Batch came in through.
type Intake string

const (
	IntakeMailer Intake = "mailer"
	IntakeREST   Intake = "rest"
)

// SendResult is how one SMTP transaction ended. Errored is plumbing, not a Delivery outcome.
type SendResult string

const (
	SendDelivered SendResult = "delivered"
	SendBounced   SendResult = "bounced"
	SendErrored   SendResult = "errored"
)

// DispatchResult is how one dispatch of a claimed Delivery ended.
type DispatchResult string

const (
	DispatchOK           DispatchResult = "ok"
	DispatchBuildError   DispatchResult = "build_error"
	DispatchPublishError DispatchResult = "publish_error"
)

// BounceSource tells a bounce answered during the SMTP transaction from one reported later by DSN.
type BounceSource string

const (
	BounceSync  BounceSource = "sync"
	BounceAsync BounceSource = "async"
)

// Engagement is the kind of event the Tracker publishes.
type Engagement string

const (
	Opened  Engagement = "opened"
	Clicked Engagement = "clicked"
)

const resultLabel = "result"

// Metrics holds the mail flow collectors of one registry.
type Metrics struct {
	batches    *prometheus.CounterVec
	recipients *prometheus.CounterVec
	outcomes   *prometheus.CounterVec
	attempts   *prometheus.CounterVec
	sendTime   *prometheus.HistogramVec
	dispatches *prometheus.CounterVec
	engagement *prometheus.CounterVec
}

// New registers the mail flow collectors on r, or reuses the ones already there.
func New(r prometheus.Registerer) *Metrics {
	return &Metrics{
		batches: metrics.Register(r, prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kannon_batches_total",
			Help: "Batches persisted at Intake, by the API they came in through. An idempotent replay is not counted.",
		}, []string{"intake"})),
		recipients: metrics.Register(r, prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kannon_recipients_total",
			Help: "Recipients decided at Intake: accepted into a Delivery, or rejected with none created.",
		}, []string{"intake", resultLabel})),
		outcomes: metrics.Register(r, prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kannon_delivery_outcomes_total",
			Help: "Delivery outcomes, counted by the Component that decides them. source and permanent are set only for bounced.",
		}, []string{"outcome", "source", "permanent"})),
		attempts: metrics.Register(r, prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kannon_send_attempts_total",
			Help: "SMTP transactions run by the SMTPSender, by how they ended.",
		}, []string{resultLabel})),
		sendTime: metrics.Register(r, prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "kannon_smtp_send_duration_seconds",
			Help:    "Time taken by one SMTP transaction of the SMTPSender, by how it ended.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60, 120},
		}, []string{resultLabel})),
		dispatches: metrics.Register(r, prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kannon_dispatch_total",
			Help: "Dispatches of a claimed Delivery by the Dispatcher, by how they ended.",
		}, []string{resultLabel})),
		engagement: metrics.Register(r, prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kannon_engagement_events_total",
			Help: "Engagement events published by the Tracker, by kind.",
		}, []string{"kind"})),
	}
}

// BatchAccepted counts one persisted Batch and the Recipients decided for it.
func (m *Metrics) BatchAccepted(intake Intake, accepted, rejected int) {
	if m == nil {
		return
	}
	m.batches.WithLabelValues(string(intake)).Inc()
	m.RecipientsDecided(intake, accepted, rejected)
}

// RecipientsDecided counts Recipient decisions taken at Intake.
func (m *Metrics) RecipientsDecided(intake Intake, accepted, rejected int) {
	if m == nil {
		return
	}
	m.recipients.WithLabelValues(string(intake), "accepted").Add(float64(accepted))
	m.recipients.WithLabelValues(string(intake), "rejected").Add(float64(rejected))
}

// Validated counts a Delivery that passed the Validator.
func (m *Metrics) Validated() { m.outcome("validated", "", "") }

// Rejected counts a Delivery the Validator refused.
func (m *Metrics) Rejected() { m.outcome("rejected", "", "") }

// Delivered counts a Delivery accepted by the receiving server.
func (m *Metrics) Delivered() { m.outcome("delivered", "", "") }

// Bounced counts a bounced Delivery; permanent follows the SMTP reply class.
func (m *Metrics) Bounced(source BounceSource, permanent bool) {
	m.outcome("bounced", string(source), strconv.FormatBool(permanent))
}

// Failed counts a Delivery whose Retry Budget ran out with no answer.
func (m *Metrics) Failed() { m.outcome("failed", "", "") }

func (m *Metrics) outcome(outcome, source, permanent string) {
	if m == nil {
		return
	}
	m.outcomes.WithLabelValues(outcome, source, permanent).Inc()
}

// SendAttempt counts one SMTP transaction and the time it took.
func (m *Metrics) SendAttempt(result SendResult, took time.Duration) {
	if m == nil {
		return
	}
	m.attempts.WithLabelValues(string(result)).Inc()
	m.sendTime.WithLabelValues(string(result)).Observe(took.Seconds())
}

// Dispatched counts one dispatch of a claimed Delivery.
func (m *Metrics) Dispatched(result DispatchResult) {
	if m == nil {
		return
	}
	m.dispatches.WithLabelValues(string(result)).Inc()
}

// Engaged counts one engagement event published.
func (m *Metrics) Engaged(kind Engagement) {
	if m == nil {
		return
	}
	m.engagement.WithLabelValues(string(kind)).Inc()
}
