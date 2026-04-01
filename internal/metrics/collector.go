package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
)

var (
	phaseDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "resource", "phase"),
		"Number of Pangolin resources by kind and phase.",
		[]string{"kind", "phase"}, nil,
	)
	onlineDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "newtsite", "online"),
		"Whether a NewtSite tunnel is online (1) or offline (0).",
		[]string{"name", "namespace"}, nil,
	)
	collectErrorDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "collector", "errors_total"),
		"Number of errors encountered while collecting metrics.",
		[]string{"kind"}, nil,
	)
)

const defaultPhase = string(pangolinv1alpha1.NewtSitePhasePending)

type ResourceCollector struct {
	client client.Reader
}

func NewResourceCollector(c client.Reader) *ResourceCollector {
	return &ResourceCollector{client: c}
}

func (c *ResourceCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- phaseDesc
	ch <- onlineDesc
	ch <- collectErrorDesc
}

func (c *ResourceCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c.collectNewtSites(ctx, ch)
	c.collectPublicResources(ctx, ch)
	c.collectPrivateResources(ctx, ch)
}

func (c *ResourceCollector) collectNewtSites(ctx context.Context, ch chan<- prometheus.Metric) {
	var list pangolinv1alpha1.NewtSiteList
	if err := c.client.List(ctx, &list); err != nil {
		slog.Error("failed to list NewtSites for metrics", "error", err)
		ch <- prometheus.MustNewConstMetric(collectErrorDesc, prometheus.CounterValue, 1, "newtsite")
		return
	}

	counts := make(map[string]float64)
	for i := range list.Items {
		site := &list.Items[i]
		phase := string(site.Status.Phase)
		if phase == "" {
			phase = defaultPhase
		}
		counts[phase]++

		online := 0.0
		if site.Status.Online {
			online = 1.0
		}
		ch <- prometheus.MustNewConstMetric(onlineDesc, prometheus.GaugeValue, online, site.Name, site.Namespace)
	}
	for phase, count := range counts {
		ch <- prometheus.MustNewConstMetric(phaseDesc, prometheus.GaugeValue, count, "newtsite", phase)
	}
}

func (c *ResourceCollector) collectPublicResources(ctx context.Context, ch chan<- prometheus.Metric) {
	var list pangolinv1alpha1.PublicResourceList
	if err := c.client.List(ctx, &list); err != nil {
		slog.Error("failed to list PublicResources for metrics", "error", err)
		ch <- prometheus.MustNewConstMetric(collectErrorDesc, prometheus.CounterValue, 1, "publicresource")
		return
	}

	counts := make(map[string]float64)
	for i := range list.Items {
		phase := string(list.Items[i].Status.Phase)
		if phase == "" {
			phase = defaultPhase
		}
		counts[phase]++
	}
	for phase, count := range counts {
		ch <- prometheus.MustNewConstMetric(phaseDesc, prometheus.GaugeValue, count, "publicresource", phase)
	}
}

func (c *ResourceCollector) collectPrivateResources(ctx context.Context, ch chan<- prometheus.Metric) {
	var list pangolinv1alpha1.PrivateResourceList
	if err := c.client.List(ctx, &list); err != nil {
		slog.Error("failed to list PrivateResources for metrics", "error", err)
		ch <- prometheus.MustNewConstMetric(collectErrorDesc, prometheus.CounterValue, 1, "privateresource")
		return
	}

	counts := make(map[string]float64)
	for i := range list.Items {
		phase := string(list.Items[i].Status.Phase)
		if phase == "" {
			phase = defaultPhase
		}
		counts[phase]++
	}
	for phase, count := range counts {
		ch <- prometheus.MustNewConstMetric(phaseDesc, prometheus.GaugeValue, count, "privateresource", phase)
	}
}
