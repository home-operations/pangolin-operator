package publicresource

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
	ctrlresolve "github.com/home-operations/pangolin-operator/internal/controller/resolve"
	"github.com/home-operations/pangolin-operator/internal/pangolin"
)

const (
	PublicResourceFinalizer = "pangolin.home-operations.com/publicresource-finalizer"
	resyncInterval          = 10 * time.Minute
	protocolHTTP            = "http"
)

// +kubebuilder:rbac:groups=pangolin.home-operations.com,resources=publicresources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=pangolin.home-operations.com,resources=publicresources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=pangolin.home-operations.com,resources=publicresources/finalizers,verbs=update
// +kubebuilder:rbac:groups=pangolin.home-operations.com,resources=newtsites,verbs=get;list;watch

// Reconciler reconciles a PublicResource object.
type Reconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	PangolinClient pangolin.API
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Starting reconciliation", "publicresource", req.NamespacedName)

	var res pangolinv1alpha1.PublicResource
	if err := r.Get(ctx, req.NamespacedName, &res); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if res.DeletionTimestamp != nil {
		return r.cleanup(ctx, &res)
	}

	if !controllerutil.ContainsFinalizer(&res, PublicResourceFinalizer) {
		controllerutil.AddFinalizer(&res, PublicResourceFinalizer)
		return ctrl.Result{}, r.Update(ctx, &res)
	}

	return r.reconcile(ctx, &res)
}

func (r *Reconciler) reconcile(ctx context.Context, res *pangolinv1alpha1.PublicResource) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	site, err := ctrlresolve.Site(ctx, r.Client, res.Spec.SiteRef)
	if err != nil {
		_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
			setCondition(s, metav1.ConditionFalse, reasonPending, err.Error(), res.Generation)
		})
		if errors.Is(err, ctrlresolve.ErrNotFound) {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	if site.Status.Phase != pangolinv1alpha1.NewtSitePhaseReady || site.Status.SiteID == 0 {
		logger.Info("NewtSite not yet ready, requeueing", "site", res.Spec.SiteRef)
		_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
			setCondition(s, metav1.ConditionFalse, reasonPending, "waiting for NewtSite to become ready", res.Generation)
		})
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	hadID := res.Status.ResourceID != 0
	if err := r.ensureExists(ctx, res, site.Status.SiteID); err != nil {
		if pangolin.IsConflict(err) {
			logger.Info("Pangolin resource already exists with that domain; manual intervention required", "error", err)
			_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
				setCondition(s, metav1.ConditionFalse, reasonPending,
					"a resource with that domain already exists in Pangolin; delete it from Pangolin or change spec.fullDomain to resolve",
					res.Generation)
			})
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
		}
		if pangolin.IsBadRequest(err) {
			_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
				s.Phase = pangolinv1alpha1.PublicResourcePhaseError
				setCondition(s, metav1.ConditionFalse, reasonPermanentError, err.Error(), res.Generation)
			})
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
		}
		_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
			setCondition(s, metav1.ConditionFalse, reasonError, err.Error(), res.Generation)
		})
		return ctrl.Result{}, err
	}

	// After create/adopt the informer cache may still hold stale status;
	// only re-fetch and attempt update for previously-existing resources.
	if hadID {
		if err := r.Get(ctx, client.ObjectKeyFromObject(res), res); err != nil {
			return ctrl.Result{}, err
		}
	}

	if hadID && res.Status.ResourceID != 0 && res.Generation != res.Status.ObservedGeneration {
		if err := r.updateResource(ctx, res, site.Status.SiteID); err != nil {
			if pangolin.IsNotFound(err) {
				logger.Info("Pangolin resource no longer exists during update, will retry", "resourceID", res.Status.ResourceID)
				_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
					s.ResourceID = 0
					s.NiceID = ""
					s.FullDomain = ""
					s.TargetIDs = []int{}
					s.RuleIDs = []int{}
				})
				return ctrl.Result{Requeue: true}, nil
			}
			_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
				setCondition(s, metav1.ConditionFalse, reasonError, err.Error(), res.Generation)
			})
			return ctrl.Result{}, err
		}
	}

	if err := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
		s.Phase = pangolinv1alpha1.PublicResourcePhaseReady
		s.ObservedGeneration = res.Generation
		setCondition(s, metav1.ConditionTrue, reasonReconciled, "resource reconciled successfully", res.Generation)
	}); err != nil {
		return ctrl.Result{}, err
	}

	logger.V(1).Info("PublicResource reconciled", "resourceID", res.Status.ResourceID)
	return ctrl.Result{RequeueAfter: resyncInterval}, nil
}

