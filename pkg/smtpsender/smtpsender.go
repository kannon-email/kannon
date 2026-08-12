package smtpsender

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kannon-email/kannon/internal/envelope"
	"github.com/kannon-email/kannon/internal/envelopepb"
	"github.com/kannon-email/kannon/internal/publisher"
	"github.com/kannon-email/kannon/internal/smtp"
	"github.com/kannon-email/kannon/internal/stats"
	"github.com/kannon-email/kannon/internal/utils"
	"github.com/kannon-email/kannon/x/config"
	"github.com/kannon-email/kannon/x/container"
	"github.com/nats-io/nats.go/jetstream"
)

type Config struct {
	MaxJobs uint `mapstructure:"max_jobs"`
}

func (c *Config) setDefaults() {
	if c.MaxJobs == 0 {
		c.MaxJobs = 10
	}
}

type smtpSender struct {
	sender    smtp.Sender
	publisher publisher.Publisher
	js        jetstream.JetStream
	guard     sendGuard
	cfg       Config
}

// sendAckPolicy is the ack deadline curve of the sending consumer, and it is
// deliberately much longer than utils.DefaultAckPolicy: this worker acks only
// once the SMTP transaction has returned, so the first deadline has to outlast
// the slowest transaction worth waiting for — internal/smtp gives a single
// delivery attempt two minutes, and falls back across MX hosts.
//
// A shorter deadline does not merely retry bookkeeping: the server hands the
// Envelope to a second worker while the first is still talking to the relay,
// and the recipient receives the email twice. That is #425, where the curve
// left behind by #396 gave every SMTP transaction one second to complete.
//
// The later entries keep an Envelope no worker can settle from cycling: the
// last one governs every attempt up to MaxDeliver.
var sendAckPolicy = utils.AckPolicy{
	2 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}

// New constructs the SMTPSender runnable, loading its slice of configuration
// from viper under the "sender" key.
func New(cnt *container.Container) container.Runnable {
	var cfg Config
	config.LoadSection("sender", &cfg)
	cfg.setDefaults()
	s := NewSMTPSender(cnt.NatsPublisher(), cnt.NatsJetStream(), cnt.Sender(), cfg)
	return container.Runnable{
		Name: "smtpsender",
		Run:  s.Run,
	}
}

func NewSMTPSender(publisher publisher.Publisher, js jetstream.JetStream, s smtp.Sender, cfg Config) *smtpSender {
	return &smtpSender{
		sender:    s,
		publisher: publisher,
		js:        js,
		cfg:       cfg,
	}
}

func (s *smtpSender) Run(ctx context.Context) error {
	slog.With("hostname", s.sender.SenderName(), "max_jobs", s.cfg.MaxJobs).
		Info("Starting SMTPSender Service")
	mustConfigureStatsJS(ctx, s.js)

	s.guard = mustGetSendGuard(ctx, s.js)
	consumer := s.mustSendingConsumer(ctx)

	return s.handleSend(ctx, consumer)
}

// mustSendingConsumer subscribes to the Envelopes waiting to be transmitted.
func (s *smtpSender) mustSendingConsumer(ctx context.Context) jetstream.Consumer {
	return utils.MustGetPullSubscriber(ctx, s.js, "kannon-sending", "kannon.sending", "kannon-sending-pool",
		utils.WithAckPolicy(sendAckPolicy))
}

func (s *smtpSender) handleSend(ctx context.Context, consumer jetstream.Consumer) error {
	slog.Info("🚀 Ready to send!\n")

	maxJobs := s.cfg.MaxJobs

	tasks := NewParallel(maxJobs)

	con, err := consumer.Consume(func(msg jetstream.Msg) {
		tasks.RunTask(func() {
			err := s.handleMessage(ctx, msg)
			s.handleMsgAck(msg, err)
		})
	})
	if err != nil {
		return fmt.Errorf("error in consuming messages: %w", err)
	}
	defer con.Drain()

	<-ctx.Done()
	tasks.WaitAndClose()

	slog.Info("👋 Shutting down SMTPSender Service")
	return ctx.Err()
}

func (s *smtpSender) handleMsgAck(msg jetstream.Msg, err error) {
	if err != nil {
		slog.Error("error in handling message", "err", err)
		if err := msg.Nak(); err != nil {
			slog.Error("cannot nak message", "err", err)
		}
		return
	}
	if err := msg.Ack(); err != nil {
		slog.Error("cannot ack message", "err", err)
	}
}

