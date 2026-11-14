package sqlc

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

type SendingPoolStatus string

const (
	SendingPoolStatusToValidate SendingPoolStatus = "to_validate"
	SendingPoolStatusValidating SendingPoolStatus = "validating"
	SendingPoolStatusSending    SendingPoolStatus = "sending"
	SendingPoolStatusScheduled  SendingPoolStatus = "scheduled"
)

// DeliveriesByPoolStatus counts the Deliveries in the Pool by status, in the shape
// mailflow.RegisterPool reads.
func (q *Queries) DeliveriesByPoolStatus(ctx context.Context) (map[string]float64, error) {
	rows, err := q.CountPoolByStatus(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]float64, len(rows))
	for _, r := range rows {
		counts[string(r.Status)] = r.Deliveries
	}
	return counts, nil
}

type CustomFields map[string]string

func (c *CustomFields) Scan(src interface{}) error {
	switch s := src.(type) {
	case []byte:
		var m map[string]string
		if err := json.Unmarshal(s, &m); err != nil {
			return err
		}
		*c = CustomFields(m)
	case string:
		var m map[string]string
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			return err
		}
		*c = CustomFields(m)
	default:
		return fmt.Errorf("unsupported scan type for CustomFields: %T", src)
	}
	return nil
}

// implement Valuer interface for custom fields
func (c CustomFields) Value() (driver.Value, error) {
	return json.Marshal(c)
}
