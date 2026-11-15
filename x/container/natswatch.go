package container

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// natsDisconnectGrace is how long a lost NATS connection may take to come back before it is worth a
// warning. NATS restarts, rolling updates and network blips all reconnect well within it.
const natsDisconnectGrace = 30 * time.Second

// natsWatch logs the life of a NATS connection after it is established. The client reconnects on its
// own; what an operator needs to see is a disconnection that lasts, not every one that heals.
type natsWatch struct {
	log   *slog.Logger
	grace time.Duration

	mu     sync.Mutex
	downAt time.Time
	warn   *time.Timer
}

func newNatsWatch(log *slog.Logger) *natsWatch {
	return &natsWatch{log: log, grace: natsDisconnectGrace}
}

func (w *natsWatch) options() []nats.Option {
	return []nats.Option{
		nats.DisconnectErrHandler(w.disconnected),
		nats.ReconnectHandler(w.reconnected),
		nats.ErrorHandler(w.asyncError),
	}
}

func (w *natsWatch) disconnected(_ *nats.Conn, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.downAt.IsZero() {
		return
	}
	w.downAt = time.Now()
	w.log.Debug("disconnected, reconnecting", "err", err)
	w.warn = time.AfterFunc(w.grace, func() {
		w.log.Warn("still disconnected, reconnecting", "disconnected_for", w.grace, "err", err)
	})
}

func (w *natsWatch) reconnected(nc *nats.Conn) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.downAt.IsZero() {
		return
	}
	down := time.Since(w.downAt)
	stillQuiet := w.warn.Stop()
	w.downAt = time.Time{}

	level := slog.LevelInfo
	if stillQuiet {
		level = slog.LevelDebug
	}
	w.log.Log(context.Background(), level, "reconnected", "url", nc.ConnectedUrlRedacted(), "disconnected_for", down.Round(time.Millisecond))
}

func (w *natsWatch) asyncError(_ *nats.Conn, sub *nats.Subscription, err error) {
	if sub != nil {
		w.log.Warn("asynchronous error", "subject", sub.Subject, "err", err)
		return
	}
	w.log.Warn("asynchronous error", "err", err)
}

// stop releases the pending warning, for a connection being closed on purpose.
func (w *natsWatch) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.warn != nil {
		w.warn.Stop()
	}
}
