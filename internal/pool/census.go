package pool

import (
	"context"
	"slices"
)

// Status is a status a Delivery holds while it is in the Pool. The statuses are the Pool's
// internal mechanics; they are exposed to operators only through kannon_pool_deliveries (ADR 0018).
type Status string

const (
	StatusToValidate Status = "to_validate"
	StatusValidating Status = "validating"
	StatusScheduled  Status = "scheduled"
	StatusSending    Status = "sending"
)

var statuses = []Status{StatusToValidate, StatusValidating, StatusScheduled, StatusSending}

// Statuses lists every Status, in a stable order.
func Statuses() []Status {
	return slices.Clone(statuses)
}

// Census counts the Deliveries in the Pool by Status.
type Census interface {
	DeliveriesByStatus(ctx context.Context) (map[Status]int, error)
}
