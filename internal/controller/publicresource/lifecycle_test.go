package publicresource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestLifecycle_CreateUpdateDelete drives a PublicResource through its full
// lifecycle: create → spec change (generation bump) → delete.
func TestLifecycle_CreateUpdateDelete(t *testing.T) {
	var (
		createResourceCalls atomic.Int32
		updateResourceCalls atomic.Int32
		deleteResourceCalls atomic.Int32
		createTargetCalls   atomic.Int32
		deleteTargetCalls   atomic.Int32
	)

	mux := http.NewServeMux()

	// ListResources — empty on first call (triggers create), returns resource on subsequent calls.
	mux.HandleFunc("/v1/org/org1/resources", func(w http.ResponseWriter, _ *http.Request) {
		if createResourceCalls.Load() == 0 {
			testutil.PangolinResponse(t, w, map[string]any{"resources": []any{}})
			return
		}
		testutil.PangolinResponse(t, w, map[string]any{
			"resources": []pangolin.ResourceItem{{ResourceID: 10, Name: "my-app", NiceID: "r-10"}},
		})
	})
	mux.HandleFunc("/v1/org/org1/resource", func(w http.ResponseWriter, _ *http.Request) {
		createResourceCalls.Add(1)
		testutil.PangolinResponse(t, w, pangolin.CreateResourceResponse{ResourceID: 10, NiceID: "r-10"})
	})
	mux.HandleFunc("/v1/resource/10/target", func(w http.ResponseWriter, _ *http.Request) {
		createTargetCalls.Add(1)
		testutil.PangolinResponse(t, w, pangolin.CreateTargetResponse{TargetID: 50 + int(createTargetCalls.Load())})
	})
	mux.HandleFunc("/v1/resource/10", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			updateResourceCalls.Add(1)
			testutil.PangolinResponse(t, w, nil)
		case http.MethodDelete:
			deleteResourceCalls.Add(1)
			testutil.PangolinResponse(t, w, nil)
		}
	})
	mux.HandleFunc("/v1/target/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteTargetCalls.Add(1)
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-app",
			Namespace:  "default",
			Generation: 1,
			Finalizers: []string{PublicResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			SiteRef:  "my-site",
			Name:     "my-app",
			Protocol: "tcp",
			Targets:  []pangolinv1alpha1.PublicTargetSpec{{Hostname: "backend.svc", Port: 80}},
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	nn := types.NamespacedName{Name: "my-app", Namespace: "default"}

	// --- Phase 1: Create ---
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("create reconcile: %v", err)
	}
	if createResourceCalls.Load() != 1 {
		t.Errorf("expected 1 CreateResource call, got %d", createResourceCalls.Load())
	}
	if createTargetCalls.Load() != 1 {
		t.Errorf("expected 1 CreateTarget call, got %d", createTargetCalls.Load())
	}

	var created pangolinv1alpha1.PublicResource
	if err := cl.Get(context.Background(), nn, &created); err != nil {
		t.Fatalf("get after create: %v", err)
	}
	if created.Status.ResourceID != 10 {
		t.Fatalf("expected ResourceID=10, got %d", created.Status.ResourceID)
	}
	if created.Status.Phase != pangolinv1alpha1.PublicResourcePhaseReady {
		t.Errorf("expected phase Ready, got %s", created.Status.Phase)
	}

	// --- Phase 2: Update (change targets, bump generation) ---
	patch := client.MergeFrom(created.DeepCopy())
	created.Spec.Targets = []pangolinv1alpha1.PublicTargetSpec{{Hostname: "new-backend.svc", Port: 9090}}
	created.Generation = 2
	if err := cl.Patch(context.Background(), &created, patch); err != nil {
		t.Fatalf("patch spec: %v", err)
	}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("update reconcile: %v", err)
	}
	if updateResourceCalls.Load() == 0 {
		t.Error("expected UpdateResource to be called on spec change")
	}
	if createTargetCalls.Load() < 2 {
		t.Error("expected new target to be created")
	}
	if deleteTargetCalls.Load() == 0 {
		t.Error("expected old target to be deleted")
	}

	// --- Phase 3: Delete ---
	var toDelete pangolinv1alpha1.PublicResource
	if err := cl.Get(context.Background(), nn, &toDelete); err != nil {
		t.Fatalf("get before delete: %v", err)
	}
	now := metav1.Now()
	toDelete.DeletionTimestamp = &now
	// Fake client doesn't set DeletionTimestamp via Delete(), so we recreate the object.
	scheme2 := testutil.NewScheme()
	cl2 := testutil.NewClientBuilder(scheme2).
		WithObjects(&toDelete, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()
	r2 := &Reconciler{Client: cl2, Scheme: scheme2, PangolinClient: pc}

	if _, err := r2.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("delete reconcile: %v", err)
	}
	if deleteResourceCalls.Load() != 1 {
		t.Errorf("expected 1 DeleteResource call, got %d", deleteResourceCalls.Load())
	}
}

