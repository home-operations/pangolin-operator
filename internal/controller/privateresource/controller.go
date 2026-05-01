package privateresource

import (
	"context"
	"errors"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
	ctrlresolve "github.com/home-operations/pangolin-operator/internal/controller/resolve"
	"github.com/home-operations/pangolin-operator/internal/controller/shared"
	"github.com/home-operations/pangolin-operator/internal/pangolin"
)

const (
	PrivateResourceFinalizer = "pangolin.home-operations.com/privateresource-finalizer"
	resyncInterval           = shared.ResyncInterval
	reconcileTimeout         = shared.ReconcileTimeout
	modeHTTP                 = "http"
)

// +kubebuilder:rbac:groups=pangolin.home-operations.com,resources=privateresources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=pangolin.home-operations.com,resources=privateresources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=pangolin.home-operations.com,resources=privateresources/finalizers,verbs=update
// +kubebuilder:rbac:groups=pangolin.home-operations.com,resources=newtsites,verbs=get;list;watch

// Reconciler reconciles a PrivateResource object.
type Reconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	PangolinClient pangolin.API
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Starting reconciliation", "privateresource", req.NamespacedName)

	var res pangolinv1alpha1.PrivateResource
	if err := r.Get(ctx, req.NamespacedName, &res); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if res.DeletionTimestamp != nil {
		return ctrl.Result{}, r.cleanup(ctx, &res)
	}

	if !controllerutil.ContainsFinalizer(&res, PrivateResourceFinalizer) {
		controllerutil.AddFinalizer(&res, PrivateResourceFinalizer)
		if err := r.Update(ctx, &res); err != nil {
			return ctrl.Result{}, err
		}
	}

	reconcileCtx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	defer cancel()
	return r.reconcile(reconcileCtx, &res)
}

func (r *Reconciler) reconcile(ctx context.Context, res *pangolinv1alpha1.PrivateResource) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	site, err := ctrlresolve.Site(ctx, r.Client, res.Spec.SiteRef)
	if err != nil {
		if patchErr := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
			shared.SetCondition(&s.Conditions, metav1.ConditionFalse, shared.ReasonPending, err.Error(), res.Generation)
		}); patchErr != nil {
			logger.Error(patchErr, "failed to patch status")
		}
		if errors.Is(err, ctrlresolve.ErrNotFound) {
			return ctrl.Result{RequeueAfter: shared.RequeueDependencyWait}, nil
		}
		return ctrl.Result{}, err
	}

	if site.Status.Phase != pangolinv1alpha1.NewtSitePhaseReady || site.Status.SiteID == 0 {
		logger.Info("NewtSite not yet ready, requeueing", "site", res.Spec.SiteRef)
		if patchErr := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
			shared.SetCondition(&s.Conditions, metav1.ConditionFalse, shared.ReasonPending, "waiting for NewtSite to become ready", res.Generation)
		}); patchErr != nil {
			logger.Error(patchErr, "failed to patch status")
		}
		return ctrl.Result{RequeueAfter: shared.RequeueDependencyWait}, nil
	}

	hadID := res.Status.SiteResourceID != 0
	adopted, err := r.ensureExists(ctx, res, site.Status.SiteID)
	if err != nil {
		if pangolin.IsBadRequest(err) {
			if patchErr := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
				s.Phase = pangolinv1alpha1.PrivateResourcePhaseError
				shared.SetCondition(&s.Conditions, metav1.ConditionFalse, shared.ReasonPermanentError, err.Error(), res.Generation)
			}); patchErr != nil {
				logger.Error(patchErr, "failed to patch status")
			}
			return ctrl.Result{}, reconcile.TerminalError(err)
		}
		if patchErr := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
			shared.SetCondition(&s.Conditions, metav1.ConditionFalse, shared.ReasonError, err.Error(), res.Generation)
		}); patchErr != nil {
			logger.Error(patchErr, "failed to patch status")
		}
		return ctrl.Result{}, err
	}

	// Re-fetch so the update check sees the latest ObservedGeneration.
	// Skip when we just adopted — the in-memory res already has the correct
	// SiteResourceID from patchStatus; a cache read could return stale data.
	if hadID && !adopted {
		if err := r.Get(ctx, client.ObjectKeyFromObject(res), res); err != nil {
			return ctrl.Result{}, err
		}
	}

	needsUpdate := adopted || (hadID && res.Status.SiteResourceID != 0 && res.Generation != res.Status.ObservedGeneration)
	if needsUpdate {
		if err := r.updateSiteResource(ctx, res, site.Status.SiteID); err != nil {
			if pangolin.IsNotFound(err) {
				logger.Info("Pangolin site resource no longer exists during update, will retry", "siteResourceID", res.Status.SiteResourceID)
				if patchErr := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
					s.SiteResourceID = 0
					s.NiceID = ""
				}); patchErr != nil {
					logger.Error(patchErr, "failed to patch status")
				}
				return ctrl.Result{RequeueAfter: time.Second}, nil
			}
			if patchErr := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
				shared.SetCondition(&s.Conditions, metav1.ConditionFalse, shared.ReasonError, err.Error(), res.Generation)
			}); patchErr != nil {
				logger.Error(patchErr, "failed to patch status")
			}
			return ctrl.Result{}, err
		}
	}

	if err := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
		s.Phase = pangolinv1alpha1.PrivateResourcePhaseReady
		s.ObservedGeneration = res.Generation
		shared.SetCondition(&s.Conditions, metav1.ConditionTrue, shared.ReasonReconciled, "resource reconciled successfully", res.Generation)
	}); err != nil {
		return ctrl.Result{}, err
	}

	logger.V(1).Info("PrivateResource reconciled", "siteResourceID", res.Status.SiteResourceID)
	return ctrl.Result{RequeueAfter: resyncInterval}, nil
}