// ensureExists verifies that the Pangolin resource exists, adopting an existing
// one or creating a new one as needed.
func (r *Reconciler) ensureExists(ctx context.Context, res *pangolinv1alpha1.PublicResource, siteID int) error {
	logger := log.FromContext(ctx)

	items, err := r.PangolinClient.ListResources(ctx, res.Spec.Name)
	if err != nil {
		return fmt.Errorf("ListResources: %w", err)
	}

	match := findResource(items, res.Spec)

	if res.Status.ResourceID != 0 {
		for _, item := range items {
			if item.ResourceID == res.Status.ResourceID {
				return nil // Still exists, nothing to do.
			}
		}
		logger.Info("Pangolin resource no longer exists", "resourceID", res.Status.ResourceID)
	}

	if match != nil {
		logger.Info("Adopting existing Pangolin resource", "resourceID", match.ResourceID)
		return r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
			s.ResourceID = match.ResourceID
			s.NiceID = match.NiceID
			s.FullDomain = match.FullDomain
			s.Phase = pangolinv1alpha1.PublicResourcePhaseCreating
		})
	}

	return r.createResource(ctx, res, siteID)
}

// findResource returns the first item matching by spec criteria.
// For HTTP resources: match by FullDomain.
// For TCP/UDP resources: match by Name.
func findResource(items []pangolin.ResourceItem, spec pangolinv1alpha1.PublicResourceSpec) *pangolin.ResourceItem {
	for i, item := range items {
		if item.Name != spec.Name {
			continue
		}
		if spec.Protocol == protocolHTTP && spec.FullDomain != "" {
			if item.FullDomain == spec.FullDomain {
				return &items[i]
			}
			continue
		}
		return &items[i]
	}
	return nil
}

// buildHTTPUpdateRequest builds the UpdateResourceRequest for HTTP-protocol resources.
func buildHTTPUpdateRequest(spec pangolinv1alpha1.PublicResourceSpec) pangolin.UpdateResourceRequest {
	f := new(false)
	req := pangolin.UpdateResourceRequest{
		Ssl:         new(spec.Ssl),
		Sso:         f,
		BlockAccess: f,
		Enabled:     spec.Enabled,
		ApplyRules:  new(len(spec.Rules) > 0),
	}
	if spec.TlsServerName != "" {
		req.TlsServerName = &spec.TlsServerName
	}
	if spec.HostHeader != "" {
		req.SetHostHeader = &spec.HostHeader
	}
	if spec.Auth != nil && spec.Auth.SsoEnabled {
		req.Sso = new(true)
		if spec.Auth.AutoLoginIdp > 0 {
			req.SkipToIdpId = &spec.Auth.AutoLoginIdp
		}
	}
	if spec.Auth != nil && len(spec.Auth.WhitelistUsers) > 0 {
		req.EmailWhitelistEnabled = new(true)
	}
	return req
}

