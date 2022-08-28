package types

import (
	"database/sql/driver"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
)

func (a *StatsData) Scan(src interface{}) error {
	switch s := src.(type) {
	case []byte:
		if err := protojson.Unmarshal(s, a); err != nil {
			return err
		}
	case string:
		if err := protojson.Unmarshal([]byte(s), a); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported scan type for StatsData: %T", src)
	}
	return nil
}

func (a *StatsData) Value() (driver.Value, error) {
	return protojson.Marshal(a)
}
