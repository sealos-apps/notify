package database

import (
	"database/sql/driver"
	"encoding/json"
	"reflect"
	"testing"
)

func TestJSONMapValue(t *testing.T) {
	value, err := JSONMap{"name": "Alice", "count": float64(2)}.Value()
	if err != nil {
		t.Fatalf("Value returned error: %v", err)
	}

	raw, ok := value.([]byte)
	if !ok {
		t.Fatalf("Value returned %T, want []byte", value)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Value returned invalid JSON: %v", err)
	}
	if decoded["name"] != "Alice" || decoded["count"] != float64(2) {
		t.Fatalf("unexpected JSON value: %#v", decoded)
	}
}

func TestJSONMapNilValue(t *testing.T) {
	value, err := (JSONMap)(nil).Value()
	if err != nil {
		t.Fatalf("Value returned error: %v", err)
	}
	if value != nil {
		t.Fatalf("nil JSONMap Value = %#v, want nil", value)
	}

	var _ driver.Valuer = JSONMap{}
}

func TestJSONMapScan(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  JSONMap
	}{
		{name: "bytes", value: []byte(`{"email":"alice@example.com"}`), want: JSONMap{"email": "alice@example.com"}},
		{name: "string", value: `{"phone":"+12025550123"}`, want: JSONMap{"phone": "+12025550123"}},
		{name: "nil", value: nil, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got JSONMap
			if err := got.Scan(tt.value); err != nil {
				t.Fatalf("Scan returned error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Scan = %#v, want %#v", got, tt.want)
			}
		})
	}
}