func (r *Reconciler) createResource(ctx context.Context, res *pangolinv1alpha1.PublicResource, siteID int) error {
	logger := log.FromContext(ctx)

	if err := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
		s.Phase = pangolinv1alpha1.PublicResourcePhaseCreating
	}); err != nil {
		return err
	}

	isHTTP := res.Spec.Protocol == protocolHTTP

	createReq := pangolin.CreateResourceRequest{
		Name:      res.Spec.Name,
		Http:      isHTTP,
		Protocol:  res.Spec.Protocol,
		ProxyPort: res.Spec.ProxyPort,
	}
	if isHTTP {
		createReq.Protocol = "tcp"

		if res.Spec.FullDomain != "" {
			domains, err := r.PangolinClient.ListDomains(ctx)
			if err != nil {
				return fmt.Errorf("ListDomains: %w", err)
			}
			domainID, ok := pangolin.ResolveDomainID(domains, res.Spec.FullDomain)
			if !ok {
				return fmt.Errorf("no Pangolin domain matches %q", res.Spec.FullDomain)
			}
			createReq.DomainId = domainID
			for _, d := range domains {
				if d.DomainID == domainID {
					sub := strings.TrimSuffix(res.Spec.FullDomain, "."+d.BaseDomain)
					sub = strings.TrimSuffix(sub, d.BaseDomain)
					if sub != res.Spec.FullDomain {
						createReq.Subdomain = sub
					}
					break
				}
			}
		}
	}

	created, err := r.PangolinClient.CreateResource(ctx, createReq)
	if err != nil {
		return fmt.Errorf("CreateResource: %w", err)
	}
	logger.Info("Pangolin resource created", "resourceID", created.ResourceID, "fullDomain", created.FullDomain)

	if err := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
		s.ResourceID = created.ResourceID
		s.NiceID = created.NiceID
		s.FullDomain = created.FullDomain
		s.Phase = pangolinv1alpha1.PublicResourcePhaseCreating
	}); err != nil {
		// Rollback to avoid orphaned Pangolin resource.
		logger.Error(err, "failed to persist ResourceID, rolling back Pangolin resource", "resourceID", created.ResourceID)
		if delErr := r.PangolinClient.DeleteResource(ctx, created.ResourceID); delErr != nil {
			logger.Error(delErr, "failed to roll back Pangolin resource", "resourceID", created.ResourceID)
		}
		return err
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(res), res); err != nil {
		return err
	}

	if isHTTP {
		if err := r.PangolinClient.UpdateResource(ctx, created.ResourceID, buildHTTPUpdateRequest(res.Spec)); err != nil {
			return fmt.Errorf("UpdateResource (HTTP settings): %w", err)
		}
	}

	targetIDs, err := r.createTargets(ctx, created.ResourceID, siteID, res.Spec.Targets)
	if err != nil {
		if len(targetIDs) > 0 {
			_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
				s.TargetIDs = targetIDs
			})
		}
		return err
	}

	ruleIDs, err := r.createRules(ctx, created.ResourceID, res.Spec.Rules)
	if err != nil {
		_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
			s.TargetIDs = targetIDs
			s.RuleIDs = ruleIDs
		})
		return err
	}

	return r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
		s.TargetIDs = targetIDs
		s.TargetsHash = hashTargets(res.Spec.Targets)
		s.RuleIDs = ruleIDs
		s.RulesHash = hashRules(res.Spec.Rules)
	})
}

func (r *Reconciler) updateResource(ctx context.Context, res *pangolinv1alpha1.PublicResource, siteID int) error {
	// Always re-apply all settings on update — spec is the source of truth.
	updateReq := pangolin.UpdateResourceRequest{
		Name: res.Spec.Name,
	}
	if res.Spec.Protocol == protocolHTTP {
		httpReq := buildHTTPUpdateRequest(res.Spec)
		httpReq.Name = res.Spec.Name
		updateReq = httpReq
	}
	if err := r.PangolinClient.UpdateResource(ctx, res.Status.ResourceID, updateReq); err != nil {
		return fmt.Errorf("UpdateResource: %w", err)
	}

	// Create new targets/rules before deleting old ones to avoid dropping traffic.
	if hashTargets(res.Spec.Targets) != res.Status.TargetsHash {
		targetIDs, err := r.createTargets(ctx, res.Status.ResourceID, siteID, res.Spec.Targets)
		if err != nil {
			if len(targetIDs) > 0 {
				_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
					s.TargetIDs = append(res.Status.TargetIDs, targetIDs...)
				})
			}
			return err
		}
		for _, id := range res.Status.TargetIDs {
			if err := r.PangolinClient.DeleteTarget(ctx, id); err != nil && !pangolin.IsNotFound(err) {
				_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
					s.TargetIDs = targetIDs
					s.TargetsHash = hashTargets(res.Spec.Targets)
				})
				return fmt.Errorf("DeleteTarget(%d): %w", id, err)
			}
		}
		if err := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
			s.TargetIDs = targetIDs
			s.TargetsHash = hashTargets(res.Spec.Targets)
		}); err != nil {
			return err
		}
	}

	if hashRules(res.Spec.Rules) != res.Status.RulesHash {
		ruleIDs, err := r.createRules(ctx, res.Status.ResourceID, res.Spec.Rules)
		if err != nil {
			if len(ruleIDs) > 0 {
				_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
					s.RuleIDs = append(res.Status.RuleIDs, ruleIDs...)
				})
			}
			return err
		}
		for _, id := range res.Status.RuleIDs {
			if err := r.PangolinClient.DeleteRule(ctx, res.Status.ResourceID, id); err != nil && !pangolin.IsNotFound(err) {
				_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
					s.RuleIDs = ruleIDs
					s.RulesHash = hashRules(res.Spec.Rules)
				})
				return fmt.Errorf("DeleteRule(%d): %w", id, err)
			}
		}
		if err := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
			s.RuleIDs = ruleIDs
			s.RulesHash = hashRules(res.Spec.Rules)
		}); err != nil {
			return err
		}
	}

	return nil
}

