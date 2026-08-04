package audit

import (
	"encoding/json"
	"fmt"

	"github.com/kannon-email/kannon/internal/authz"
)

// The stream and the subjects an Audit Record travels on. StreamName and StreamSubjects are exported
// because two runnables configure the same stream and a consumer subscribes to it, and a second
// spelling of either would be a second stream nobody reads.
const (
	StreamName     = "kannon-audit"
	StreamSubjects = "kannon.audit.*"

	subjectPrefix = "kannon.audit."
)

// Subject is where a Record is published. Two subjects for three outcomes, and deliberately: the
// point of putting the outcome in the subject is that an operator can alert on refusals without
// querying the table, and a request nothing authenticated is a refusal. The distinction between the
// two kinds of refusal is not lost — it is in the record, which is where a reader can filter on it.
func Subject(o authz.Outcome) string {
	if o == authz.Allowed {
		return subjectPrefix + string(authz.Allowed)
	}
	return subjectPrefix + string(authz.Denied)
}

// Marshal renders a Record for the wire. JSON, where everything else in Kannon crossing NATS is
// proto: the payload lands in a jsonb column, so proto here would mean two schemas to keep aligned
// and a permanently public type for an event no external client will ever see (ADR 0010).
func Marshal(r Record) ([]byte, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal audit record: %w", err)
	}
	return data, nil
}

// Unmarshal reads a Record off the wire, refusing one that is not worth a row. Both failures are
// permanent — a payload does not become parseable on redelivery — which is why a consumer abandons
// what this refuses rather than asking for it again.
func Unmarshal(data []byte) (Record, error) {
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return Record{}, fmt.Errorf("cannot unmarshal audit record: %w", err)
	}
	if err := r.validate(); err != nil {
		return Record{}, fmt.Errorf("audit record is not well formed: %w", err)
	}
	return r, nil
}