// TestLifecycle_AdoptExistingResource verifies that when a Pangolin resource
// already exists with a matching name+domain, the operator adopts it.
func TestLifecycle_AdoptExistingResource(t *testing.T) {
	createCalled := false
	updateCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/resources", func(w http.ResponseWriter, _ *http.Request) {
		testutil.PangolinResponse(t, w, map[string]any{
			"resources": []pangolin.ResourceItem{
				{ResourceID: 42, NiceID: "r-42", Name: "my-app", FullDomain: "app.example.com"},
			},
		})
	})
	mux.HandleFunc("/v1/org/org1/resource", func(w http.ResponseWriter, _ *http.Request) {
		createCalled = true
		testutil.PangolinResponse(t, w, pangolin.CreateResourceResponse{ResourceID: 99})
	})
	mux.HandleFunc("/v1/resource/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			updateCalled = true
			testutil.PangolinResponse(t, w, nil)
		}
	})
	mux.HandleFunc("/v1/resource/42/target", func(w http.ResponseWriter, _ *http.Request) {
		testutil.PangolinResponse(t, w, pangolin.CreateTargetResponse{TargetID: 1})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-app",
			Namespace:  "default",
			Finalizers: []string{PublicResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			SiteRef:    "my-site",
			Name:       "my-app",
			Protocol:   "http",
			FullDomain: "app.example.com",
			Targets:    []pangolinv1alpha1.PublicTargetSpec{{Hostname: "backend.svc", Port: 80, Method: "http"}},
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	nn := types.NamespacedName{Name: "my-app", Namespace: "default"}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if createCalled {
		t.Error("expected CreateResource NOT to be called when adopting existing resource")
	}
	if !updateCalled {
		t.Error("expected UpdateResource to be called after adoption to sync spec")
	}

	var updated pangolinv1alpha1.PublicResource
	if err := cl.Get(context.Background(), nn, &updated); err != nil {
		t.Fatalf("get: %v", err)
	}
	if updated.Status.ResourceID != 42 {
		t.Errorf("expected adopted ResourceID=42, got %d", updated.Status.ResourceID)
	}
}

// TestLifecycle_ReAdoptWithStaleHashes verifies that when a previously-reconciled
// resource is re-adopted (Pangolin resource disappeared and a new one matched),
// stale TargetsHash/RulesHash are cleared so targets and rules are recreated.
func TestLifecycle_ReAdoptWithStaleHashes(t *testing.T) {
	var createTargetCalls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/resources", func(w http.ResponseWriter, _ *http.Request) {
		// The old resource (ID=10) is gone; a new one (ID=77) matches.
		testutil.PangolinResponse(t, w, map[string]any{
			"resources": []pangolin.ResourceItem{
				{ResourceID: 77, NiceID: "r-77", Name: "my-app", FullDomain: "app.example.com"},
			},
		})
	})
	mux.HandleFunc("/v1/resource/77", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			testutil.PangolinResponse(t, w, nil)
		}
	})
	mux.HandleFunc("/v1/resource/77/target", func(w http.ResponseWriter, _ *http.Request) {
		createTargetCalls.Add(1)
		testutil.PangolinResponse(t, w, pangolin.CreateTargetResponse{TargetID: 200})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	targets := []pangolinv1alpha1.PublicTargetSpec{{Hostname: "backend.svc", Port: 80, Method: "http"}}
	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-app",
			Namespace:  "default",
			Finalizers: []string{PublicResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			SiteRef:    "my-site",
			Name:       "my-app",
			Protocol:   "http",
			FullDomain: "app.example.com",
			Targets:    targets,
		},
		Status: pangolinv1alpha1.PublicResourceStatus{
			// Simulate a previously-reconciled resource with stale hashes.
			ResourceID:  10,
			NiceID:      "r-10",
			TargetIDs:   []int{99},
			TargetsHash: hashJSON(targets),
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	nn := types.NamespacedName{Name: "my-app", Namespace: "default"}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if createTargetCalls.Load() == 0 {
		t.Error("expected targets to be recreated on re-adopted resource (stale hashes should have been cleared)")
	}

	var updated pangolinv1alpha1.PublicResource
	if err := cl.Get(context.Background(), nn, &updated); err != nil {
		t.Fatalf("get: %v", err)
	}
	if updated.Status.ResourceID != 77 {
		t.Errorf("expected adopted ResourceID=77, got %d", updated.Status.ResourceID)
	}
}

// TestLifecycle_CreateThenSteadyState verifies that a second reconcile after
// a successful create is a no-op: no extra create, update, or target calls.
func TestLifecycle_CreateThenSteadyState(t *testing.T) {
	var (
		createResourceCalls atomic.Int32
		updateResourceCalls atomic.Int32
		createTargetCalls   atomic.Int32
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/resources", func(w http.ResponseWriter, _ *http.Request) {
		if createResourceCalls.Load() == 0 {
			testutil.PangolinResponse(t, w, map[string]any{"resources": []any{}})
			return
		}
		testutil.PangolinResponse(t, w, map[string]any{
			"resources": []pangolin.ResourceItem{{ResourceID: 10, Name: "my-app", NiceID: "r-10"}},
		})
	})
	mux.HandleFunc("/v1/org/org1/resource", func(w http.ResponseWriter, _ *http.Request) {
		createResourceCalls.Add(1)
		testutil.PangolinResponse(t, w, pangolin.CreateResourceResponse{ResourceID: 10, NiceID: "r-10"})
	})
	mux.HandleFunc("/v1/resource/10/target", func(w http.ResponseWriter, _ *http.Request) {
		createTargetCalls.Add(1)
		testutil.PangolinResponse(t, w, pangolin.CreateTargetResponse{TargetID: 50})
	})
	mux.HandleFunc("/v1/resource/10", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			updateResourceCalls.Add(1)
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-app",
			Namespace:  "default",
			Generation: 1,
			Finalizers: []string{PublicResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			SiteRef:  "my-site",
			Name:     "my-app",
			Protocol: "tcp",
			Targets:  []pangolinv1alpha1.PublicTargetSpec{{Hostname: "backend.svc", Port: 80}},
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	nn := types.NamespacedName{Name: "my-app", Namespace: "default"}

	// First reconcile: create.
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("create reconcile: %v", err)
	}
	if createResourceCalls.Load() != 1 {
		t.Fatalf("expected 1 create, got %d", createResourceCalls.Load())
	}

	// Snapshot counters after create.
	createsAfter := createResourceCalls.Load()
	updatesAfter := updateResourceCalls.Load()
	targetsAfter := createTargetCalls.Load()

	// Second reconcile (steady-state requeue, no spec change): must be a no-op.
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("steady-state reconcile: %v", err)
	}
	if createResourceCalls.Load() != createsAfter {
		t.Error("steady-state reconcile must not create a new resource")
	}
	if updateResourceCalls.Load() != updatesAfter {
		t.Error("steady-state reconcile must not call UpdateResource")
	}
	if createTargetCalls.Load() != targetsAfter {
		t.Error("steady-state reconcile must not create new targets")
	}
}

// TestLifecycle_AdoptThenSteadyState verifies that a second reconcile after
// adoption + update is a no-op.
func TestLifecycle_AdoptThenSteadyState(t *testing.T) {
	var (
		updateResourceCalls atomic.Int32
		createTargetCalls   atomic.Int32
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/resources", func(w http.ResponseWriter, _ *http.Request) {
		testutil.PangolinResponse(t, w, map[string]any{
			"resources": []pangolin.ResourceItem{
				{ResourceID: 42, NiceID: "r-42", Name: "my-app", FullDomain: "app.example.com"},
			},
		})
	})
	mux.HandleFunc("/v1/resource/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			updateResourceCalls.Add(1)
			testutil.PangolinResponse(t, w, nil)
		}
	})
	mux.HandleFunc("/v1/resource/42/target", func(w http.ResponseWriter, _ *http.Request) {
		createTargetCalls.Add(1)
		testutil.PangolinResponse(t, w, pangolin.CreateTargetResponse{TargetID: 1})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-app",
			Namespace:  "default",
			Finalizers: []string{PublicResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			SiteRef:    "my-site",
			Name:       "my-app",
			Protocol:   "http",
			FullDomain: "app.example.com",
			Targets:    []pangolinv1alpha1.PublicTargetSpec{{Hostname: "backend.svc", Port: 80, Method: "http"}},
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	nn := types.NamespacedName{Name: "my-app", Namespace: "default"}

	// First reconcile: adopt + update.
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("adopt reconcile: %v", err)
	}
	if updateResourceCalls.Load() != 1 {
		t.Fatalf("expected 1 update after adoption, got %d", updateResourceCalls.Load())
	}

	// Snapshot counters after adopt.
	updatesAfter := updateResourceCalls.Load()
	targetsAfter := createTargetCalls.Load()

	// Second reconcile (steady-state): must be a no-op.
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("steady-state reconcile: %v", err)
	}
	if updateResourceCalls.Load() != updatesAfter {
		t.Error("steady-state reconcile after adopt must not call UpdateResource again")
	}
	if createTargetCalls.Load() != targetsAfter {
		t.Error("steady-state reconcile after adopt must not create targets again")
	}
}

