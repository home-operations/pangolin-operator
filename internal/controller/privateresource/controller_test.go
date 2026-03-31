package privateresource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
	"github.com/home-operations/pangolin-operator/internal/pangolin"
	"github.com/home-operations/pangolin-operator/internal/testutil"
)

// TestReconcile_CreateSiteResource verifies that CreateSiteResource is called
// with non-nil roleIds/userIds and that the status is updated.
func TestReconcile_CreateSiteResource(t *testing.T) {
	createCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/site-resource", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		var req pangolin.CreateSiteResourceRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.RoleIds == nil {
			t.Error("roleIds must not be nil")
		}
		if req.UserIds == nil {
			t.Error("userIds must not be nil")
		}
		if req.ClientIds == nil {
			t.Error("clientIds must not be nil")
		}
		createCalled = true
		testutil.PangolinResponse(t, w, pangolin.CreateSiteResourceResponse{SiteResourceID: 55, NiceID: "sres-55"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PrivateResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-priv",
			Namespace:  "default",
			Finalizers: []string{PrivateResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PrivateResourceSpec{
			SiteRef:     "my-site",
			Name:        "my-priv",
			Mode:        "host",
			Destination: "10.0.0.5",
			TcpPorts:    "*",
			// RoleIds and UserIds intentionally nil to test nil-guard
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PrivateResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-priv", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !createCalled {
		t.Error("expected CreateSiteResource to be called")
	}
}

// TestUpdateSiteResource_CallsAPIWhenNameDiffers verifies updateSiteResource calls
// UpdateSiteResource when live name differs from spec.
func TestUpdateSiteResource_CallsAPIWhenNameDiffers(t *testing.T) {
	updateCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site-resource/55", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			updateCalled = true
			var req pangolin.UpdateSiteResourceRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Name != "new-name" {
				t.Errorf("expected name 'new-name', got %q", req.Name)
			}
			if req.RoleIds == nil || req.UserIds == nil || req.ClientIds == nil {
				t.Error("roleIds, userIds, and clientIds must not be nil on update")
			}
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PrivateResource{
		Spec: pangolinv1alpha1.PrivateResourceSpec{
			Name:        "new-name",
			Mode:        "host",
			Destination: "10.0.0.5",
			TcpPorts:    "*",
		},
		Status: pangolinv1alpha1.PrivateResourceStatus{SiteResourceID: 55},
	}

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{
		Client: testutil.NewClientBuilder(testutil.NewScheme()).
			WithStatusSubresource(&pangolinv1alpha1.PrivateResource{}).Build(),
		Scheme:         testutil.NewScheme(),
		PangolinClient: pc,
	}

	if err := r.updateSiteResource(context.Background(), res, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updateCalled {
		t.Error("expected UpdateSiteResource to be called on name change")
	}
}

// TestUpdateSiteResource_AlwaysCallsAPI verifies that UpdateSiteResource is always called,
// even when name/mode/destination are unchanged, so that port/icmp/alias changes are applied.
func TestUpdateSiteResource_AlwaysCallsAPI(t *testing.T) {
	updateCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site-resource/55", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			updateCalled = true
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PrivateResource{
		Spec: pangolinv1alpha1.PrivateResourceSpec{
			Name:        "same-name",
			Mode:        "host",
			Destination: "10.0.0.5",
			TcpPorts:    "8080",
		},
		Status: pangolinv1alpha1.PrivateResourceStatus{SiteResourceID: 55},
	}

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{
		Client:         testutil.NewClientBuilder(testutil.NewScheme()).Build(),
		Scheme:         testutil.NewScheme(),
		PangolinClient: pc,
	}

	if err := r.updateSiteResource(context.Background(), res, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updateCalled {
		t.Error("expected UpdateSiteResource to be called even when name/mode/destination are unchanged")
	}
}

// TestReconcile_Update_CallsUpdateOnGenerationChange verifies the full reconcile
// path triggers updateSiteResource when generation > observedGeneration.
func TestReconcile_Update_CallsUpdateOnGenerationChange(t *testing.T) {
	updateCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site-resource/55", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			updateCalled = true
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PrivateResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-priv",
			Namespace:  "default",
			Generation: 2,
			Finalizers: []string{PrivateResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PrivateResourceSpec{
			SiteRef:     "my-site",
			Name:        "new-name",
			Mode:        "host",
			Destination: "10.0.0.5",
		},
		Status: pangolinv1alpha1.PrivateResourceStatus{
			SiteResourceID:     55,
			ObservedGeneration: 1,
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PrivateResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-priv", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updateCalled {
		t.Error("expected UpdateSiteResource to be called on generation change")
	}
}

// TestReconcile_DriftDetection_ResetsSiteResourceIDOn404 verifies that when the Pangolin
// site resource no longer exists, SiteResourceID is reset and reconcile requeues for re-creation.
func TestReconcile_DriftDetection_ResetsSiteResourceIDOn404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/site/1/resource/nice/sres-55", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PrivateResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-priv",
			Namespace:  "default",
			Generation: 1,
			Finalizers: []string{PrivateResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PrivateResourceSpec{
			SiteRef:     "my-site",
			Name:        "my-priv",
			Mode:        "host",
			Destination: "10.0.0.5",
		},
		Status: pangolinv1alpha1.PrivateResourceStatus{
			SiteResourceID:     55,
			NiceID:             "sres-55",
			ObservedGeneration: 1, // steady state
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PrivateResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-priv", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter to be set after drift detection")
	}

	var updated pangolinv1alpha1.PrivateResource
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "my-priv", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get resource: %v", err)
	}
	if updated.Status.SiteResourceID != 0 {
		t.Errorf("expected SiteResourceID to be reset to 0, got %d", updated.Status.SiteResourceID)
	}
}

// TestReconcile_PeriodicResync verifies that a successful reconcile returns
// a RequeueAfter interval for periodic re-sync.
func TestReconcile_PeriodicResync(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/site/1/resource/nice/sres-55", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			testutil.PangolinResponse(t, w, pangolin.SiteResourceItem{
				SiteResourceID: 55, NiceID: "sres-55", Name: "my-priv", Mode: "host", Destination: "10.0.0.5",
			})
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PrivateResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-priv",
			Namespace:  "default",
			Generation: 1,
			Finalizers: []string{PrivateResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PrivateResourceSpec{
			SiteRef:     "my-site",
			Name:        "my-priv",
			Mode:        "host",
			Destination: "10.0.0.5",
		},
		Status: pangolinv1alpha1.PrivateResourceStatus{
			SiteResourceID:     55,
			NiceID:             "sres-55",
			ObservedGeneration: 1,
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PrivateResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-priv", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter to be set for periodic re-sync")
	}
}

// TestCleanup_DeletesSiteResourceAndRemovesFinalizer verifies full deletion path.
func TestCleanup_DeletesSiteResourceAndRemovesFinalizer(t *testing.T) {
	deleteCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site-resource/55", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteCalled = true
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	now := metav1.Now()
	res := &pangolinv1alpha1.PrivateResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "my-priv",
			Namespace:         "default",
			Finalizers:        []string{PrivateResourceFinalizer},
			DeletionTimestamp: &now,
		},
		Spec:   pangolinv1alpha1.PrivateResourceSpec{SiteRef: "my-site"},
		Status: pangolinv1alpha1.PrivateResourceStatus{SiteResourceID: 55},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PrivateResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-priv", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleteCalled {
		t.Error("expected DeleteSiteResource to be called during cleanup")
	}

	var updated pangolinv1alpha1.PrivateResource
	_ = cl.Get(context.Background(), client.ObjectKey{Name: "my-priv", Namespace: "default"}, &updated)
	for _, f := range updated.Finalizers {
		if f == PrivateResourceFinalizer {
			t.Error("expected finalizer to be removed after cleanup")
		}
	}
}

