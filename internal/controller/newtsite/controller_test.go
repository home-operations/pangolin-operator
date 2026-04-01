package newtsite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
	"github.com/home-operations/pangolin-operator/internal/pangolin"
	"github.com/home-operations/pangolin-operator/internal/testutil"
)

// TestUpdateSite_CallsAPIWhenNameDiffers verifies that updateSite calls UpdateSite
// when the live name in Pangolin differs from spec.name.
func TestUpdateSite_CallsAPIWhenNameDiffers(t *testing.T) {
	updateCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site/42", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			testutil.PangolinResponse(t, w, pangolin.GetSiteResponse{SiteID: 42, Name: "old-name"})
		case http.MethodPost:
			updateCalled = true
			var req pangolin.UpdateSiteRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Name != "new-name" {
				t.Errorf("expected name 'new-name', got %q", req.Name)
			}
			testutil.PangolinResponse(t, w, nil)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cl := testutil.NewClientBuilder(testutil.NewScheme()).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{
		Endpoint: srv.URL,
		APIKey:   "key",
		OrgID:    "org1",
	})
	r := &Reconciler{Client: cl, Scheme: testutil.NewScheme(), PangolinClient: pc, OperatorNamespace: "default"}

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
			testutil.PangolinResponse(t, w, pangolin.GetSiteResponse{SiteID: 42, Name: "same-name"})
		case http.MethodPost:
			updateCalled = true
			testutil.PangolinResponse(t, w, nil)
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

	r := &Reconciler{Client: testutil.NewClientBuilder(testutil.NewScheme()).Build(), Scheme: testutil.NewScheme(), PangolinClient: pc, OperatorNamespace: "default"}
	if err := r.updateSite(context.Background(), site); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updateCalled {
		t.Error("expected UpdateSite NOT to be called when name is unchanged")
	}
}

func TestFSindOrCreate_AdoptsExistingSite(t *testing.T) {
	createCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/sites", func(w http.ResponseWriter, r *http.Request) {
		testutil.PangolinResponse(t, w, struct {
			Sites []pangolin.SiteItem `json:"sites"`
		}{
			Sites: []pangolin.SiteItem{
				{SiteID: 99, NiceID: "nice-99", Name: "my-site", Type: "newt"},
			},
		})
	})
	mux.HandleFunc("/v1/org/org1/site", func(w http.ResponseWriter, r *http.Request) {
		createCalled = true
		testutil.PangolinResponse(t, w, pangolin.CreateSiteResponse{SiteID: 100, NiceID: "nice-100"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	site := &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{Name: "my-site", Namespace: "default"},
		Spec:       pangolinv1alpha1.NewtSiteSpec{Name: "my-site"},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(site).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc, OperatorNamespace: "default"}

	if err := r.findOrCreate(context.Background(), site); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createCalled {
		t.Error("expected CreateSite NOT to be called when an existing site is found")
	}

	var updated pangolinv1alpha1.NewtSite
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(site), &updated); err != nil {
		t.Fatalf("could not get updated site: %v", err)
	}
	if updated.Status.SiteID != 99 {
		t.Errorf("expected SiteID 99, got %d", updated.Status.SiteID)
	}
	if updated.Status.NiceID != "nice-99" {
		t.Errorf("expected NiceID 'nice-99', got %q", updated.Status.NiceID)
	}
}

func TestFindOrCreate_CreatesWhenNotFound(t *testing.T) {
	createCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/sites", func(w http.ResponseWriter, r *http.Request) {
		testutil.PangolinResponse(t, w, struct {
			Sites []pangolin.SiteItem `json:"sites"`
		}{})
	})
	mux.HandleFunc("/v1/org/org1/pick-site-defaults", func(w http.ResponseWriter, r *http.Request) {
		testutil.PangolinResponse(t, w, pangolin.PickSiteDefaultsResponse{
			NewtID: "nid", NewtSecret: "nsec", ClientAddress: "100.90.0.1",
		})
	})
	mux.HandleFunc("/v1/org/org1/site", func(w http.ResponseWriter, r *http.Request) {
		createCalled = true
		testutil.PangolinResponse(t, w, pangolin.CreateSiteResponse{SiteID: 100, NiceID: "nice-100"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	site := &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{Name: "my-site", Namespace: "default"},
		Spec:       pangolinv1alpha1.NewtSiteSpec{Name: "my-site"},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(site).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc, OperatorNamespace: "default"}

	if err := r.findOrCreate(context.Background(), site); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !createCalled {
		t.Error("expected CreateSite to be called when no existing site is found")
	}
}

func TestReconcile_CreateSite_PassesAddress(t *testing.T) {
	const clientAddress = "100.90.128.1"
	createSiteCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/sites", func(w http.ResponseWriter, r *http.Request) {
		testutil.PangolinResponse(t, w, struct {
			Sites []pangolin.SiteItem `json:"sites"`
		}{})
	})
	mux.HandleFunc("/v1/org/org1/pick-site-defaults", func(w http.ResponseWriter, r *http.Request) {
		testutil.PangolinResponse(t, w, pangolin.PickSiteDefaultsResponse{
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
		testutil.PangolinResponse(t, w, pangolin.CreateSiteResponse{SiteID: 10, NiceID: "nice-10"})
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

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(site).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc, OperatorNamespace: "default"}
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
			testutil.PangolinResponse(t, w, pangolin.GetSiteResponse{SiteID: 42, Name: "old-name"})
		case http.MethodPost:
			updateCalled = true
			testutil.PangolinResponse(t, w, nil)
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
		Status: pangolinv1alpha1.NewtSiteStatus{ //nolint:gosec // test fixture, not real credentials
			SiteID:             42,
			ObservedGeneration: 1,
			NewtSecretName:     "my-site-newt-credentials",
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(site).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc, OperatorNamespace: "default"}
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
			testutil.PangolinResponse(t, w, nil)
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

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(site).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc, OperatorNamespace: "default"}
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

// TestReconcile_DriftDetection_ResetsSiteIDOn404 verifies that when the Pangolin
// site no longer exists, SiteID is reset and reconcile requeues for re-creation.
func TestReconcile_DriftDetection_ResetsSiteIDOn404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	site := &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-site",
			Namespace:  "default",
			Generation: 1,
			Finalizers: []string{NewtSiteFinalizer},
		},
		Spec: pangolinv1alpha1.NewtSiteSpec{Name: "my-site"},
		Status: pangolinv1alpha1.NewtSiteStatus{ //nolint:gosec // test fixture, not real credentials
			SiteID:             42,
			ObservedGeneration: 1, // generation matches — steady state
			NewtSecretName:     "my-site-newt-credentials",
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(site).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc, OperatorNamespace: "default"}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-site", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter to be set after drift detection")
	}

	var updated pangolinv1alpha1.NewtSite
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "my-site", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get site: %v", err)
	}
	if updated.Status.SiteID != 0 {
		t.Errorf("expected SiteID to be reset to 0, got %d", updated.Status.SiteID)
	}
}