// TestLifecycle_UpdatePersistsBeforeDelete verifies that during a target update,
// new target IDs and hash are persisted to status before old targets are deleted.
// This ensures crash safety — if the operator crashes between persist and delete,
// the next reconcile won't create duplicates.
func TestLifecycle_UpdatePersistsBeforeDelete(t *testing.T) {
	var statusAtDeleteTime pangolinv1alpha1.PublicResourceStatus

	oldTargets := []pangolinv1alpha1.PublicTargetSpec{{Hostname: "old.svc", Port: 80}}
	newTargets := []pangolinv1alpha1.PublicTargetSpec{{Hostname: "new.svc", Port: 9090}}

	scheme := testutil.NewScheme()
	// cl is shared between the reconciler and the delete handler so the handler
	// can read the persisted status at the moment the delete call arrives.
	cl := testutil.NewClientBuilder(scheme).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/resource/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			testutil.PangolinResponse(t, w, nil)
		}
	})
	mux.HandleFunc("/v1/resource/7/target", func(w http.ResponseWriter, _ *http.Request) {
		testutil.PangolinResponse(t, w, pangolin.CreateTargetResponse{TargetID: 200})
	})
	mux.HandleFunc("/v1/target/99", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			// Capture the K8s status at the moment the old target is being deleted.
			var snap pangolinv1alpha1.PublicResource
			if err := cl.Get(context.Background(), client.ObjectKey{Name: "my-res", Namespace: "default"}, &snap); err != nil {
				t.Errorf("get inside delete handler: %v", err)
			}
			statusAtDeleteTime = snap.Status
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{Name: "my-res", Namespace: "default"},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			Name:    "my-res",
			Targets: newTargets,
		},
		Status: pangolinv1alpha1.PublicResourceStatus{
			ResourceID:  7,
			TargetIDs:   []int{99},
			TargetsHash: hashJSON(oldTargets),
		},
	}
	if err := cl.Create(context.Background(), res); err != nil {
		t.Fatalf("create fixture: %v", err)
	}

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	rec := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}

	if err := rec.updateResource(context.Background(), res, 1); err != nil {
		t.Fatalf("updateResource: %v", err)
	}

	// At the time the old target was deleted, status must already have the new IDs+hash.
	if len(statusAtDeleteTime.TargetIDs) != 1 || statusAtDeleteTime.TargetIDs[0] != 200 {
		t.Errorf("at delete time: expected TargetIDs=[200], got %v", statusAtDeleteTime.TargetIDs)
	}
	if statusAtDeleteTime.TargetsHash != hashJSON(newTargets) {
		t.Error("at delete time: expected TargetsHash to match new targets")
	}
}

