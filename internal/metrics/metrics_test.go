package metrics

import "testing"

func TestClassifyEndpoint(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		// Sites
		{"https://pangolin.example.com/v1/org/org1/pick-site-defaults", "site_defaults"},
		{"https://pangolin.example.com/v1/org/org1/site", "site"},
		{"https://pangolin.example.com/v1/site/42", "site"},

		// Resources
		{"https://pangolin.example.com/v1/org/org1/resource", "resource"},
		{"https://pangolin.example.com/v1/resource/7", "resource"},

		// Targets (direct and sub-resource)
		{"https://pangolin.example.com/v1/resource/7/target", "target"},
		{"https://pangolin.example.com/v1/target/99", "target"},

		// Rules (direct and sub-resource)
		{"https://pangolin.example.com/v1/resource/7/rule", "rule"},
		{"https://pangolin.example.com/v1/rule/55", "rule"},

		// Site resources
		{"https://pangolin.example.com/v1/org/org1/site-resource", "site_resource"},
		{"https://pangolin.example.com/v1/site-resource/12", "site_resource"},

		// Domains
		{"https://pangolin.example.com/v1/org/org1/domains", "domain"},

		// Edge cases
		{"https://pangolin.example.com/v1/unknown-path", "unknown"},
		{"://invalid", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := ClassifyEndpoint(tt.url); got != tt.want {
				t.Errorf("ClassifyEndpoint(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