// ensureExists verifies that the Pangolin site resource exists, adopting an
// existing one or creating a new one as needed. It returns adopted=true when an
// existing Pangolin resource was linked so the caller knows to apply the spec.
func (r *Reconciler) ensureExists(ctx context.Context, res *pangolinv1alpha1.PrivateResource, siteID int) (adopted bool, err error) {
	logger := log.FromContext(ctx)

	items, err := r.PangolinClient.ListSiteResources(ctx, res.Spec.Name)
	if err != nil {
		return false, fmt.Errorf("ListSiteResources: %w", err)
	}

	match := findSiteResource(items, res.Spec, siteID)

	if res.Status.SiteResourceID != 0 {
		for _, item := range items {
			if item.SiteResourceID == res.Status.SiteResourceID {
				return false, nil // Still exists, nothing to do.
			}
		}
		logger.Info("Pangolin site resource no longer exists", "siteResourceID", res.Status.SiteResourceID)
	}

	if match != nil {
		logger.Info("Adopting existing Pangolin site resource", "siteResourceID", match.SiteResourceID)
		return true, r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
			s.SiteResourceID = match.SiteResourceID
			s.NiceID = match.NiceID
			s.FullDomain = match.FullDomain
			s.Phase = pangolinv1alpha1.PrivateResourcePhaseCreating
		})
	}

	return false, r.createSiteResource(ctx, res, siteID)
}

// findSiteResource matches by Name+Mode and either FullDomain (mode=http) or
// Destination+SiteID. HTTP resources match on FullDomain so the backend can
// change without forcing a recreate.
func findSiteResource(items []pangolin.SiteResourceItem, spec pangolinv1alpha1.PrivateResourceSpec, siteID int) *pangolin.SiteResourceItem {
	for i, item := range items {
		if item.Name != spec.Name || item.Mode != spec.Mode {
			continue
		}
		if spec.Mode == modeHTTP {
			if spec.FullDomain != "" && item.FullDomain == spec.FullDomain {
				return &items[i]
			}
			continue
		}
		if item.Destination == spec.Destination && item.SiteID == siteID {
			return &items[i]
		}
	}
	return nil
}

// orEmpty returns s if non-nil, otherwise an empty slice of the same type.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// httpFields holds the mode=http payload fields shared by create and update.
type httpFields struct {
	DomainID, Subdomain, Scheme string
	DestinationPort             int
	Ssl                         *bool
}

func (r *Reconciler) httpFieldsFor(ctx context.Context, spec pangolinv1alpha1.PrivateResourceSpec) (httpFields, error) {
	if spec.FullDomain == "" {
		return httpFields{}, fmt.Errorf("fullDomain is required when mode is %q", modeHTTP)
	}
	domains, err := r.PangolinClient.ListDomains(ctx)
	if err != nil {
		return httpFields{}, fmt.Errorf("ListDomains: %w", err)
	}
	domainID, subdomain, ok := pangolin.ResolveDomain(domains, spec.FullDomain)
	if !ok {
		return httpFields{}, fmt.Errorf("no Pangolin domain matches %q", spec.FullDomain)
	}
	return httpFields{
		DomainID:        domainID,
		Subdomain:       subdomain,
		Scheme:          spec.Scheme,
		DestinationPort: spec.DestinationPort,
		Ssl:             spec.Ssl,
	}, nil
}