// TestLifecycle_RulesUpdateCycle verifies that rules are replaced correctly
// when the spec changes.
func TestLifecycle_RulesUpdateCycle(t *testing.T) {
	var (
		createRuleCalls atomic.Int32
		deleteRuleCalls atomic.Int32
	)

	oldRules := []pangolinv1alpha1.PublicRuleSpec{{Action: "DROP", Match: "IP", Value: "1.2.3.4"}}
	newRules := []pangolinv1alpha1.PublicRuleSpec{{Action: "ACCEPT", Match: "CIDR", Value: "10.0.0.0/8"}}
	targets := []pangolinv1alpha1.PublicTargetSpec{{Hostname: "backend.svc", Port: 80}}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/resource/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			testutil.PangolinResponse(t, w, nil)
		}
	})
	mux.HandleFunc("/v1/resource/7/rule", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			createRuleCalls.Add(1)
			testutil.PangolinResponse(t, w, pangolin.CreateRuleResponse{RuleID: 300 + int(createRuleCalls.Load())})
		}
	})
	mux.HandleFunc("/v1/resource/7/rule/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteRuleCalls.Add(1)
			testutil.PangolinResponse(t, w, nil)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{Name: "my-res", Namespace: "default"},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			Name:    "my-res",
			Targets: targets,
			Rules:   newRules,
		},
		Status: pangolinv1alpha1.PublicResourceStatus{
			ResourceID:  7,
			TargetIDs:   []int{50},
			TargetsHash: hashJSON(targets),
			RuleIDs:     []int{10},
			RulesHash:   hashJSON(oldRules),
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	rec := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}

	if err := rec.updateResource(context.Background(), res, 1); err != nil {
		t.Fatalf("updateResource: %v", err)
	}
	if createRuleCalls.Load() == 0 {
		t.Error("expected new rules to be created")
	}
	if deleteRuleCalls.Load() == 0 {
		t.Error("expected old rules to be deleted")
	}

	var updated pangolinv1alpha1.PublicResource
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "my-res", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("get: %v", err)
	}
	if updated.Status.RulesHash != hashJSON(newRules) {
		t.Error("expected RulesHash to match new rules after update")
	}
}

