package privateresource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
	"github.com/home-operations/pangolin-operator/internal/pangolin"
	"github.com/home-operations/pangolin-operator/internal/testutil"
)

// TestLifecycle_CreateUpdateDelete drives a PrivateResource through its full
// lifecycle: create → spec change → delete.
func TestLifecycle_CreateUpdateDelete(t *testing.T) {
	var (
		createCalls atomic.Int32
		updateCalls atomic.Int32
		deleteCalls atomic.Int32
	)

	mux := http.NewServeMux()

	mux.HandleFunc("/v1/org/org1/site-resources", func(w http.ResponseWriter, _ *http.Request) {
		if createCalls.Load() == 0 {
			testutil.PangolinResponse(t, w, map[string]any{"siteResources": []any{}})
			return
		}
		testutil.PangolinResponse(t, w, map[string]any{
			"siteResources": []pangolin.SiteResourceItem{
				{SiteResourceID: 20, NiceID: "sr-20", Name: "my-vpn", Mode: "host", Destination: "10.0.0.5", SiteID: 1},
			},
		})
	})
	mux.HandleFunc("/v1/org/org1/site-resource", func(w http.ResponseWriter, _ *http.Request) {
		createCalls.Add(1)
		testutil.PangolinResponse(t, w, pangolin.CreateSiteResourceResponse{SiteResourceID: 20, NiceID: "sr-20"})
	})
	mux.HandleFunc("/v1/site-resource/20", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			updateCalls.Add(1)
			testutil.PangolinResponse(t, w, nil)
		case http.MethodDelete:
			deleteCalls.Add(1)
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PrivateResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-vpn",
			Namespace:  "default",
			Generation: 1,
			Finalizers: []string{PrivateResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PrivateResourceSpec{
			SiteRef:     "my-site",
			Name:        "my-vpn",
			Mode:        "host",
			Destination: "10.0.0.5",
			TcpPorts:    "*",
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PrivateResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	nn := types.NamespacedName{Name: "my-vpn", Namespace: "default"}

	// --- Phase 1: Create ---
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("create reconcile: %v", err)
	}
	if createCalls.Load() != 1 {
		t.Errorf("expected 1 create call, got %d", createCalls.Load())
	}

	var created pangolinv1alpha1.PrivateResource
	if err := cl.Get(context.Background(), nn, &created); err != nil {
		t.Fatalf("get after create: %v", err)
	}
	if created.Status.SiteResourceID != 20 {
		t.Fatalf("expected SiteResourceID=20, got %d", created.Status.SiteResourceID)
	}
	if created.Status.Phase != pangolinv1alpha1.PrivateResourcePhaseReady {
		t.Errorf("expected phase Ready, got %s", created.Status.Phase)
	}

	// --- Phase 2: Update (bump generation) ---
	patch := client.MergeFrom(created.DeepCopy())
	created.Spec.TcpPorts = "8080-8090"
	created.Generation = 2
	if err := cl.Patch(context.Background(), &created, patch); err != nil {
		t.Fatalf("patch spec: %v", err)
	}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("update reconcile: %v", err)
	}
	if updateCalls.Load() == 0 {
		t.Error("expected UpdateSiteResource to be called on generation change")
	}

	// --- Phase 3: Delete ---
	var toDelete pangolinv1alpha1.PrivateResource
	if err := cl.Get(context.Background(), nn, &toDelete); err != nil {
		t.Fatalf("get before delete: %v", err)
	}
	now := metav1.Now()
	toDelete.DeletionTimestamp = &now

	scheme2 := testutil.NewScheme()
	cl2 := testutil.NewClientBuilder(scheme2).
		WithObjects(&toDelete, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PrivateResource{}).
		Build()
	r2 := &Reconciler{Client: cl2, Scheme: scheme2, PangolinClient: pc}

	if _, err := r2.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("delete reconcile: %v", err)
	}
	if deleteCalls.Load() != 1 {
		t.Errorf("expected 1 delete call, got %d", deleteCalls.Load())
	}
}

