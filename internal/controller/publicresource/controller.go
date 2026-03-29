package publicresource

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
	"github.com/home-operations/pangolin-operator/internal/pangolin"
)

const (
	PublicResourceFinalizer = "pangolin.home-operations.com/publicresource-finalizer"
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

	siteNamespace := res.Spec.SiteNamespace
	if siteNamespace == "" {
		siteNamespace = res.Namespace
	}
	var site pangolinv1alpha1.NewtSite
	if err := r.Get(ctx, client.ObjectKey{Namespace: siteNamespace, Name: res.Spec.SiteRef}, &site); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, fmt.Errorf("get NewtSite %q: %w", res.Spec.SiteRef, err)
		}
		logger.Info("NewtSite not found, requeueing", "site", res.Spec.SiteRef, "siteNamespace", siteNamespace)
		_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
			setCondition(s, metav1.ConditionFalse, reasonPending, fmt.Sprintf("NewtSite %q not found", res.Spec.SiteRef), res.Generation)
		})
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if site.Status.Phase != pangolinv1alpha1.NewtSitePhaseReady || site.Status.SiteID == 0 {
		logger.Info("NewtSite not yet ready, requeueing", "site", res.Spec.SiteRef)
		_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
			setCondition(s, metav1.ConditionFalse, reasonPending, "waiting for NewtSite to become ready", res.Generation)
		})
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if res.Status.ResourceID == 0 {
		if err := r.createResource(ctx, res, site.Status.SiteID); err != nil {
			if pangolin.IsConflict(err) {
				logger.Info("Pangolin resource already exists with that domain; manual intervention required", "error", err)
				_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
					setCondition(s, metav1.ConditionFalse, reasonPending,
						"a resource with that domain already exists in Pangolin; delete it from Pangolin or change spec.fullDomain to resolve",
						res.Generation)
				})
				return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
			}
			_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
				setCondition(s, metav1.ConditionFalse, reasonError, err.Error(), res.Generation)
			})
			return ctrl.Result{}, err
		}
	} else if res.Generation != res.Status.ObservedGeneration {
		if err := r.updateResource(ctx, res, site.Status.SiteID); err != nil {
			if pangolin.IsNotFound(err) {
				logger.Info("Pangolin resource no longer exists, will re-create", "resourceID", res.Status.ResourceID)
				if patchErr := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
					s.ResourceID = 0
					s.NiceID = ""
					s.FullDomain = ""
					s.TargetIDs = []int{}
					s.RuleIDs = []int{}
				}); patchErr != nil {
					return ctrl.Result{}, patchErr
				}
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
	return ctrl.Result{}, nil
}

