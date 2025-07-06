package sqlc_test

import (
	"reflect"
	"testing"

	sqlc "github.com/kannon-email/kannon/internal/db"
)

func TestReadWriteAttachment(t *testing.T) {
	var testData = []struct {
		name string
		data sqlc.Attachments
	}{
		{
			name: "empty attachment",
			data: sqlc.Attachments{},
		},
		{
			name: "single file attachment",
			data: sqlc.Attachments{
				"file1.txt": []byte("this is a file"),
			},
		},
		{
			name: "nil attachment",
			data: nil,
		},
	}

	for _, tt := range testData {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal the attachment
			value, err := tt.data.Value()
			if err != nil {
				t.Fatalf("error marshaling attachment: %v", err)
			}

			// Unmarshal the attachment
			var att sqlc.Attachments
			err = att.Scan(value)
			if err != nil {
				t.Fatalf("error unmarshaling attachment: %v", err)
			}

			// Check if the attachments are the same
			if !reflect.DeepEqual(tt.data, att) {
				t.Fatalf("attachments are not equal: %v != %v", tt.data, att)
			}
		})
	}
}