// TestLifecycle_AdoptExistingSiteResource verifies that when a matching Pangolin
// site resource already exists, the operator adopts it instead of creating a new one.
func TestLifecycle_AdoptExistingSiteResource(t *testing.T) {
	createCalled := false
	updateCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/site-resources", func(w http.ResponseWriter, _ *http.Request) {
		testutil.PangolinResponse(t, w, map[string]any{
			"siteResources": []pangolin.SiteResourceItem{
				{SiteResourceID: 77, NiceID: "sr-77", Name: "my-vpn", Mode: "host", Destination: "10.0.0.5", SiteID: 1},
			},
		})
	})
	mux.HandleFunc("/v1/org/org1/site-resource", func(w http.ResponseWriter, _ *http.Request) {
		createCalled = true
		testutil.PangolinResponse(t, w, pangolin.CreateSiteResourceResponse{SiteResourceID: 999})
	})
	mux.HandleFunc("/v1/site-resource/77", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			updateCalled = true
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PrivateResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-vpn",
			Namespace:  "default",
			Finalizers: []string{PrivateResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PrivateResourceSpec{
			SiteRef:     "my-site",
			Name:        "my-vpn",
			Mode:        "host",
			Destination: "10.0.0.5",
			TcpPorts:    "*",
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PrivateResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	nn := types.NamespacedName{Name: "my-vpn", Namespace: "default"}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if createCalled {
		t.Error("expected no CreateSiteResource call when adopting")
	}
	if !updateCalled {
		t.Error("expected UpdateSiteResource to be called after adoption to sync spec")
	}

	var updated pangolinv1alpha1.PrivateResource
	if err := cl.Get(context.Background(), nn, &updated); err != nil {
		t.Fatalf("get: %v", err)
	}
	if updated.Status.SiteResourceID != 77 {
		t.Errorf("expected adopted SiteResourceID=77, got %d", updated.Status.SiteResourceID)
	}
}

// TestLifecycle_CreateThenSteadyState verifies that a second reconcile after
// a successful create is a no-op: no extra create or update calls.
func TestLifecycle_CreateThenSteadyState(t *testing.T) {
	var (
		createCalls atomic.Int32
		updateCalls atomic.Int32
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/site-resources", func(w http.ResponseWriter, _ *http.Request) {
		if createCalls.Load() == 0 {
			testutil.PangolinResponse(t, w, map[string]any{"siteResources": []any{}})
			return
		}
		testutil.PangolinResponse(t, w, map[string]any{
			"siteResources": []pangolin.SiteResourceItem{
				{SiteResourceID: 20, NiceID: "sr-20", Name: "my-vpn", Mode: "host", Destination: "10.0.0.5", SiteID: 1},
			},
		})
	})
	mux.HandleFunc("/v1/org/org1/site-resource", func(w http.ResponseWriter, _ *http.Request) {
		createCalls.Add(1)
		testutil.PangolinResponse(t, w, pangolin.CreateSiteResourceResponse{SiteResourceID: 20, NiceID: "sr-20"})
	})
	mux.HandleFunc("/v1/site-resource/20", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			updateCalls.Add(1)
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PrivateResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-vpn",
			Namespace:  "default",
			Generation: 1,
			Finalizers: []string{PrivateResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PrivateResourceSpec{
			SiteRef:     "my-site",
			Name:        "my-vpn",
			Mode:        "host",
			Destination: "10.0.0.5",
			TcpPorts:    "*",
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PrivateResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	nn := types.NamespacedName{Name: "my-vpn", Namespace: "default"}

	// First reconcile: create.
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("create reconcile: %v", err)
	}
	if createCalls.Load() != 1 {
		t.Fatalf("expected 1 create, got %d", createCalls.Load())
	}

	createsAfter := createCalls.Load()
	updatesAfter := updateCalls.Load()

	// Second reconcile (steady-state): must be a no-op.
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("steady-state reconcile: %v", err)
	}
	if createCalls.Load() != createsAfter {
		t.Error("steady-state reconcile must not create a new resource")
	}
	if updateCalls.Load() != updatesAfter {
		t.Error("steady-state reconcile must not call UpdateSiteResource")
	}
}

// TestLifecycle_AdoptThenSteadyState verifies that a second reconcile after
// adoption + update is a no-op.
func TestLifecycle_AdoptThenSteadyState(t *testing.T) {
	var updateCalls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/site-resources", func(w http.ResponseWriter, _ *http.Request) {
		testutil.PangolinResponse(t, w, map[string]any{
			"siteResources": []pangolin.SiteResourceItem{
				{SiteResourceID: 77, NiceID: "sr-77", Name: "my-vpn", Mode: "host", Destination: "10.0.0.5", SiteID: 1},
			},
		})
	})
	mux.HandleFunc("/v1/site-resource/77", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			updateCalls.Add(1)
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PrivateResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-vpn",
			Namespace:  "default",
			Finalizers: []string{PrivateResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PrivateResourceSpec{
			SiteRef:     "my-site",
			Name:        "my-vpn",
			Mode:        "host",
			Destination: "10.0.0.5",
			TcpPorts:    "*",
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PrivateResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	nn := types.NamespacedName{Name: "my-vpn", Namespace: "default"}

	// First reconcile: adopt + update.
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("adopt reconcile: %v", err)
	}
	if updateCalls.Load() != 1 {
		t.Fatalf("expected 1 update after adoption, got %d", updateCalls.Load())
	}

	updatesAfter := updateCalls.Load()

	// Second reconcile (steady-state): must be a no-op.
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("steady-state reconcile: %v", err)
	}
	if updateCalls.Load() != updatesAfter {
		t.Error("steady-state reconcile after adopt must not call UpdateSiteResource again")
	}
}

// TestLifecycle_UpdateNotFound_ResetsAndRecreates verifies that a 404 during
// update resets the SiteResourceID and the next reconcile re-creates.
func TestLifecycle_UpdateNotFound_ResetsAndRecreates(t *testing.T) {
	var listCalls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/site-resources", func(w http.ResponseWriter, _ *http.Request) {
		n := listCalls.Add(1)
		if n == 1 {
			// First call: resource still in list → ensureExists passes.
			testutil.PangolinResponse(t, w, map[string]any{
				"siteResources": []pangolin.SiteResourceItem{
					{SiteResourceID: 55, Name: "my-vpn", Mode: "host", Destination: "10.0.0.5", SiteID: 1},
				},
			})
			return
		}
		// Subsequent calls: resource gone → triggers re-create.
		testutil.PangolinResponse(t, w, map[string]any{"siteResources": []any{}})
	})
	mux.HandleFunc("/v1/site-resource/55", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/v1/org/org1/site-resource", func(w http.ResponseWriter, _ *http.Request) {
		testutil.PangolinResponse(t, w, pangolin.CreateSiteResourceResponse{SiteResourceID: 88, NiceID: "sr-88"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PrivateResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-vpn",
			Namespace:  "default",
			Generation: 2,
			Finalizers: []string{PrivateResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PrivateResourceSpec{
			SiteRef:     "my-site",
			Name:        "my-vpn",
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
	nn := types.NamespacedName{Name: "my-vpn", Namespace: "default"}

	// First reconcile: ensureExists passes, updateSiteResource returns 404 → reset + requeue.
	result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn})
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if !result.Requeue {
		t.Error("expected Requeue after 404 reset")
	}

	var reset pangolinv1alpha1.PrivateResource
	if err := cl.Get(context.Background(), nn, &reset); err != nil {
		t.Fatalf("get after reset: %v", err)
	}
	if reset.Status.SiteResourceID != 0 {
		t.Errorf("expected SiteResourceID=0 after 404, got %d", reset.Status.SiteResourceID)
	}

	// Second reconcile: list returns empty → re-creates.
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	var recreated pangolinv1alpha1.PrivateResource
	if err := cl.Get(context.Background(), nn, &recreated); err != nil {
		t.Fatalf("get after recreate: %v", err)
	}
	if recreated.Status.SiteResourceID != 88 {
		t.Errorf("expected SiteResourceID=88 after re-creation, got %d", recreated.Status.SiteResourceID)
	}
}
