package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const maxBodyBytes = 10 << 20

func decodeBody[T any](body io.Reader) (T, error) {
	value, _, err := decodeBodyBytes[T](body)
	return value, err
}

// Only operation adapters needing required-member presence retain the bounded bytes.
func decodeBodyBytes[T any](body io.Reader) (value T, raw []byte, err error) {
	raw, err = io.ReadAll(io.LimitReader(body, maxBodyBytes+1))
	if len(raw) > maxBodyBytes {
		return value, nil, problemError(413, "request_body_too_large")
	}
	if err != nil {
		return value, nil, problemError(400, "malformed_request")
	}
	var tree any
	if json.Unmarshal(raw, &tree) == nil {
		if pointer, found := nullPointer(tree, ""); found {
			return value, nil, &requestError{status: 422, code: "validation_failed", fields: []FieldError{{Code: "validation_failed", Pointer: pointer, Detail: "Null is not allowed"}}}
		}
	}
	err = strictJSON(raw, &value)
	return value, raw, err
}

func strictJSON(raw []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err == nil {
		var tail any
		if d.Decode(&tail) == io.EOF {
			return nil
		}
	}
	return problemError(400, "malformed_request")
}

// Request schemas contain no nullable inputs. Check presence before decoding into
// Go pointers, which otherwise conflate explicit null with an omitted member.
func nullPointer(v any, path string) (string, bool) {
	if v == nil {
		return path, true
	}
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			escaped := strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
			if p, ok := nullPointer(v, path+"/"+escaped); ok {
				return p, true
			}
		}
	case []any:
		for i, v := range x {
			if p, ok := nullPointer(v, fmt.Sprintf("%s/%d", path, i)); ok {
				return p, true
			}
		}
	}
	return "", false
}