func (r *Reconciler) createSiteResource(ctx context.Context, res *pangolinv1alpha1.PrivateResource, siteID int) error {
	logger := log.FromContext(ctx)

	if err := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
		s.Phase = pangolinv1alpha1.PrivateResourcePhaseCreating
	}); err != nil {
		return err
	}

	req := pangolin.CreateSiteResourceRequest{
		Name:        res.Spec.Name,
		SiteID:      siteID,
		Mode:        res.Spec.Mode,
		Destination: res.Spec.Destination,
		DisableIcmp: res.Spec.DisableIcmp,
		Alias:       res.Spec.Alias,
		RoleIds:     orEmpty(res.Spec.RoleIds),
		UserIds:     orEmpty(res.Spec.UserIds),
		ClientIds:   orEmpty(res.Spec.ClientIds),
	}
	if res.Spec.Mode == modeHTTP {
		h, err := r.httpFieldsFor(ctx, res.Spec)
		if err != nil {
			return err
		}
		req.DomainId, req.Subdomain = h.DomainID, h.Subdomain
		req.Scheme, req.DestinationPort, req.Ssl = h.Scheme, h.DestinationPort, h.Ssl
	} else {
		req.TcpPortRangeString = res.Spec.TcpPorts
		req.UdpPortRangeString = res.Spec.UdpPorts
	}

	created, err := r.PangolinClient.CreateSiteResource(ctx, req)
	if err != nil {
		return fmt.Errorf("CreateSiteResource: %w", err)
	}
	logger.Info("Pangolin site resource created", "siteResourceID", created.SiteResourceID)

	if patchErr := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
		s.SiteResourceID = created.SiteResourceID
		s.NiceID = created.NiceID
		s.FullDomain = created.FullDomain
		s.Phase = pangolinv1alpha1.PrivateResourcePhaseCreating
	}); patchErr != nil {
		// Rollback to avoid orphaned Pangolin site resource.
		logger.Error(patchErr, "failed to persist SiteResourceID, rolling back Pangolin site resource", "siteResourceID", created.SiteResourceID)
		if delErr := r.PangolinClient.DeleteSiteResource(ctx, created.SiteResourceID); delErr != nil {
			logger.Error(delErr, "failed to roll back Pangolin site resource", "siteResourceID", created.SiteResourceID)
		}
		return patchErr
	}
	return nil
}

func (r *Reconciler) updateSiteResource(ctx context.Context, res *pangolinv1alpha1.PrivateResource, siteID int) error {
	req := pangolin.UpdateSiteResourceRequest{
		SiteID:      siteID,
		Name:        res.Spec.Name,
		Mode:        res.Spec.Mode,
		Destination: res.Spec.Destination,
		DisableIcmp: res.Spec.DisableIcmp,
		Alias:       res.Spec.Alias,
		RoleIds:     orEmpty(res.Spec.RoleIds),
		UserIds:     orEmpty(res.Spec.UserIds),
		ClientIds:   orEmpty(res.Spec.ClientIds),
	}
	if res.Spec.Mode == modeHTTP {
		h, err := r.httpFieldsFor(ctx, res.Spec)
		if err != nil {
			return err
		}
		req.DomainId, req.Subdomain = h.DomainID, h.Subdomain
		req.Scheme, req.DestinationPort, req.Ssl = h.Scheme, h.DestinationPort, h.Ssl
	} else {
		req.TcpPortRangeString = res.Spec.TcpPorts
		req.UdpPortRangeString = res.Spec.UdpPorts
	}

	if err := r.PangolinClient.UpdateSiteResource(ctx, res.Status.SiteResourceID, req); err != nil {
		return fmt.Errorf("UpdateSiteResource: %w", err)
	}
	return nil
}

func (r *Reconciler) cleanup(ctx context.Context, res *pangolinv1alpha1.PrivateResource) error {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Cleaning up PrivateResource", "name", res.Name)

	if res.Status.SiteResourceID != 0 {
		if err := r.PangolinClient.DeleteSiteResource(ctx, res.Status.SiteResourceID); err != nil && !pangolin.IsNotFound(err) {
			return fmt.Errorf("delete Pangolin site resource %d: %w", res.Status.SiteResourceID, err)
		}
		logger.Info("Deleted Pangolin site resource", "siteResourceID", res.Status.SiteResourceID)
	}

	controllerutil.RemoveFinalizer(res, PrivateResourceFinalizer)
	if err := r.Update(ctx, res); err != nil {
		return fmt.Errorf("remove finalizer: %w", err)
	}
	return nil
}

func (r *Reconciler) patchStatus(ctx context.Context, res *pangolinv1alpha1.PrivateResource, mutate func(*pangolinv1alpha1.PrivateResourceStatus)) error {
	patch := client.MergeFrom(res.DeepCopy())
	mutate(&res.Status)
	return r.Status().Patch(ctx, res, patch)
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pangolinv1alpha1.PrivateResource{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
