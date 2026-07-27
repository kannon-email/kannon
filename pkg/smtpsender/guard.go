package smtpsender

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// sendGuard admits at most one SMTP transaction per Envelope stored on the
// sending stream.
//
// A correct ack deadline (see sendAckPolicy) keeps the server from redelivering
// an Envelope that is merely slow to send, but it cannot rule redelivery out:
// a worker that dies mid-send, a relay that hangs past the deadline, or any of
// the MaxDeliver retries hands the same stored message to another worker, and
// the mail goes out twice. The guard is what makes that harmless — a claim
// taken before the transaction starts, so two workers holding the same message
// at the same time still produce one email (#425).
type sendGuard interface {
	// Claim reserves key for the caller. It reports false when the key was
	// already claimed, which means another delivery of the same stored message
	// is being — or has already been — sent.
	Claim(ctx context.Context, key string) (bool, error)
}

const (
	// sendGuardBucket holds one entry per Envelope handed to SMTP.
	sendGuardBucket = "kannon-sent-envelopes"

	// sendGuardTTL is how long a claim is remembered. It has to outlast the
	// window in which the server can still redeliver a message — the whole
	// sendAckPolicy curve, plus the time a dead worker takes to be replaced —
	// and no longer, since every entry is dead weight after that.
	sendGuardTTL = 1 * time.Hour
)

// mustGetSendGuard opens the guard's key/value bucket, exiting on failure the
// same way a missing stream does: a sender that cannot deduplicate its own
// redeliveries is a sender that re-sends live email.
func mustGetSendGuard(ctx context.Context, js jetstream.JetStream) sendGuard {
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      sendGuardBucket,
		Description: "Envelopes already handed to SMTP, so a redelivery does not send them again",
		TTL:         sendGuardTTL,
		Storage:     jetstream.FileStorage,
		Replicas:    1,
	})
	if err != nil {
		slog.Error("cannot create send guard bucket", "bucket", sendGuardBucket, "err", err)
		os.Exit(1)
	}

	slog.Info("send guard ready", "bucket", sendGuardBucket)
	return &kvSendGuard{kv: kv}
}

// kvSendGuard keeps the claims in a JetStream key/value bucket, so the guard
// holds across the worker pool of one process, across replicas, and across
// restarts — which is exactly when a redelivery happens.
type kvSendGuard struct {
	kv jetstream.KeyValue
}

func (g *kvSendGuard) Claim(ctx context.Context, key string) (bool, error) {
	// Create is the atomic primitive: it fails rather than overwriting, so of
	// two workers racing on the same Envelope exactly one is told to send.
	if _, err := g.kv.Create(ctx, key, nil); err != nil {
		if errors.Is(err, jetstream.ErrKeyExists) {
			return false, nil
		}
		return false, fmt.Errorf("cannot claim %s: %w", key, err)
	}
	return true, nil
}

// sendKey names the unit the guard protects: one stored message on the sending
// stream, which is one intended SMTP transaction.
//
// The stream sequence is what makes this precise. Every redelivery of a message
// carries the same sequence, so they collapse onto one key; a Delivery that is
// legitimately dispatched again after a transient failure is a *new* message
// with a new sequence, and is sent again as it should be. The Envelope's email
// ID is folded in as well, so a sequence reused by a recreated stream cannot
// suppress an unrelated Delivery.
func sendKey(msg jetstream.Msg, emailID string) (string, error) {
	meta, err := msg.Metadata()
	if err != nil {
		return "", fmt.Errorf("cannot read message metadata: %w", err)
	}

	sum := sha256.Sum256([]byte(emailID))
	return strconv.FormatUint(meta.Sequence.Stream, 10) + "." + base64.RawURLEncoding.EncodeToString(sum[:8]), nil
}
