// Package trackingpb translates Tracking Policies between the wire enums and
// the internal/tracking domain types. It is the only place that knows both, so
// that internal/tracking stays free of any protobuf dependency and every API
// boundary that accepts a Policy rejects the same values.
package trackingpb

import (
	"errors"
	"fmt"

	"github.com/kannon-email/kannon/internal/tracking"
	pb "github.com/kannon-email/kannon/proto/kannon/tracking/types"
)

var (
	// ErrUnknownMode is returned for a wire value this build does not know,
	// which normally means a client built against a newer schema.
	ErrUnknownMode = errors.New("unknown tracking mode")
	// ErrUnsupportedMode is returned for a Mode that exists on the scale but
	// is not implemented, so that selecting it fails loudly instead of being
	// silently treated as something else.
	ErrUnsupportedMode = errors.New("unsupported tracking mode")
)

// wireModes maps every wire value this build knows to its domain Mode.
var wireModes = map[pb.TrackingMode]tracking.Mode{
	pb.TrackingMode_TRACKING_MODE_UNSPECIFIED:  tracking.ModeUnspecified,
	pb.TrackingMode_TRACKING_MODE_OFF:          tracking.ModeOff,
	pb.TrackingMode_TRACKING_MODE_ANONYMOUS:    tracking.ModeAnonymous,
	pb.TrackingMode_TRACKING_MODE_PSEUDONYMOUS: tracking.ModePseudonymous,
	pb.TrackingMode_TRACKING_MODE_IDENTIFIED:   tracking.ModeIdentified,
	pb.TrackingMode_TRACKING_MODE_FULL:         tracking.ModeFull,
}

// ToPolicy translates a Policy stated on the wire into the domain type. A nil
// message states nothing. Pseudonymous is rejected with ErrUnsupportedMode
// because it is reserved and not implemented.
func ToPolicy(p *pb.TrackingPolicy) (tracking.Policy, error) {
	if p == nil {
		return tracking.Policy{}, nil
	}
	opens, err := toMode(p.GetOpens())
	if err != nil {
		return tracking.Policy{}, fmt.Errorf("opens: %w", err)
	}
	links, err := toMode(p.GetLinks())
	if err != nil {
		return tracking.Policy{}, fmt.Errorf("links: %w", err)
	}
	return tracking.Policy{Opens: opens, Links: links}, nil
}

// domainModes is wireModes reversed, so a translation is a lookup in either
// direction and the mapping is stated once.
var domainModes = func() map[tracking.Mode]pb.TrackingMode {
	out := make(map[tracking.Mode]pb.TrackingMode, len(wireModes))
	for wire, mode := range wireModes {
		out[mode] = wire
	}
	return out
}()

// FromPolicy translates a Policy into its wire representation. A Mode this
// build does not know becomes unspecified, since the two zero values coincide.
func FromPolicy(p tracking.Policy) *pb.TrackingPolicy {
	return &pb.TrackingPolicy{
		Opens: FromMode(p.Opens),
		Links: FromMode(p.Links),
	}
}

// FromMode translates a single Mode into its wire representation, for the
// boundaries that state one Mode rather than a whole Policy — a stat event
// describes one engagement channel, so it carries one Mode. A Mode this build
// does not know becomes unspecified, since the two zero values coincide.
func FromMode(m tracking.Mode) pb.TrackingMode {
	return domainModes[m]
}

func toMode(m pb.TrackingMode) (tracking.Mode, error) {
	mode, ok := wireModes[m]
	if !ok {
		return tracking.ModeUnspecified, fmt.Errorf("%w: %d", ErrUnknownMode, m)
	}
	if mode == tracking.ModePseudonymous {
		return tracking.ModeUnspecified, fmt.Errorf("%w: %s", ErrUnsupportedMode, mode)
	}
	return mode, nil
}
