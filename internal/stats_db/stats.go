package sq

import (
	"github.com/ludusrusso/kannon/generated/pb/stats/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type StatsType string

const (
	StatsTypePrepared  StatsType = "accepted"
	StatsTypeDelivered StatsType = "delivered"
	StatsTypeOpened    StatsType = "opened"
	StatsTypeClicked   StatsType = "clicked"
	StatsTypeBounce    StatsType = "bounced"
)

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
