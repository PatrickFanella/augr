package postgres

import (
	"encoding/json"
	"reflect"
)

// jsonBytesEqual compares two JSON byte slices for semantic equality,
// ignoring differences in whitespace and key ordering that PostgreSQL
// JSONB may introduce.
func jsonBytesEqual(a, b json.RawMessage) bool {
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}