// TestReconcile_UpdateSite_Handles404 verifies that when updateSite encounters a 404,
// the SiteID is reset and reconcile requeues for re-creation.
func TestReconcile_UpdateSite_Handles404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site/42", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
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
		Status: pangolinv1alpha1.NewtSiteStatus{ //nolint:gosec // test fixture, not real credentials
			SiteID:             42,
			ObservedGeneration: 1, // generation mismatch triggers update path
			NewtSecretName:     "my-site-newt-credentials",
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(site).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc, OperatorNamespace: "default"}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-site", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter to be set after 404 on update")
	}

	var updated pangolinv1alpha1.NewtSite
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "my-site", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get site: %v", err)
	}
	if updated.Status.SiteID != 0 {
		t.Errorf("expected SiteID to be reset to 0, got %d", updated.Status.SiteID)
	}
}

// TestReconcile_PeriodicResync verifies that a successful reconcile always returns
// a RequeueAfter interval for periodic re-sync.
func TestReconcile_PeriodicResync(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			testutil.PangolinResponse(t, w, pangolin.GetSiteResponse{SiteID: 42, Name: "my-site"})
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	site := &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-site",
			Namespace:  "default",
			Generation: 1,
			Finalizers: []string{NewtSiteFinalizer},
		},
		Spec: pangolinv1alpha1.NewtSiteSpec{Name: "my-site"},
		Status: pangolinv1alpha1.NewtSiteStatus{ //nolint:gosec // test fixture, not real credentials
			SiteID:             42,
			ObservedGeneration: 1,
			NewtSecretName:     "my-site-newt-credentials",
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(site).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc, OperatorNamespace: "default"}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-site", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter to be set for periodic re-sync")
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

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(site).
		WithStatusSubresource(&pangolinv1alpha1.NewtSite{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc, OperatorNamespace: "default"}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-site", Namespace: "default"},
	})
	if err == nil {
		t.Fatal("expected error when Pangolin delete fails")
	}
}
