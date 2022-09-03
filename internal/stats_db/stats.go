package sq

import (
	types "github.com/ludusrusso/kannon/proto/kannon/stats/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type StatsType string

const (
	StatsTypeAccepted  StatsType = "accepted"
	StatsTypeRejected  StatsType = "rejected"
	StatsTypeDelivered StatsType = "delivered"
	StatsTypeOpened    StatsType = "opened"
	StatsTypeClicked   StatsType = "clicked"
	StatsTypeBounce    StatsType = "bounced"
	StatsTypeError     StatsType = "error"
	StatsTypeUnknown   StatsType = "unknown"
)

func GetStatsType(d *types.Stats) StatsType {
	if d.Data.GetAccepted() != nil {
		return StatsTypeAccepted
	}
	if d.Data.GetRejected() != nil {
		return StatsTypeRejected
	}
	if d.Data.GetBounced() != nil {
		return StatsTypeBounce
	}
	if d.Data.GetClicked() != nil {
		return StatsTypeClicked
	}
	if d.Data.GetDelivered() != nil {
		return StatsTypeDelivered
	}
	if d.Data.GetOpened() != nil {
		return StatsTypeOpened
	}
	if d.Data.GetError() != nil {
		return StatsTypeError
	}
	return StatsTypeUnknown
}

func (s Stat) Pb() *types.Stats {
	return &types.Stats{
		MessageId: s.MessageID,
		Domain:    s.Domain,
		Email:     s.Email,
		Timestamp: timestamppb.New(s.Timestamp),
		Type:      string(s.Type),
		Data:      s.Data,
	}
}
