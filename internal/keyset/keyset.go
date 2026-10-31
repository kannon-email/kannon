// Package keyset describes a bounded read in a fixed order, resumed strictly after a row position.
// Transports own cursor syntax; operations and repositories only ever see the typed position.
package keyset

// Page asks for at most Limit rows that follow After in the collection's fixed order.
// A nil After starts at the beginning. K is the collection's position type.
type Page[K any] struct {
	After *K
	Limit int
}
