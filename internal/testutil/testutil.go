package testutil

import (
	"encoding/json"
	"net/http"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
	ctrlresolve "github.com/home-operations/pangolin-operator/internal/controller/resolve"
)

// NewScheme returns a runtime.Scheme with all types used by the operator registered.
func NewScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = pangolinv1alpha1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	return s
}

// NewClientBuilder returns a fake.ClientBuilder pre-configured with the operator
// scheme and the NewtSite field index used by the resolve package.
func NewClientBuilder(scheme *runtime.Scheme) *fake.ClientBuilder {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(
			&pangolinv1alpha1.NewtSite{},
			ctrlresolve.IndexField,
			func(obj client.Object) []string { return []string{obj.GetName()} },
		)
}

// PangolinResponse writes a JSON success envelope to w.
func PangolinResponse(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	type envelope struct {
		Data    any    `json:"data"`
		Success bool   `json:"success"`
		Message string `json:"message"`
		Status  int    `json:"status"`
	}
	if err := json.NewEncoder(w).Encode(envelope{Data: data, Success: true, Status: 200}); err != nil {
		t.Fatalf("failed to write test response: %v", err)
	}
}

// ReadySite returns a NewtSite in the Ready phase with a valid SiteID.
func ReadySite() *pangolinv1alpha1.NewtSite {
	return &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{Name: "my-site", Namespace: "default"},
		Spec:       pangolinv1alpha1.NewtSiteSpec{},
		Status: pangolinv1alpha1.NewtSiteStatus{
			Phase:  pangolinv1alpha1.NewtSitePhaseReady,
			SiteID: 1,
		},
	}
}
