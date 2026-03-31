package publicresource

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

// TestReconcile_CreateResource creates a new PublicResource and verifies that
// CreateResource and CreateTarget are called, and targetIds + targetsHash are stored.
func TestReconcile_CreateResource(t *testing.T) {
	createResourceCalled := false
	createTargetCalled := false
	applySettingsCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/resources", func(w http.ResponseWriter, r *http.Request) {
		testutil.PangolinResponse(t, w, map[string]any{"resources": []any{}})
	})
	mux.HandleFunc("/v1/org/org1/domains", func(w http.ResponseWriter, r *http.Request) {
		testutil.PangolinResponse(t, w, map[string]any{
			"domains": []map[string]any{
				{"domainId": "dom-1", "baseDomain": "example.com"},
			},
		})
	})
	mux.HandleFunc("/v1/org/org1/resource", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		createResourceCalled = true
		testutil.PangolinResponse(t, w, pangolin.CreateResourceResponse{ResourceID: 7, NiceID: "res-7", FullDomain: "app.example.com"})
	})
	mux.HandleFunc("/v1/resource/7/target", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		createTargetCalled = true
		testutil.PangolinResponse(t, w, pangolin.CreateTargetResponse{TargetID: 99})
	})
	mux.HandleFunc("/v1/resource/7", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			testutil.PangolinResponse(t, w, pangolin.ResourceItem{ResourceID: 7, Name: "my-res"})
		case http.MethodPost:
			applySettingsCalled = true
			var req pangolin.UpdateResourceRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Sso == nil || *req.Sso {
				t.Error("expected sso=false on create")
			}
			if req.BlockAccess == nil || *req.BlockAccess {
				t.Error("expected blockAccess=false on create")
			}
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-res",
			Namespace:  "default",
			Finalizers: []string{PublicResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			SiteRef:    "my-site",
			Name:       "my-res",
			FullDomain: "app.example.com",
			Protocol:   "http",
			Targets:    []pangolinv1alpha1.PublicTargetSpec{{Hostname: "backend.default.svc.cluster.local", Port: 8080, Method: "http"}},
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-res", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !createResourceCalled {
		t.Error("expected CreateResource to be called")
	}
	if !applySettingsCalled {
		t.Error("expected UpdateResource (HTTP settings) to be called after create")
	}
	if !createTargetCalled {
		t.Error("expected CreateTarget to be called")
	}
}

// TestUpdateResource_NameChange verifies that UpdateResource is called when spec.name changes.
func TestUpdateResource_NameChange(t *testing.T) {
	updateCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/resource/7", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			testutil.PangolinResponse(t, w, pangolin.ResourceItem{ResourceID: 7, Name: "old-name"})
		case http.MethodPost:
			updateCalled = true
			var req pangolin.UpdateResourceRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Name != "new-name" {
				t.Errorf("expected name 'new-name', got %q", req.Name)
			}
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	targets := []pangolinv1alpha1.PublicTargetSpec{{Hostname: "backend.default.svc.cluster.local", Port: 80, Method: "http"}}
	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{Name: "my-res", Namespace: "default"},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			Name:    "new-name",
			Targets: targets,
		},
		Status: pangolinv1alpha1.PublicResourceStatus{
			ResourceID:  7,
			TargetsHash: hashTargets(targets),
		},
	}

	scheme := testutil.NewScheme()
	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{
		Client: testutil.NewClientBuilder(scheme).
			WithObjects(res).
			WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).Build(),
		Scheme:         scheme,
		PangolinClient: pc,
	}

	if err := r.updateResource(context.Background(), res, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updateCalled {
		t.Error("expected UpdateResource to be called on name change")
	}
}

// TestUpdateResource_TargetsChanged verifies that when the targets hash differs,
// existing targets are deleted and new ones are created.
func TestUpdateResource_TargetsChanged(t *testing.T) {
	deleteTargetCalled := false
	createTargetCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/resource/7", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			testutil.PangolinResponse(t, w, pangolin.ResourceItem{ResourceID: 7, Name: "my-res"})
		case http.MethodPost:
			testutil.PangolinResponse(t, w, nil)
		}
	})
	mux.HandleFunc("/v1/target/99", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteTargetCalled = true
			testutil.PangolinResponse(t, w, nil)
		}
	})
	mux.HandleFunc("/v1/resource/7/target", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			createTargetCalled = true
			testutil.PangolinResponse(t, w, pangolin.CreateTargetResponse{TargetID: 100})
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	oldTargets := []pangolinv1alpha1.PublicTargetSpec{{Hostname: "old.svc", Port: 80}}
	newTargets := []pangolinv1alpha1.PublicTargetSpec{{Hostname: "new.svc", Port: 9090}}

	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{Name: "my-res", Namespace: "default"},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			Name:    "my-res",
			Targets: newTargets,
		},
		Status: pangolinv1alpha1.PublicResourceStatus{
			ResourceID:  7,
			TargetIDs:   []int{99},
			TargetsHash: hashTargets(oldTargets), // stale hash
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}

	if err := r.updateResource(context.Background(), res, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleteTargetCalled {
		t.Error("expected old target to be deleted")
	}
	if !createTargetCalled {
		t.Error("expected new target to be created")
	}
}

