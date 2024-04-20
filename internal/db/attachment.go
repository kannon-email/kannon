package sqlc

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

var ErrInvalidAttachment = fmt.Errorf("invalid attachment")

type Attachments map[string][]byte

// implement Vauler interface
func (a Attachments) Value() (driver.Value, error) {
	return json.Marshal(a)
}

// implement Scanner interface
func (a *Attachments) Scan(src interface{}) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("%w: %w", ErrInvalidAttachment, err)
		}
	}()

	if src == nil {
		*a = nil
		return nil
	}

	var byteSrc []byte

	switch s := src.(type) {
	case []byte:
		byteSrc = s
	case string:
		byteSrc = []byte(s)
	default:
		return fmt.Errorf("unsupported scan type for TemplateType: %T", src)
	}

	err = json.Unmarshal(byteSrc, a)

	return
}
