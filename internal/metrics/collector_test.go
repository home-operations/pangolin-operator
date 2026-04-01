package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = pangolinv1alpha1.AddToScheme(s)
	return s
}

func collectMetrics(t *testing.T, c *ResourceCollector) []prometheus.Metric {
	t.Helper()
	ch := make(chan prometheus.Metric, 64)
	c.Collect(ch)
	close(ch)
	var got []prometheus.Metric
	for m := range ch {
		got = append(got, m)
	}
	return got
}

func TestCollector_EmptyCluster(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newScheme()).Build()
	c := NewResourceCollector(cl)

	metrics := collectMetrics(t, c)
	if len(metrics) != 0 {
		t.Errorf("expected 0 metrics for empty cluster, got %d", len(metrics))
	}
}

func TestCollector_CountsByPhase(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newScheme()).
		WithObjects(
			&pangolinv1alpha1.NewtSite{
				ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "default"},
				Status:     pangolinv1alpha1.NewtSiteStatus{Phase: pangolinv1alpha1.NewtSitePhaseReady, Online: true},
			},
			&pangolinv1alpha1.NewtSite{
				ObjectMeta: metav1.ObjectMeta{Name: "s2", Namespace: "default"},
				Status:     pangolinv1alpha1.NewtSiteStatus{Phase: pangolinv1alpha1.NewtSitePhaseReady},
			},
			&pangolinv1alpha1.NewtSite{
				ObjectMeta: metav1.ObjectMeta{Name: "s3", Namespace: "other"},
				Status:     pangolinv1alpha1.NewtSiteStatus{Phase: pangolinv1alpha1.NewtSitePhaseError},
			},
			&pangolinv1alpha1.PublicResource{
				ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
				Status:     pangolinv1alpha1.PublicResourceStatus{Phase: pangolinv1alpha1.PublicResourcePhaseReady},
			},
			&pangolinv1alpha1.PrivateResource{
				ObjectMeta: metav1.ObjectMeta{Name: "pr1", Namespace: "default"},
				Status:     pangolinv1alpha1.PrivateResourceStatus{Phase: pangolinv1alpha1.PrivateResourcePhaseCreating},
			},
		).Build()

	c := NewResourceCollector(cl)
	got := collectMetrics(t, c)

	if len(got) != 7 {
		t.Errorf("expected 7 metrics, got %d", len(got))
	}
}

func TestCollector_EmptyPhaseDefaultsToPending(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newScheme()).
		WithObjects(
			&pangolinv1alpha1.NewtSite{
				ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "default"},
			},
		).Build()

	c := NewResourceCollector(cl)
	got := collectMetrics(t, c)

	if len(got) != 2 {
		t.Errorf("expected 2 metrics, got %d", len(got))
	}
}

func TestCollector_Describe(t *testing.T) {
	c := NewResourceCollector(nil)
	ch := make(chan *prometheus.Desc, 10)
	c.Describe(ch)
	close(ch)

	var descs []*prometheus.Desc
	for d := range ch {
		descs = append(descs, d)
	}
	if len(descs) != 3 {
		t.Errorf("expected 3 descriptors, got %d", len(descs))
	}
}