// TestUpdateResource_TargetsUnchanged verifies that when the targets hash matches,
// no delete/create calls are made.
func TestUpdateResource_TargetsUnchanged(t *testing.T) {
	deleteTargetCalled := false
	createTargetCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/resource/7", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			testutil.PangolinResponse(t, w, pangolin.ResourceItem{ResourceID: 7, Name: "my-res"})
		case http.MethodPost:
			testutil.PangolinResponse(t, w, nil)
		}
	})
	mux.HandleFunc("/v1/target/", func(w http.ResponseWriter, r *http.Request) {
		deleteTargetCalled = true
	})
	mux.HandleFunc("/v1/resource/7/target", func(w http.ResponseWriter, r *http.Request) {
		createTargetCalled = true
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	targets := []pangolinv1alpha1.PublicTargetSpec{{Hostname: "backend.default.svc.cluster.local", Port: 80}}
	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{Name: "my-res", Namespace: "default"},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			Name:    "my-res",
			Targets: targets,
		},
		Status: pangolinv1alpha1.PublicResourceStatus{
			ResourceID:  7,
			TargetIDs:   []int{99},
			TargetsHash: hashTargets(targets), // hash matches
		},
	}

	scheme := testutil.NewScheme()
	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{
		Client: testutil.NewClientBuilder(scheme).
			WithObjects(res).
			WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).Build(),
		Scheme:         scheme,
		PangolinClient: pc,
	}

	if err := r.updateResource(context.Background(), res, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleteTargetCalled {
		t.Error("expected no target deletion when hash is unchanged")
	}
	if createTargetCalled {
		t.Error("expected no target creation when hash is unchanged")
	}
}

// TestCleanup_DeletesResourceAndRemovesFinalizer verifies full deletion path.
func TestCleanup_DeletesResourceAndRemovesFinalizer(t *testing.T) {
	deleteCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/resource/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteCalled = true
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	now := metav1.Now()
	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "my-res",
			Namespace:         "default",
			Finalizers:        []string{PublicResourceFinalizer},
			DeletionTimestamp: &now,
		},
		Spec:   pangolinv1alpha1.PublicResourceSpec{SiteRef: "my-site"},
		Status: pangolinv1alpha1.PublicResourceStatus{ResourceID: 7},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-res", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleteCalled {
		t.Error("expected DeleteResource to be called during cleanup")
	}

	var updated pangolinv1alpha1.PublicResource
	_ = cl.Get(context.Background(), client.ObjectKey{Name: "my-res", Namespace: "default"}, &updated)
	for _, f := range updated.Finalizers {
		if f == PublicResourceFinalizer {
			t.Error("expected finalizer to be removed after cleanup")
		}
	}
}

// TestCleanup_FailsAndRetriesOnDeleteError verifies that a Pangolin API failure
// during cleanup is returned as an error.
func TestCleanup_FailsAndRetriesOnDeleteError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/resource/7", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "server error"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	now := metav1.Now()
	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "my-res",
			Namespace:         "default",
			Finalizers:        []string{PublicResourceFinalizer},
			DeletionTimestamp: &now,
		},
		Spec:   pangolinv1alpha1.PublicResourceSpec{SiteRef: "my-site"},
		Status: pangolinv1alpha1.PublicResourceStatus{ResourceID: 7},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-res", Namespace: "default"},
	})
	if err == nil {
		t.Fatal("expected error when Pangolin delete fails")
	}
}

// TestReconcile_RequeuesWhenSiteNotReady verifies that a PublicResource requeues
// when its NewtSite is not yet in Ready phase.
func TestReconcile_RequeuesWhenSiteNotReady(t *testing.T) {
	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-res",
			Namespace:  "default",
			Finalizers: []string{PublicResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PublicResourceSpec{SiteRef: "my-site"},
	}
	site := &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{Name: "my-site", Namespace: "default"},
		Status:     pangolinv1alpha1.NewtSiteStatus{Phase: pangolinv1alpha1.NewtSitePhasePending},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, site).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: "http://localhost", APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-res", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter to be set when site is not ready")
	}
}

func TestReconcile_409Conflict_RequeuesWithCondition(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/resources", func(w http.ResponseWriter, r *http.Request) {
		testutil.PangolinResponse(t, w, map[string]any{"resources": []any{}})
	})
	mux.HandleFunc("/v1/org/org1/domains", func(w http.ResponseWriter, r *http.Request) {
		testutil.PangolinResponse(t, w, pangolin.ListDomainsResponse{
			Domains: []pangolin.Domain{
				{DomainID: "d1", BaseDomain: "example.com"},
			},
		})
	})
	mux.HandleFunc("/v1/org/org1/resource", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-res",
			Namespace:  "default",
			Finalizers: []string{PublicResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			SiteRef:    "my-site",
			Name:       "My App",
			Protocol:   "http",
			FullDomain: "app.example.com",
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-res", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("expected no error on 409 conflict, got: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter to be set on conflict")
	}
}

