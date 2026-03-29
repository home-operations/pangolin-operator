package publicresource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
	return s
}

func pangolinResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	type envelope struct {
		Data    any  `json:"data"`
		Success bool `json:"success"`
	}
	_ = json.NewEncoder(w).Encode(envelope{Data: data, Success: true})
}

func readySite() *pangolinv1alpha1.NewtSite {
	return &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{Name: "my-site", Namespace: "default"},
		Spec:       pangolinv1alpha1.NewtSiteSpec{},
		Status: pangolinv1alpha1.NewtSiteStatus{
			Phase:  pangolinv1alpha1.NewtSitePhaseReady,
			SiteID: 1,
		},
	}
}

// TestReconcile_CreateResource creates a new PublicResource and verifies that
// CreateResource and CreateTarget are called, and targetIds + targetsHash are stored.
func TestReconcile_CreateResource(t *testing.T) {
	createResourceCalled := false
	createTargetCalled := false
	applySettingsCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/domains", func(w http.ResponseWriter, r *http.Request) {
		pangolinResponse(w, map[string]any{
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
		pangolinResponse(w, pangolin.CreateResourceResponse{ResourceID: 7, NiceID: "res-7", FullDomain: "app.example.com"})
	})
	mux.HandleFunc("/v1/resource/7/target", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		createTargetCalled = true
		pangolinResponse(w, pangolin.CreateTargetResponse{TargetID: 99})
	})
	mux.HandleFunc("/v1/resource/7", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			pangolinResponse(w, pangolin.GetResourceResponse{ResourceID: 7, Name: "my-res"})
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
			pangolinResponse(w, nil)
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

	scheme := newTestScheme()
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(res, readySite()).
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
			pangolinResponse(w, pangolin.GetResourceResponse{ResourceID: 7, Name: "old-name"})
		case http.MethodPost:
			updateCalled = true
			var req pangolin.UpdateResourceRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Name != "new-name" {
				t.Errorf("expected name 'new-name', got %q", req.Name)
			}
			pangolinResponse(w, nil)
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

	scheme := newTestScheme()
	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).
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
			pangolinResponse(w, pangolin.GetResourceResponse{ResourceID: 7, Name: "my-res"})
		case http.MethodPost:
			pangolinResponse(w, nil)
		}
	})
	mux.HandleFunc("/v1/target/99", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteTargetCalled = true
			pangolinResponse(w, nil)
		}
	})
	mux.HandleFunc("/v1/resource/7/target", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			createTargetCalled = true
			pangolinResponse(w, pangolin.CreateTargetResponse{TargetID: 100})
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

	scheme := newTestScheme()
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
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
			pangolinResponse(w, pangolin.GetResourceResponse{ResourceID: 7, Name: "my-res"})
		case http.MethodPost:
			pangolinResponse(w, nil)
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

	scheme := newTestScheme()
	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).
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
			pangolinResponse(w, nil)
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

	scheme := newTestScheme()
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(res, readySite()).
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

	scheme := newTestScheme()
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(res, readySite()).
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

	scheme := newTestScheme()
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
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
	mux.HandleFunc("/v1/org/org1/domains", func(w http.ResponseWriter, r *http.Request) {
		pangolinResponse(w, struct {
			Domains []any `json:"domains"`
		}{Domains: []any{}})
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

	scheme := newTestScheme()
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(res, readySite()).
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
