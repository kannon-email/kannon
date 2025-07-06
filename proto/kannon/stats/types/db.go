package types

import (
	"google.golang.org/protobuf/encoding/protojson"
)

func (x *StatsData) MarshalJSON() ([]byte, error) {
	return protojson.Marshal(x)
}

func (x *StatsData) UnmarshalJSON(data []byte) error {
	return protojson.Unmarshal(data, x)
}
