package metrics

import (
	"net/url"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const namespace = "pangolin"

var (
	APIRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "api",
		Name:      "request_duration_seconds",
		Help:      "Latency of Pangolin API requests.",
	}, []string{"method", "endpoint"})

	APIRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "api",
		Name:      "requests_total",
		Help:      "Total Pangolin API requests by result.",
	}, []string{"method", "endpoint", "status"})
)

func init() {
	ctrlmetrics.Registry.MustRegister(APIRequestDuration, APIRequestsTotal)
}

func ClassifyEndpoint(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "unknown"
	}

	path := strings.TrimPrefix(u.Path, "/v1/")
	if strings.HasPrefix(path, "org/") {
		parts := strings.SplitN(path, "/", 3)
		if len(parts) >= 3 {
			path = parts[2] // everything after org/{id}/
		}
	}

	segments := strings.Split(strings.TrimRight(path, "/"), "/")
	first := segments[0]

	switch first {
	case "pick-site-defaults":
		return "site_defaults"
	case "site":
		return "site"
	case "resource":
		if len(segments) >= 3 {
			switch segments[2] {
			case "target":
				return "target"
			case "rule":
				return "rule"
			}
		}
		return "resource"
	case "target":
		return "target"
	case "rule":
		return "rule"
	case "site-resource":
		return "site_resource"
	case "domains":
		return "domain"
	default:
		return "unknown"
	}
}
