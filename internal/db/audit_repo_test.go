package sqlc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kannon-email/kannon/internal/audit"
	"github.com/kannon-email/kannon/internal/authz"
)

func TestAuditRepository(t *testing.T) {
	repo := NewAuditRepository(db)
	audit.RunRepoSpec(t, repo, readAuditRecords)
}

// readAuditRecords is how the specification sees what it wrote, and it is raw SQL in a test file
// rather than a method on AuditRepository deliberately. The table is write-only and nothing
// authorises reading it (ADR 0010), so production must not gain a read path merely because a test
// needs one: a query that exists is a query something can be made to call, and an authorization
// decision able to consult the register of earlier decisions would no longer be a decision about
// authority. Filtering by principal is what lets the specification share one database with every
// other suite in this package without seeing rows it did not write.
func readAuditRecords(ctx context.Context, principal string) ([]audit.Record, error) {
	rows, err := db.Query(ctx,
		"SELECT id, occurred_at, principal, resource, action, outcome, data FROM audit_records WHERE principal = $1 ORDER BY occurred_at",
		principal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []audit.Record
	for rows.Next() {
		var (
			rec        audit.Record
			occurredAt time.Time
			action     string
			outcome    string
			data       []byte
		)
		if err := rows.Scan(&rec.ID, &occurredAt, &rec.Principal, &rec.Resource, &action, &outcome, &data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &rec.Details); err != nil {
			return nil, err
		}
		rec.OccurredAt = occurredAt
		rec.Action = authz.Action(action)
		rec.Outcome = authz.Outcome(outcome)
		records = append(records, rec)
	}
	return records, rows.Err()
}
