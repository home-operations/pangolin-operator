package metrics

import "testing"

func TestClassifyEndpoint(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		// Sites
		{"https://pangolin.example.com/v1/org/org1/pick-site-defaults", "site_defaults"},
		{"https://pangolin.example.com/v1/org/org1/site", endpointSite},
		{"https://pangolin.example.com/v1/site/42", endpointSite},

		// Resources
		{"https://pangolin.example.com/v1/org/org1/resource", endpointResource},
		{"https://pangolin.example.com/v1/resource/7", endpointResource},

		// Targets (direct and sub-resource)
		{"https://pangolin.example.com/v1/resource/7/target", endpointTarget},
		{"https://pangolin.example.com/v1/target/99", endpointTarget},

		// Rules (direct and sub-resource)
		{"https://pangolin.example.com/v1/resource/7/rule", endpointRule},
		{"https://pangolin.example.com/v1/rule/55", endpointRule},

		// Site resources
		{"https://pangolin.example.com/v1/org/org1/site-resource", endpointSiteResource},
		{"https://pangolin.example.com/v1/site-resource/12", endpointSiteResource},

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