func (r *Reconciler) createTargets(ctx context.Context, resourceID, siteID int, targets []pangolinv1alpha1.PublicTargetSpec) ([]int, error) {
	ids := make([]int, 0, len(targets))
	for _, t := range targets {
		target, err := r.PangolinClient.CreateTarget(ctx, resourceID, pangolin.CreateTargetRequest{
			SiteID:          siteID,
			Ip:              t.Hostname,
			Port:            t.Port,
			Method:          t.Method,
			Enabled:         t.Enabled,
			Path:            t.Path,
			PathMatchType:   t.PathMatchType,
			RewritePath:     t.RewritePath,
			RewritePathType: t.RewritePathType,
			Priority:        t.Priority,
		})
		if err != nil {
			return ids, fmt.Errorf("CreateTarget: %w", err)
		}
		ids = append(ids, target.TargetID)
	}
	return ids, nil
}

func (r *Reconciler) createRules(ctx context.Context, resourceID int, rules []pangolinv1alpha1.PublicRuleSpec) ([]int, error) {
	ids := make([]int, 0, len(rules))
	for i, rule := range rules {
		priority := rule.Priority
		if priority == 0 {
			priority = (i + 1) * 10
		}
		created, err := r.PangolinClient.CreateRule(ctx, resourceID, pangolin.CreateRuleRequest{
			Action:   strings.ToUpper(rule.Action),
			Match:    strings.ToUpper(rule.Match),
			Value:    rule.Value,
			Priority: priority,
		})
		if err != nil {
			return ids, fmt.Errorf("CreateRule: %w", err)
		}
		ids = append(ids, created.RuleID)
	}
	return ids, nil
}

func hashTargets(targets []pangolinv1alpha1.PublicTargetSpec) string {
	b, _ := json.Marshal(targets)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func hashRules(rules []pangolinv1alpha1.PublicRuleSpec) string {
	b, _ := json.Marshal(rules)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func (r *Reconciler) cleanup(ctx context.Context, res *pangolinv1alpha1.PublicResource) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Cleaning up PublicResource", "name", res.Name)

	if res.Status.ResourceID != 0 {
		if err := r.PangolinClient.DeleteResource(ctx, res.Status.ResourceID); err != nil && !pangolin.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("delete Pangolin resource %d: %w", res.Status.ResourceID, err)
		}
		logger.Info("Deleted Pangolin resource", "resourceID", res.Status.ResourceID)
	}

	controllerutil.RemoveFinalizer(res, PublicResourceFinalizer)
	if err := r.Update(ctx, res); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) patchStatus(ctx context.Context, res *pangolinv1alpha1.PublicResource, mutate func(*pangolinv1alpha1.PublicResourceStatus)) error {
	patch := client.MergeFrom(res.DeepCopy())
	mutate(&res.Status)
	return r.Status().Patch(ctx, res, patch)
}

// setCondition sets the Ready condition on the status.
func setCondition(s *pangolinv1alpha1.PublicResourceStatus, status metav1.ConditionStatus, reason, message string, generation int64) {
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
		For(&pangolinv1alpha1.PublicResource{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