// TestCleanup_FailsAndRetriesOnDeleteError verifies that a Pangolin API failure
// during cleanup is returned as an error.
func TestCleanup_FailsAndRetriesOnDeleteError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site-resource/55", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "server error"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	now := metav1.Now()
	res := &pangolinv1alpha1.PrivateResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "my-priv",
			Namespace:         "default",
			Finalizers:        []string{PrivateResourceFinalizer},
			DeletionTimestamp: &now,
		},
		Spec:   pangolinv1alpha1.PrivateResourceSpec{SiteRef: "my-site"},
		Status: pangolinv1alpha1.PrivateResourceStatus{SiteResourceID: 55},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PrivateResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-priv", Namespace: "default"},
	})
	if err == nil {
		t.Fatal("expected error when Pangolin delete fails")
	}
}

// TestReconcile_RequeuesWhenSiteNotReady verifies that a PrivateResource requeues
// when its NewtSite is not yet in Ready phase.
func TestReconcile_RequeuesWhenSiteNotReady(t *testing.T) {
	res := &pangolinv1alpha1.PrivateResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-priv",
			Namespace:  "default",
			Finalizers: []string{PrivateResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PrivateResourceSpec{SiteRef: "my-site"},
	}
	site := &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{Name: "my-site", Namespace: "default"},
		Status:     pangolinv1alpha1.NewtSiteStatus{Phase: pangolinv1alpha1.NewtSitePhasePending},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, site).
		WithStatusSubresource(&pangolinv1alpha1.PrivateResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: "http://localhost", APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-priv", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter to be set when site is not ready")
	}
}