func (s *smtpSender) handleMessage(ctx context.Context, msg jetstream.Msg) error {
	// The wire form goes no further than this line (ADR 0012). What the worker
	// guards, transmits and reports on is the Envelope itself, so how the
	// Dispatcher encoded it is the sending topic's business and not this
	// worker's.
	env, err := envelopepb.UnmarshalEnvelope(msg.Data())
	if err != nil {
		return err
	}

	if !s.claimSend(ctx, msg, env) {
		return nil
	}

	sendErr := s.sender.Send(env.ReturnPath(), env.To(), env.Body())
	if sendErr != nil {
		slog.Info(fmt.Sprintf("Cannot send email %v - %v: %v", utils.ObfuscateEmail(env.To()), env.EmailID(), sendErr.Error()))
		return s.handleSendError(sendErr, env)
	}
	slog.Info(fmt.Sprintf("Email delivered: %v - %v", utils.ObfuscateEmail(env.To()), env.EmailID()))
	return s.handleSendSuccess(env)
}

// claimSend reports whether this delivery of the Envelope is the one allowed
// to talk to SMTP. A delivery that loses the claim is a redelivery of an
// Envelope already handed to a relay: it is acknowledged and dropped, because
// re-sending it would put the same email in the recipient's mailbox twice.
//
// The guard fails open, including when it was never wired (Run always wires
// it): it exists to make a rare redelivery harmless, and a bucket that cannot
// be reached is not a reason to stop sending mail — a duplicate is a far
// smaller failure than a batch that never leaves.
func (s *smtpSender) claimSend(ctx context.Context, msg jetstream.Msg, env *envelope.Envelope) bool {
	if s.guard == nil {
		return true
	}

	key, err := sendKey(msg, env.EmailID())
	if err != nil {
		slog.Error("cannot derive send guard key, sending anyway", "email_id", env.EmailID(), "err", err)
		return true
	}

	claimed, err := s.guard.Claim(ctx, key)
	if err != nil {
		slog.Error("send guard unavailable, sending anyway", "email_id", env.EmailID(), "err", err)
		return true
	}

	if !claimed {
		slog.Warn("skipping redelivered envelope: already handed to SMTP",
			"email_id", env.EmailID(), "to", utils.ObfuscateEmail(env.To()))
	}
	return claimed
}

func (s *smtpSender) handleSendSuccess(env *envelope.Envelope) error {
	msgID, domain, err := utils.ExtractMsgIDAndDomainFromEmailID(env.EmailID())
	if err != nil {
		return nil
	}

	// PublishStat derives kannon.stats.delivered from the Outcome, which is the
	// same subject this used to name by hand. Hand-naming it is what put the
	// asynchronous bounce on a topic no consumer subscribed to (#376), and there
	// is no reason for the one happy path to keep its own copy of the rule.
	return publisher.PublishStat(s.publisher, stats.Event{
		MessageID: msgID,
		Domain:    domain,
		Email:     env.To(),
		Timestamp: time.Now(),
		Outcome:   stats.Delivered(),
	})
}

func (s *smtpSender) handleSendError(sendErr smtp.SenderError, env *envelope.Envelope) error {
	msgID, domain, err := utils.ExtractMsgIDAndDomainFromEmailID(env.EmailID())
	if err != nil {
		return nil
	}

	event := stats.Event{
		MessageID: msgID,
		Domain:    domain,
		Email:     env.To(),
		Timestamp: time.Now(),
	}
	if !env.ShouldRetry() || sendErr.IsPermanent() {
		// The permanent argument below still reads sendErr.IsPermanent(), not
		// the reason this branch was taken: a 4xx that lands here because
		// ShouldRetry is already false is terminal for us, but it is not
		// evidence the address is dead, and Bounced.Permanent tracks the SMTP
		// reply class, never the retry decision. #378 read this as the wrong
		// flag at retry exhaustion; #433 re-specified `permanent` to follow the
		// reply class on both the synchronous and asynchronous path
		// (CONTEXT.md, Bounced), which is exactly what this line already
		// did. "Fixing" it would regress the async 4xx assertions in
		// e2e/e2e_test.go.
		event.Outcome = stats.Bounced(sendErr.IsPermanent(), sendErr.Code(), sendErr.Error())
	} else {
		event.Outcome = stats.Errored(sendErr.Code(), sendErr.Error())
	}

	return publisher.PublishStat(s.publisher, event)
}

func mustConfigureStatsJS(ctx context.Context, js jetstream.JetStream) {
	name := "kannon-stats"

	confs := jetstream.StreamConfig{
		Name:        name,
		Description: "Email Stats for Kannon",
		Replicas:    1,
		Subjects:    []string{"kannon.stats.*"},
		Retention:   jetstream.LimitsPolicy,
		Duplicates:  10 * time.Minute,
		MaxAge:      24 * time.Hour,
		Storage:     jetstream.FileStorage,
		Discard:     jetstream.DiscardOld,
	}
	_, err := js.CreateOrUpdateStream(ctx, confs)
	if err != nil {
		slog.Error("cannot create js stream", "err", err)
		os.Exit(1)
	}

	slog.Info(fmt.Sprintf("created js stream: %v", name))
}
