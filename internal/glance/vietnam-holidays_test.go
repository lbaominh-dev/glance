package glance

import (
	"encoding/json"
	"testing"
)

func TestLunarToSolarTet(t *testing.T) {
	tests := []struct {
		lunarYear int
		wantDay   int
		wantMonth int
		wantYear  int
	}{
		{2024, 10, 2, 2024},
		{2025, 29, 1, 2025},
		{2026, 17, 2, 2026},
		{2027, 6, 2, 2027},
	}

	for _, test := range tests {
		day, month, year := lunarToSolar(1, 1, test.lunarYear, vietnamTimeZone)
		if day != test.wantDay || month != test.wantMonth || year != test.wantYear {
			t.Errorf("Tết %d = %02d/%02d/%d, want %02d/%02d/%d",
				test.lunarYear, day, month, year, test.wantDay, test.wantMonth, test.wantYear)
		}
	}
}

func TestVietnamHolidaysJSON(t *testing.T) {
	var holidays map[string]string

	if err := json.Unmarshal([]byte(vietnamHolidaysJSON), &holidays); err != nil {
		t.Fatalf("unmarshaling holidays: %v", err)
	}

	expected := map[string]string{
		"2026-01-01": "Tết Dương lịch",
		"2026-02-17": "Tết Nguyên Đán",
		"2026-04-30": "Ngày Giải phóng miền Nam",
		"2026-05-01": "Quốc tế Lao động",
		"2026-09-02": "Quốc khánh",
	}

	for date, name := range expected {
		if holidays[date] != name {
			t.Errorf("holiday on %s = %q, want %q", date, holidays[date], name)
		}
	}
}