// TestLifecycle_400BadRequest_SetsErrorPhase verifies that a 400 from CreateResource
// sets the phase to Error and returns a terminal error (no requeue).
func TestLifecycle_400BadRequest_SetsErrorPhase(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/resources", func(w http.ResponseWriter, _ *http.Request) {
		testutil.PangolinResponse(t, w, map[string]any{"resources": []any{}})
	})
	mux.HandleFunc("/v1/org/org1/resource", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "invalid proxy port"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "bad-res",
			Namespace:  "default",
			Finalizers: []string{PublicResourceFinalizer},
		},
		Spec: pangolinv1alpha1.PublicResourceSpec{
			SiteRef:   "my-site",
			Name:      "bad-res",
			Protocol:  "tcp",
			ProxyPort: -1,
			Targets:   []pangolinv1alpha1.PublicTargetSpec{{Hostname: "backend.svc", Port: 80}},
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
		NamespacedName: types.NamespacedName{Name: "bad-res", Namespace: "default"},
	})
	if err == nil {
		t.Fatal("expected terminal error on 400 bad request")
	}
	if !strings.Contains(err.Error(), "terminal") {
		t.Errorf("expected terminal error, got: %v", err)
	}

	var updated pangolinv1alpha1.PublicResource
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "bad-res", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("get: %v", err)
	}
	if updated.Status.Phase != pangolinv1alpha1.PublicResourcePhaseError {
		t.Errorf("expected phase Error, got %s", updated.Status.Phase)
	}
}

