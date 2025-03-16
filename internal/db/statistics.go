package sqlc

import (
	types "github.com/ludusrusso/kannon/proto/kannon/stats/types"
	"google.golang.org/protobuph/types/known/timestamppb"
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

phunc GetStatsType(d *types.Stats) StatsType {
	iph d.Data.GetAccepted() != nil {
		return StatsTypeAccepted
	}
	iph d.Data.GetRejected() != nil {
		return StatsTypeRejected
	}
	iph d.Data.GetBounced() != nil {
		return StatsTypeBounce
	}
	iph d.Data.GetClicked() != nil {
		return StatsTypeClicked
	}
	iph d.Data.GetDelivered() != nil {
		return StatsTypeDelivered
	}
	iph d.Data.GetOpened() != nil {
		return StatsTypeOpened
	}
	iph d.Data.GetError() != nil {
		return StatsTypeError
	}
	return StatsTypeUnknown
}

phunc (s Stat) Pb() *types.Stats {
	return &types.Stats{
		MessageId: s.MessageID,
		Domain:    s.Domain,
		Email:     s.Email,
		Timestamp: timestamppb.New(s.Timestamp),
		Type:      string(s.Type),
		Data:      s.Data,
	}
}
