package privateresource

import (
	"context"
	"errors"
	"fmt"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
	ctrlresolve "github.com/home-operations/pangolin-operator/internal/controller/resolve"
	"github.com/home-operations/pangolin-operator/internal/pangolin"
)

const (
	PrivateResourceFinalizer = "pangolin.home-operations.com/privateresource-finalizer"
	resyncInterval           = 10 * time.Minute
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
		return r.cleanup(ctx, &res)
	}

	if !controllerutil.ContainsFinalizer(&res, PrivateResourceFinalizer) {
		controllerutil.AddFinalizer(&res, PrivateResourceFinalizer)
		return ctrl.Result{}, r.Update(ctx, &res)
	}

	return r.reconcile(ctx, &res)
}

func (r *Reconciler) reconcile(ctx context.Context, res *pangolinv1alpha1.PrivateResource) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	site, err := ctrlresolve.Site(ctx, r.Client, res.Spec.SiteRef)
	if err != nil {
		_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
			setCondition(s, metav1.ConditionFalse, reasonPending, err.Error(), res.Generation)
		})
		if errors.Is(err, ctrlresolve.ErrNotFound) || errors.Is(err, ctrlresolve.ErrAmbiguous) {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	if site.Status.Phase != pangolinv1alpha1.NewtSitePhaseReady || site.Status.SiteID == 0 {
		logger.Info("NewtSite not yet ready, requeueing", "site", res.Spec.SiteRef)
		_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
			setCondition(s, metav1.ConditionFalse, reasonPending, "waiting for NewtSite to become ready", res.Generation)
		})
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if res.Status.SiteResourceID == 0 {
		if err := r.createSiteResource(ctx, res, site.Status.SiteID); err != nil {
			if pangolin.IsBadRequest(err) {
				_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
					s.Phase = pangolinv1alpha1.PrivateResourcePhaseError
					setCondition(s, metav1.ConditionFalse, reasonPermanentError, err.Error(), res.Generation)
				})
				return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
			}
			_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
				setCondition(s, metav1.ConditionFalse, reasonError, err.Error(), res.Generation)
			})
			return ctrl.Result{}, err
		}
		if err := r.Get(ctx, client.ObjectKeyFromObject(res), res); err != nil {
			return ctrl.Result{}, err
		}
	} else if res.Generation != res.Status.ObservedGeneration {
		if err := r.updateSiteResource(ctx, res, site.Status.SiteID); err != nil {
			if pangolin.IsNotFound(err) {
				logger.Info("Pangolin site resource no longer exists, will re-create", "siteResourceID", res.Status.SiteResourceID)
				if patchErr := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
					s.SiteResourceID = 0
					s.NiceID = ""
				}); patchErr != nil {
					return ctrl.Result{}, patchErr
				}
				return ctrl.Result{RequeueAfter: time.Second}, nil
			}
			_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
				setCondition(s, metav1.ConditionFalse, reasonError, err.Error(), res.Generation)
			})
			return ctrl.Result{}, err
		}
	} else if res.Status.SiteResourceID != 0 {
		// Steady-state drift check — verify the resource still exists via list.
		siteResources, err := r.PangolinClient.ListSiteResources(ctx, res.Spec.Name)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("drift check ListSiteResources: %w", err)
		}
		found := false
		for _, sr := range siteResources {
			if sr.SiteResourceID == res.Status.SiteResourceID {
				found = true
				break
			}
		}
		if !found {
			logger.Info("Pangolin site resource no longer exists, resetting for re-creation", "siteResourceID", res.Status.SiteResourceID)
			if patchErr := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
				s.SiteResourceID = 0
				s.NiceID = ""
			}); patchErr != nil {
				return ctrl.Result{}, patchErr
			}
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
	}

	if err := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
		s.Phase = pangolinv1alpha1.PrivateResourcePhaseReady
		s.ObservedGeneration = res.Generation
		setCondition(s, metav1.ConditionTrue, reasonReconciled, "resource reconciled successfully", res.Generation)
	}); err != nil {
		return ctrl.Result{}, err
	}

	logger.V(1).Info("PrivateResource reconciled", "siteResourceID", res.Status.SiteResourceID)
	return ctrl.Result{RequeueAfter: resyncInterval}, nil
}

