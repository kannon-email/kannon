package sqlc

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

type CustomFields map[string]string

func (c *CustomFields) Scan(src interface{}) error {
	switch s := src.(type) {
	case []byte:
		var m map[string]string
		if err := json.Unmarshal(s, &m); err != nil {
			return err
		}
		*c = CustomFields(m)
	case string:
		var m map[string]string
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			return err
		}
		*c = CustomFields(m)
	default:
		return fmt.Errorf("unsupported scan type for CustomFields: %T", src)
	}
	return nil
}

// implement Valuer interface for custom fields
func (c CustomFields) Value() (driver.Value, error) {
	return json.Marshal(c)
}
