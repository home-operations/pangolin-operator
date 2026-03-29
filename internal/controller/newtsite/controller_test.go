package newtsite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
	"github.com/home-operations/pangolin-operator/internal/pangolin"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = pangolinv1alpha1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	return s
}

// pangolinResponse writes a success envelope to w.
func pangolinResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	type envelope struct {
		Data    any  `json:"data"`
		Success bool `json:"success"`
	}
	_ = json.NewEncoder(w).Encode(envelope{Data: data, Success: true})
}

// TestUpdateSite_CallsAPIWhenNameDiffers verifies that updateSite calls UpdateSite
// when the live name in Pangolin differs from spec.name.
func TestUpdateSite_CallsAPIWhenNameDiffers(t *testing.T) {
	updateCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site/42", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			pangolinResponse(w, pangolin.GetSiteResponse{SiteID: 42, Name: "old-name"})
		case http.MethodPost:
			updateCalled = true
			var req pangolin.UpdateSiteRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Name != "new-name" {
				t.Errorf("expected name 'new-name', got %q", req.Name)
			}
			pangolinResponse(w, nil)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cl := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{
		Endpoint: srv.URL,
		APIKey:   "key",
		OrgID:    "org1",
	})
	r := &Reconciler{Client: cl, Scheme: newTestScheme(), PangolinClient: pc}

	site := &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{Name: "my-site", Namespace: "default"},
		Spec:       pangolinv1alpha1.NewtSiteSpec{Name: "new-name"},
		Status:     pangolinv1alpha1.NewtSiteStatus{SiteID: 42},
	}

	if err := r.updateSite(context.Background(), site); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updateCalled {
		t.Error("expected UpdateSite to be called")
	}
}

// TestUpdateSite_SkipsAPIWhenNameUnchanged verifies that updateSite does NOT call
// UpdateSite when the live name already matches spec.name.
func TestUpdateSite_SkipsAPIWhenNameUnchanged(t *testing.T) {
	updateCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site/42", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			pangolinResponse(w, pangolin.GetSiteResponse{SiteID: 42, Name: "same-name"})
		case http.MethodPost:
			updateCalled = true
			pangolinResponse(w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	site := &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{Name: "my-site", Namespace: "default"},
		Spec:       pangolinv1alpha1.NewtSiteSpec{Name: "same-name"},
		Status:     pangolinv1alpha1.NewtSiteStatus{SiteID: 42},
	}
	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})

	r := &Reconciler{Client: fake.NewClientBuilder().WithScheme(newTestScheme()).Build(), Scheme: newTestScheme(), PangolinClient: pc}
	if err := r.updateSite(context.Background(), site); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updateCalled {
		t.Error("expected UpdateSite NOT to be called when name is unchanged")
	}
}

// TestReconcile_CreateSite_PassesAddress verifies the full reconcile path for a new
// site: PickSiteDefaults is called, CreateSite receives the clientAddress, and the
// credential Secret is created.
func TestReconcile_CreateSite_PassesAddress(t *testing.T) {
	const clientAddress = "100.90.128.1"
	createSiteCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/pick-site-defaults", func(w http.ResponseWriter, r *http.Request) {
		pangolinResponse(w, pangolin.PickSiteDefaultsResponse{
			NewtID:        "nid",
			NewtSecret:    "nsec",
			ClientAddress: clientAddress,
		})
	})
	mux.HandleFunc("/v1/org/org1/site", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		var req pangolin.CreateSiteRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Address != clientAddress {
			t.Errorf("expected address %q, got %q", clientAddress, req.Address)
		}
		createSiteCalled = true
		pangolinResponse(w, pangolin.CreateSiteResponse{SiteID: 10, NiceID: "nice-10"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	site := &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-site",
			Namespace:  "default",
			Finalizers: []string{NewtSiteFinalizer},
		},
		Spec: pangolinv1alpha1.NewtSiteSpec{Name: "my-site"},
	}

	scheme := newTestScheme()
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(site).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-site", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !createSiteCalled {
		t.Error("expected CreateSite to be called")
	}

	// The credential secret should exist.
	var secret corev1.Secret
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "my-site-newt-credentials", Namespace: "default"}, &secret); err != nil {
		t.Errorf("expected newt-credentials secret to be created: %v", err)
	}
}

// TestReconcile_Update_CallsUpdateSiteOnGenerationChange verifies that when a site
// already has a SiteID and generation > observedGeneration, updateSite is invoked.
func TestReconcile_Update_CallsUpdateSiteOnGenerationChange(t *testing.T) {
	updateCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site/42", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			pangolinResponse(w, pangolin.GetSiteResponse{SiteID: 42, Name: "old-name"})
		case http.MethodPost:
			updateCalled = true
			pangolinResponse(w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	site := &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-site",
			Namespace:  "default",
			Generation: 2,
			Finalizers: []string{NewtSiteFinalizer},
		},
		Spec: pangolinv1alpha1.NewtSiteSpec{Name: "new-name"},
		Status: pangolinv1alpha1.NewtSiteStatus{
			SiteID:             42,
			ObservedGeneration: 1,
			NewtSecretName:     "my-site-newt-credentials",
		},
	}

	scheme := newTestScheme()
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(site).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-site", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updateCalled {
		t.Error("expected UpdateSite to be called on generation change")
	}
}

// TestCleanup_DeletesSiteAndRemovesFinalizer verifies that cleanup calls DeleteSite
// and removes the finalizer.
func TestCleanup_DeletesSiteAndRemovesFinalizer(t *testing.T) {
	deleteCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteCalled = true
			pangolinResponse(w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	now := metav1.Now()
	site := &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "my-site",
			Namespace:         "default",
			Finalizers:        []string{NewtSiteFinalizer},
			DeletionTimestamp: &now,
		},
		Spec:   pangolinv1alpha1.NewtSiteSpec{},
		Status: pangolinv1alpha1.NewtSiteStatus{SiteID: 42},
	}

	scheme := newTestScheme()
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(site).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-site", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleteCalled {
		t.Error("expected DeleteSite to be called during cleanup")
	}

	// Finalizer should have been removed.
	var updated pangolinv1alpha1.NewtSite
	_ = cl.Get(context.Background(), client.ObjectKey{Name: "my-site", Namespace: "default"}, &updated)
	for _, f := range updated.Finalizers {
		if f == NewtSiteFinalizer {
			t.Error("expected finalizer to be removed after cleanup")
		}
	}
}

// TestCleanup_FailsAndRetriesOnDeleteError verifies that a Pangolin API failure
// during cleanup is returned as an error (fail-and-retry, not log-and-continue).
func TestCleanup_FailsAndRetriesOnDeleteError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site/42", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "server error"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	now := metav1.Now()
	site := &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "my-site",
			Namespace:         "default",
			Finalizers:        []string{NewtSiteFinalizer},
			DeletionTimestamp: &now,
		},
		Spec:   pangolinv1alpha1.NewtSiteSpec{},
		Status: pangolinv1alpha1.NewtSiteStatus{SiteID: 42},
	}

	scheme := newTestScheme()
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(site).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-site", Namespace: "default"},
	})
	if err == nil {
		t.Fatal("expected error when Pangolin delete fails")
	}
}