// TestReconcile_DriftDetection_RecreatesWhenNotInList verifies that when the Pangolin
// resource no longer exists in the list, a new resource is created via ensureExists.
func TestReconcile_DriftDetection_RecreatesWhenNotInList(t *testing.T) {
	createResourceCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/resources", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// Return an empty list — resource 7 is not present.
			testutil.PangolinResponse(t, w, map[string]any{"resources": []any{}})
		}
	})
	mux.HandleFunc("/v1/org/org1/resource", func(w http.ResponseWriter, r *http.Request) {
		createResourceCalled = true
		testutil.PangolinResponse(t, w, pangolin.CreateResourceResponse{ResourceID: 42, NiceID: "res-42"})
	})
	mux.HandleFunc("/v1/resource/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			testutil.PangolinResponse(t, w, nil)
		}
	})
	mux.HandleFunc("/v1/resource/42/target", func(w http.ResponseWriter, r *http.Request) {
		testutil.PangolinResponse(t, w, pangolin.CreateTargetResponse{TargetID: 100})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-res",
			Namespace:  "default",
			Generation: 1,
			Finalizers: []string{PublicResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			SiteRef:  "my-site",
			Name:     "my-res",
			Protocol: "tcp",
			Targets:  []pangolinv1alpha1.PublicTargetSpec{{Hostname: "backend.svc", Port: 80}},
		},
		Status: pangolinv1alpha1.PublicResourceStatus{
			ResourceID:         7,
			ObservedGeneration: 1, // steady state
			TargetIDs:          []int{99},
			TargetsHash:        hashTargets([]pangolinv1alpha1.PublicTargetSpec{{Hostname: "backend.svc", Port: 80}}),
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-res", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !createResourceCalled {
		t.Error("expected CreateResource to be called when resource is missing from list")
	}

	var updated pangolinv1alpha1.PublicResource
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "my-res", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get resource: %v", err)
	}
	if updated.Status.ResourceID != 42 {
		t.Errorf("expected ResourceID to be 42 after re-creation, got %d", updated.Status.ResourceID)
	}
}

// TestReconcile_PeriodicResync verifies that a successful reconcile returns
// a RequeueAfter interval for periodic re-sync.
func TestReconcile_PeriodicResync(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/resources", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			testutil.PangolinResponse(t, w, map[string]any{
				"resources": []pangolin.ResourceItem{{ResourceID: 7, Name: "my-res"}},
			})
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	targets := []pangolinv1alpha1.PublicTargetSpec{{Hostname: "backend.svc", Port: 80}}
	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-res",
			Namespace:  "default",
			Generation: 1,
			Finalizers: []string{PublicResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			SiteRef:  "my-site",
			Name:     "my-res",
			Protocol: "tcp",
			Targets:  targets,
		},
		Status: pangolinv1alpha1.PublicResourceStatus{
			ResourceID:         7,
			ObservedGeneration: 1,
			TargetIDs:          []int{99},
			TargetsHash:        hashTargets(targets),
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-res", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter to be set for periodic re-sync")
	}
}

// TestReconcile_CreateResource_TargetFailure verifies that when CreateTarget fails
// after CreateResource succeeds, the resourceID is still persisted in status so that
// re-reconciliation doesn't create a duplicate resource.
func TestReconcile_CreateResource_TargetFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/resources", func(w http.ResponseWriter, r *http.Request) {
		testutil.PangolinResponse(t, w, map[string]any{"resources": []any{}})
	})
	mux.HandleFunc("/v1/org/org1/domains", func(w http.ResponseWriter, _ *http.Request) {
		testutil.PangolinResponse(t, w, map[string]any{
			"domains": []map[string]any{
				{"domainId": "dom-1", "baseDomain": "example.com"},
			},
		})
	})
	mux.HandleFunc("/v1/org/org1/resource", func(w http.ResponseWriter, _ *http.Request) {
		testutil.PangolinResponse(t, w, pangolin.CreateResourceResponse{ResourceID: 7, NiceID: "res-7", FullDomain: "app.example.com"})
	})
	mux.HandleFunc("/v1/resource/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			testutil.PangolinResponse(t, w, nil)
		}
	})
	mux.HandleFunc("/v1/resource/7/target", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "target backend error"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-res",
			Namespace:  "default",
			Finalizers: []string{PublicResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			SiteRef:    "my-site",
			Name:       "my-res",
			FullDomain: "app.example.com",
			Protocol:   "http",
			Targets:    []pangolinv1alpha1.PublicTargetSpec{{Hostname: "backend.svc", Port: 8080, Method: "http"}},
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "my-res", Namespace: "default"},
	})
	if err == nil {
		t.Fatal("expected error when CreateTarget fails")
	}

	// ResourceID should still be persisted so the next reconcile doesn't re-create the resource.
	var updated pangolinv1alpha1.PublicResource
	if getErr := cl.Get(context.Background(), client.ObjectKey{Name: "my-res", Namespace: "default"}, &updated); getErr != nil {
		t.Fatalf("failed to get resource: %v", getErr)
	}
	if updated.Status.ResourceID == 0 {
		t.Error("expected ResourceID to be persisted in status after partial failure")
	}
}
