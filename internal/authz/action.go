package authz

import "fmt"

// Action is one verb a Principal may be allowed to perform on a Resource. The vocabulary is
// closed and says nothing about what is acted on — the Resource carries that — so a new kind
// of thing adds a path rather than an Action, and no nonsensical Grant can be written down.
type Action string

const (
	// Create covers both making a thing and, on a Domain's Batches, sending mail. There is
	// no send Action: CONTEXT.md defines a Batch as the aggregate one Mailer API call
	// creates, so a send is a creation, and a send verb would be the only type-bound one.
	Create Action = "create"

	// Read inspects one thing.
	Read Action = "read"

	// List enumerates what exists. Separate from Read because prefix domination makes a
	// collection and its items one Resource family: with one verb, knowing which things
	// exist could never be granted apart from inspecting them.
	List Action = "list"

	// Update changes a thing.
	Update Action = "update"

	// Delete removes a thing, including irreversible deactivation of an API
	// Key — which is a removal rather than a change.
	Delete Action = "delete"

	// Attribute permits stating an Attribution: naming who asked, on the far side of a
	// caller Kannon cannot see into. An Action rather than a flag on the Principal, so it
	// can be permitted within part of the tree. No Role holds it yet — see ADR 0008.
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
