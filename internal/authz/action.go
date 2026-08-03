package authz

import "fmt"

// Action is one verb a Principal may be allowed to perform on a Resource.
//
// The vocabulary is closed, and it deliberately says nothing about *what* is
// being acted on — the Resource already carries that. Two consequences follow.
// Giving Kannon a new kind of thing to manage adds a path, not an Action. And a
// nonsensical Grant cannot be written down: there is no create-template that
// could be granted over apikeys.
type Action string

const (
	// Create covers both making a thing and, on a Domain's Batches, sending
	// mail. There is no send Action: CONTEXT.md defines a Batch as the
	// aggregate one Mailer API call creates, so a send *is* a creation, and a
	// dedicated verb would be the only type-bound one in the vocabulary.
	Create Action = "create"

	// Read inspects one thing.
	Read Action = "read"

	// List enumerates what exists. Separate from Read because prefix
	// domination makes a collection and its items one Resource family: with a
	// single verb, knowing which things exist could never be granted apart
	// from inspecting them, and enumeration discloses something different from
	// inspection.
	List Action = "list"

	// Update changes a thing.
	Update Action = "update"

	// Delete removes a thing, including irreversible deactivation of an API
	// Key — which is a removal rather than a change.
	Delete Action = "delete"

	// Attribute permits stating an Attribution: naming who asked, on the far
	// side of a caller Kannon cannot see into. It is an Action on a Resource
	// like any other rather than a flag on the Principal, which costs nothing
	// and yields the ability to permit attribution only within part of the
	// tree. No Role in the catalogue holds it yet — see ADR 0008.
	Attribute Action = "attribute"
)

// resourceActions are the Actions over Kannon's own resources. Attribute is
// excluded: it is a front-end capability rather than an administrative one.
var resourceActions = []Action{Create, Read, List, Update, Delete}

// allActions is every Action in the vocabulary.
var allActions = append(append([]Action{}, resourceActions...), Attribute)

// ParseAction validates a string against the closed vocabulary.
func ParseAction(s string) (Action, error) {
	for _, a := range allActions {
		if Action(s) == a {
			return a, nil
		}
	}
	return "", fmt.Errorf("unknown action %q", s)
}

// String returns the Action's wire form.
func (a Action) String() string {
	return string(a)
}