func (r *Reconciler) createResource(ctx context.Context, res *pangolinv1alpha1.PublicResource, siteID int) error {
	logger := log.FromContext(ctx)

	if err := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
		s.Phase = pangolinv1alpha1.PublicResourcePhaseCreating
	}); err != nil {
		return err
	}

	isHTTP := res.Spec.Protocol == "http"

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
		return err
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(res), res); err != nil {
		return err
	}

	if isHTTP {
		if err := r.applyHTTPSettings(ctx, created.ResourceID, res); err != nil {
			return err
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

func (r *Reconciler) applyHTTPSettings(ctx context.Context, resourceID int, res *pangolinv1alpha1.PublicResource) error {
	f := new(false)
	updateReq := pangolin.UpdateResourceRequest{
		Ssl:         new(res.Spec.Ssl),
		Sso:         f,
		BlockAccess: f,
	}
	updateReq.Enabled = res.Spec.Enabled
	if res.Spec.TlsServerName != "" {
		updateReq.TlsServerName = &res.Spec.TlsServerName
	}
	if res.Spec.HostHeader != "" {
		updateReq.SetHostHeader = &res.Spec.HostHeader
	}
	if res.Spec.Auth != nil && res.Spec.Auth.SsoEnabled {
		updateReq.Sso = new(true)
		if res.Spec.Auth.AutoLoginIdp > 0 {
			updateReq.SkipToIdpId = &res.Spec.Auth.AutoLoginIdp
		}
	}
	if res.Spec.Auth != nil && len(res.Spec.Auth.WhitelistUsers) > 0 {
		updateReq.EmailWhitelistEnabled = new(true)
	}
	if len(res.Spec.Rules) > 0 {
		updateReq.ApplyRules = new(true)
	}
	if err := r.PangolinClient.UpdateResource(ctx, resourceID, updateReq); err != nil {
		return fmt.Errorf("UpdateResource (HTTP settings): %w", err)
	}
	return nil
}

func (r *Reconciler) updateResource(ctx context.Context, res *pangolinv1alpha1.PublicResource, siteID int) error {
	live, err := r.PangolinClient.GetResource(ctx, res.Status.ResourceID)
	if err != nil {
		return fmt.Errorf("GetResource: %w", err)
	}

	// Always re-apply all settings on update — spec is the source of truth.
	updateReq := pangolin.UpdateResourceRequest{}
	if live.Name != res.Spec.Name {
		updateReq.Name = res.Spec.Name
	}
	if res.Spec.Protocol == "http" {
		f := new(false)
		updateReq.Ssl = new(res.Spec.Ssl)
		updateReq.Sso = f
		updateReq.BlockAccess = f
		updateReq.Enabled = res.Spec.Enabled
		if res.Spec.TlsServerName != "" {
			updateReq.TlsServerName = &res.Spec.TlsServerName
		}
		if res.Spec.HostHeader != "" {
			updateReq.SetHostHeader = &res.Spec.HostHeader
		}
		if res.Spec.Auth != nil && res.Spec.Auth.SsoEnabled {
			updateReq.Sso = new(true)
			if res.Spec.Auth.AutoLoginIdp > 0 {
				updateReq.SkipToIdpId = &res.Spec.Auth.AutoLoginIdp
			}
		}
		if res.Spec.Auth != nil && len(res.Spec.Auth.WhitelistUsers) > 0 {
			updateReq.EmailWhitelistEnabled = new(true)
		}
		updateReq.ApplyRules = new(len(res.Spec.Rules) > 0)
	}
	if err := r.PangolinClient.UpdateResource(ctx, res.Status.ResourceID, updateReq); err != nil {
		return fmt.Errorf("UpdateResource: %w", err)
	}

	if hashTargets(res.Spec.Targets) != res.Status.TargetsHash {
		for _, id := range res.Status.TargetIDs {
			if err := r.PangolinClient.DeleteTarget(ctx, id); err != nil && !pangolin.IsNotFound(err) {
				return fmt.Errorf("DeleteTarget(%d): %w", id, err)
			}
		}
		targetIDs, err := r.createTargets(ctx, res.Status.ResourceID, siteID, res.Spec.Targets)
		if err != nil {
			if len(targetIDs) > 0 {
				_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
					s.TargetIDs = targetIDs
				})
			}
			return err
		}
		if err := r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
			s.TargetIDs = targetIDs
			s.TargetsHash = hashTargets(res.Spec.Targets)
		}); err != nil {
			return err
		}
	}

	if hashRules(res.Spec.Rules) != res.Status.RulesHash {
		for _, id := range res.Status.RuleIDs {
			if err := r.PangolinClient.DeleteRule(ctx, id); err != nil && !pangolin.IsNotFound(err) {
				return fmt.Errorf("DeleteRule(%d): %w", id, err)
			}
		}
		ruleIDs, err := r.createRules(ctx, res.Status.ResourceID, res.Spec.Rules)
		if err != nil {
			if len(ruleIDs) > 0 {
				_ = r.patchStatus(ctx, res, func(s *pangolinv1alpha1.PublicResourceStatus) {
					s.RuleIDs = ruleIDs
				})
			}
			return err
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
		For(&pangolinv1alpha1.PublicResource{}).
		Complete(r)
}