// TestLifecycle_UpdateNotFound_ResetsAndRecreates verifies that a 404 during
// updateResource resets the ResourceID and the next reconcile re-creates.
func TestLifecycle_UpdateNotFound_ResetsAndRecreates(t *testing.T) {
	var listCalls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/org1/resources", func(w http.ResponseWriter, _ *http.Request) {
		n := listCalls.Add(1)
		if n == 1 {
			// First call: resource still exists in list → ensureExists passes.
			testutil.PangolinResponse(t, w, map[string]any{
				"resources": []pangolin.ResourceItem{{ResourceID: 7, Name: "my-res"}},
			})
			return
		}
		// Subsequent calls: resource gone → triggers re-create.
		testutil.PangolinResponse(t, w, map[string]any{"resources": []any{}})
	})
	mux.HandleFunc("/v1/resource/7", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/v1/org/org1/resource", func(w http.ResponseWriter, _ *http.Request) {
		testutil.PangolinResponse(t, w, pangolin.CreateResourceResponse{ResourceID: 50, NiceID: "r-50"})
	})
	mux.HandleFunc("/v1/resource/50", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			testutil.PangolinResponse(t, w, nil)
		}
	})
	mux.HandleFunc("/v1/resource/50/target", func(w http.ResponseWriter, _ *http.Request) {
		testutil.PangolinResponse(t, w, pangolin.CreateTargetResponse{TargetID: 100})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res := &pangolinv1alpha1.PublicResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-res",
			Namespace:  "default",
			Generation: 2,
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
			ObservedGeneration: 1,
			TargetIDs:          []int{99},
			TargetsHash:        "stale",
		},
	}

	scheme := testutil.NewScheme()
	cl := testutil.NewClientBuilder(scheme).
		WithObjects(res, testutil.ReadySite()).
		WithStatusSubresource(&pangolinv1alpha1.PublicResource{}).
		Build()

	pc := pangolin.NewClient(pangolin.Credentials{Endpoint: srv.URL, APIKey: "key", OrgID: "org1"})
	r := &Reconciler{Client: cl, Scheme: scheme, PangolinClient: pc}
	nn := types.NamespacedName{Name: "my-res", Namespace: "default"}

	// First reconcile: ensureExists passes (resource in list), but updateResource
	// returns 404 → resets ResourceID and requeues.
	result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn})
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter after 404 reset")
	}

	var reset pangolinv1alpha1.PublicResource
	if err := cl.Get(context.Background(), nn, &reset); err != nil {
		t.Fatalf("get after reset: %v", err)
	}
	if reset.Status.ResourceID != 0 {
		t.Errorf("expected ResourceID=0 after 404, got %d", reset.Status.ResourceID)
	}

	// Second reconcile: list returns empty → re-creates the resource.
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: nn}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	var recreated pangolinv1alpha1.PublicResource
	if err := cl.Get(context.Background(), nn, &recreated); err != nil {
		t.Fatalf("get after recreate: %v", err)
	}
	if recreated.Status.ResourceID != 50 {
		t.Errorf("expected ResourceID=50 after re-creation, got %d", recreated.Status.ResourceID)
	}
}