// orEmpty returns s if non-nil, otherwise an empty slice of the same type.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func (r *Reconciler) createSiteResource(ctx context.Context, res *pangolinv1alpha1.PrivateResource, siteID int) error {
	logger := log.FromContext(ctx)

	if err := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
		s.Phase = pangolinv1alpha1.PrivateResourcePhaseCreating
	}); err != nil {
		return err
	}

	created, err := r.PangolinClient.CreateSiteResource(ctx, pangolin.CreateSiteResourceRequest{
		Name:               res.Spec.Name,
		SiteID:             siteID,
		Mode:               res.Spec.Mode,
		Destination:        res.Spec.Destination,
		TcpPortRangeString: res.Spec.TcpPorts,
		UdpPortRangeString: res.Spec.UdpPorts,
		DisableIcmp:        res.Spec.DisableIcmp,
		Alias:              res.Spec.Alias,
		RoleIds:            orEmpty(res.Spec.RoleIds),
		UserIds:            orEmpty(res.Spec.UserIds),
		ClientIds:          orEmpty(res.Spec.ClientIds),
	})
	if err != nil {
		return fmt.Errorf("CreateSiteResource: %w", err)
	}
	logger.Info("Pangolin site resource created", "siteResourceID", created.SiteResourceID)

	if patchErr := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PrivateResourceStatus) {
		s.SiteResourceID = created.SiteResourceID
		s.NiceID = created.NiceID
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
	if err := r.PangolinClient.UpdateSiteResource(ctx, res.Status.SiteResourceID, pangolin.UpdateSiteResourceRequest{
		SiteID:             siteID,
		Name:               res.Spec.Name,
		Mode:               res.Spec.Mode,
		Destination:        res.Spec.Destination,
		TcpPortRangeString: res.Spec.TcpPorts,
		UdpPortRangeString: res.Spec.UdpPorts,
		DisableIcmp:        res.Spec.DisableIcmp,
		Alias:              res.Spec.Alias,
		RoleIds:            orEmpty(res.Spec.RoleIds),
		UserIds:            orEmpty(res.Spec.UserIds),
		ClientIds:          orEmpty(res.Spec.ClientIds),
	}); err != nil {
		return fmt.Errorf("UpdateSiteResource: %w", err)
	}
	return nil
}

func (r *Reconciler) cleanup(ctx context.Context, res *pangolinv1alpha1.PrivateResource) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Cleaning up PrivateResource", "name", res.Name)

	if res.Status.SiteResourceID != 0 {
		if err := r.PangolinClient.DeleteSiteResource(ctx, res.Status.SiteResourceID); err != nil && !pangolin.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("delete Pangolin site resource %d: %w", res.Status.SiteResourceID, err)
		}
		logger.Info("Deleted Pangolin site resource", "siteResourceID", res.Status.SiteResourceID)
	}

	controllerutil.RemoveFinalizer(res, PrivateResourceFinalizer)
	if err := r.Update(ctx, res); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) patchStatus(ctx context.Context, res *pangolinv1alpha1.PrivateResource, mutate func(*pangolinv1alpha1.PrivateResourceStatus)) error {
	patch := client.MergeFrom(res.DeepCopy())
	mutate(&res.Status)
	return r.Status().Patch(ctx, res, patch)
}

func setCondition(s *pangolinv1alpha1.PrivateResourceStatus, status metav1.ConditionStatus, reason, message string, generation int64) {
	apimeta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             status,
		ObservedGeneration: generation,
		Reason:             reason,
		Message:            message,
	})
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pangolinv1alpha1.PrivateResource{}).
		Complete(r)
}
