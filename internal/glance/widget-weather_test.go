package glance

import "testing"

func TestAirQualityCategory(t *testing.T) {
	tests := []struct {
		aqi         int
		category    string
		description string
		classname   string
	}{
		{0, "Good", "Good", "good"},
		{50, "Good", "Good", "good"},
		{51, "Moderate", "Moderate", "moderate"},
		{100, "Moderate", "Moderate", "moderate"},
		{101, "Sensitive", "Unhealthy for Sensitive Groups", "sensitive"},
		{150, "Sensitive", "Unhealthy for Sensitive Groups", "sensitive"},
		{151, "Unhealthy", "Unhealthy", "unhealthy"},
		{200, "Unhealthy", "Unhealthy", "unhealthy"},
		{201, "Very Unhealthy", "Very Unhealthy", "very-unhealthy"},
		{300, "Very Unhealthy", "Very Unhealthy", "very-unhealthy"},
		{301, "Hazardous", "Hazardous", "hazardous"},
		{500, "Hazardous", "Hazardous", "hazardous"},
	}

	for _, test := range tests {
		category, description, classname := airQualityCategory(test.aqi)
		if category != test.category || description != test.description || classname != test.classname {
			t.Errorf("airQualityCategory(%d) = (%q, %q, %q), want (%q, %q, %q)",
				test.aqi, category, description, classname, test.category, test.description, test.classname)
		}
	}
}
