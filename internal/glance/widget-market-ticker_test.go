package glance

import "testing"

func TestFormatVietnameseNumber(t *testing.T) {
	tests := []struct {
		value    float64
		decimals int
		want     string
	}{
		{1710.72, 2, "1.710,72"},
		{-30.9, 2, "-30,90"},
		{0, 2, "0,00"},
		{140000000 / 1000000.0, 1, "140,0"},
		{1234567.89, 2, "1.234.567,89"},
	}

	for _, test := range tests {
		got := formatVietnameseNumber(test.value, test.decimals)
		if got != test.want {
			t.Errorf("formatVietnameseNumber(%v, %d) = %q, want %q",
				test.value, test.decimals, got, test.want)
		}
	}
}

func TestMarketIndexLabel(t *testing.T) {
	tests := map[string]string{
		"VNINDEX":       "VN-Index",
		"vn30":          "VN30",
		"HNXIndex":      "HNX-Index",
		"HNXUpcomIndex": "UPCOM",
		"UNKNOWN":       "UNKNOWN",
	}

	for symbol, want := range tests {
		if got := marketIndexLabel(symbol); got != want {
			t.Errorf("marketIndexLabel(%q) = %q, want %q", symbol, got, want)
		}
	}
}

func TestTickerChartPoints(t *testing.T) {
	if points := tickerChartPoints([]float64{1, 1, 1}); points != "" {
		t.Errorf("expected no chart for flat values, got %q", points)
	}

	if points := tickerChartPoints([]float64{1}); points != "" {
		t.Errorf("expected no chart for a single value, got %q", points)
	}

	points := tickerChartPoints([]float64{1, 2, 0, 3})
	if points == "" {
		t.Error("expected chart points for varying values")
	}
}

func TestTickerChangeClass(t *testing.T) {
	tests := map[float64]string{
		1.2:  "color-positive",
		-1.2: "color-negative",
		0:    "color-subdue",
	}

	for change, want := range tests {
		if got := tickerChangeClass(change); got != want {
			t.Errorf("tickerChangeClass(%v) = %q, want %q", change, got, want)
		}
	}
}
