package audit

import (
	"log/slog"

	"github.com/kannon-email/kannon/internal/authz"
)

// publisher is the minimal seam over a NATS connection this package needs: fire and forget, on the
// same model as the stat publisher. internal/publisher.Publisher satisfies it structurally, so the
// container's publisher passes straight through without this package importing a transport.
type publisher interface {
	Publish(subj string, data []byte) error
}

// NewRecorder returns the Recorder that puts every authorization decision on its way to the audit
// table. It decorates next rather than replacing it — turning the table on must not take away the log
// lines an operator already relies on, and turning it off must not silence the authorization layer.
//
// Nothing here can fail an operation. A publish that does not go through, and a record that cannot
// even be rendered, both fall through to next: the record changes destination rather than
// disappearing, and the destination it falls back to is the one ADR 0009 considered sufficient.
func NewRecorder(p publisher, next authz.Recorder) authz.Recorder {
	return &recorder{pub: p, next: next}
}

type recorder struct {
	pub  publisher
	next authz.Recorder
}

func (r *recorder) Record(d authz.Decision) {
	defer r.next.Record(d)

	record := NewRecord(d)

	data, err := Marshal(record)
	if err != nil {
		slog.Error("cannot render an Audit Record; it stays in the log only",
			"id", record.ID, "err", err)
		return
	}

	// nc.Publish and not an acknowledged one: an ack would add a round trip to the request path of
	// every authorized operation, and the two callers of ConfigureStream plus the
	// pending-with-no-consumers warning stand in for the failure it would have caught (ADR 0010).
	if err := r.pub.Publish(Subject(d.Outcome), data); err != nil {
		slog.Error("cannot publish an Audit Record; it stays in the log only",
			"id", record.ID, "err", err)
	}
}
