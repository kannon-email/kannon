package types

import (
	"database/sql/driver"
	"fmt"

	"github.com/networkteam/obfuscate"
	"google.golang.org/protobuf/encoding/protojson"
)

func (d *StatsData) Scan(src interface{}) error {
	switch s := src.(type) {
	case []byte:
		if err := protojson.Unmarshal(s, d); err != nil {
			return err
		}
	case string:
		if err := protojson.Unmarshal([]byte(s), d); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported scan type for StatsData: %T", src)
	}
	return nil
}

func (d *StatsData) Value() (driver.Value, error) {
	return protojson.Marshal(d)
}

func (d *Stats) GetObfuscatedEmail() string {
	return obfuscate.EmailAddressPartially(d.Email)
}
