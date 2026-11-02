package glance

import (
	"encoding/json"
	"testing"
)

func TestExtractFirstJsonArray(t *testing.T) {
	contents := `prefix "events":[{"name":"a]b","nested":[1,2]}],"next":true`

	raw, ok := extractFirstJsonArray(contents, `"events":[`)
	if !ok {
		t.Fatal("expected to find events array")
	}

	var events []struct {
		Name   string `json:"name"`
		Nested []int  `json:"nested"`
	}

	if err := json.Unmarshal(raw, &events); err != nil {
		t.Fatalf("unmarshaling extracted array: %v", err)
	}

	if len(events) != 1 || events[0].Name != "a]b" || len(events[0].Nested) != 2 {
		t.Errorf("unexpected events: %+v", events)
	}
}

func TestFormatSteamPrice(t *testing.T) {
	tests := []struct {
		cents *int
		want  string
	}{
		{nil, "Chưa có giá"},
		{new(int), "Chưa có giá"},
		{intPtr(10800000), "108.000 ₫"},
		{intPtr(38500000), "385.000 ₫"},
	}

	for _, test := range tests {
		if got := formatSteamPrice(test.cents); got != test.want {
			t.Errorf("formatSteamPrice(%v) = %q, want %q", test.cents, got, test.want)
		}
	}
}

func intPtr(value int) *int {
	return &value
}
