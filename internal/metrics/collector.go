package metrics

import (
	"context"
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
)

type ResourceCollector struct {
	client client.Reader
}

func NewResourceCollector(c client.Reader) *ResourceCollector {
	return &ResourceCollector{client: c}
}

func (c *ResourceCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- phaseDesc
	ch <- onlineDesc
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
		return
	}

	counts := make(map[string]float64)
	for i := range list.Items {
		site := &list.Items[i]
		phase := string(site.Status.Phase)
		if phase == "" {
			phase = "Pending"
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
		return
	}

	counts := make(map[string]float64)
	for i := range list.Items {
		phase := string(list.Items[i].Status.Phase)
		if phase == "" {
			phase = "Pending"
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
		return
	}

	counts := make(map[string]float64)
	for i := range list.Items {
		phase := string(list.Items[i].Status.Phase)
		if phase == "" {
			phase = "Pending"
		}
		counts[phase]++
	}
	for phase, count := range counts {
		ch <- prometheus.MustNewConstMetric(phaseDesc, prometheus.GaugeValue, count, "privateresource", phase)
	}
}
