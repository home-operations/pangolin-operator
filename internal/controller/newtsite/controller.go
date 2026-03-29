package newtsite

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
	"github.com/home-operations/pangolin-operator/internal/autodiscover"
	"github.com/home-operations/pangolin-operator/internal/pangolin"
)

const (
	NewtSiteFinalizer = "pangolin.home-operations.com/newtsite-finalizer"
)

// +kubebuilder:rbac:groups=pangolin.home-operations.com,resources=newtsites,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=pangolin.home-operations.com,resources=newtsites/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=pangolin.home-operations.com,resources=newtsites/finalizers,verbs=update
// +kubebuilder:rbac:groups=pangolin.home-operations.com,resources=publicresources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch

type Reconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	PangolinClient pangolin.API
	NewtEndpoint   string
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Starting reconciliation", "newtsite", req.NamespacedName)

	var site pangolinv1alpha1.NewtSite
	if err := r.Get(ctx, req.NamespacedName, &site); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if site.DeletionTimestamp != nil {
		return r.cleanup(ctx, &site)
	}

	if !controllerutil.ContainsFinalizer(&site, NewtSiteFinalizer) {
		controllerutil.AddFinalizer(&site, NewtSiteFinalizer)
		return ctrl.Result{}, r.Update(ctx, &site)
	}

	return r.reconcile(ctx, &site)
}

func (r *Reconciler) reconcile(ctx context.Context, site *pangolinv1alpha1.NewtSite) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if site.Status.SiteID == 0 {
		if err := r.createSite(ctx, site); err != nil {
			_ = r.patchStatus(ctx, site, func(s *pangolinv1alpha1.NewtSiteStatus) {
				setCondition(s, metav1.ConditionFalse, reasonError, err.Error(), site.Generation)
			})
			return ctrl.Result{}, err
		}
		if err := r.Get(ctx, client.ObjectKeyFromObject(site), site); err != nil {
			return ctrl.Result{}, err
		}
	} else if site.Generation != site.Status.ObservedGeneration {
		if err := r.updateSite(ctx, site); err != nil {
			_ = r.patchStatus(ctx, site, func(s *pangolinv1alpha1.NewtSiteStatus) {
				setCondition(s, metav1.ConditionFalse, reasonError, err.Error(), site.Generation)
			})
			return ctrl.Result{}, err
		}
	}

	online := false
	if site.Spec.Type != "local" {
		if err := r.ensureServiceAccount(ctx, site); err != nil {
			_ = r.patchStatus(ctx, site, func(s *pangolinv1alpha1.NewtSiteStatus) {
				setCondition(s, metav1.ConditionFalse, reasonError, err.Error(), site.Generation)
			})
			return ctrl.Result{}, err
		}
		if err := r.ensureDeployment(ctx, site); err != nil {
			_ = r.patchStatus(ctx, site, func(s *pangolinv1alpha1.NewtSiteStatus) {
				setCondition(s, metav1.ConditionFalse, reasonError, err.Error(), site.Generation)
			})
			return ctrl.Result{}, err
		}
		var dep appsv1.Deployment
		if err := r.Get(ctx, client.ObjectKey{Namespace: site.Namespace, Name: site.Name}, &dep); err == nil {
			online = dep.Status.ReadyReplicas > 0
		}
	}

	if err := r.patchStatus(ctx, site, func(s *pangolinv1alpha1.NewtSiteStatus) {
		s.Phase = pangolinv1alpha1.NewtSitePhaseReady
		s.ObservedGeneration = site.Generation
		s.Online = online
		setCondition(s, metav1.ConditionTrue, reasonReconciled, "site reconciled successfully", site.Generation)
	}); err != nil {
		return ctrl.Result{}, err
	}

	logger.V(1).Info("NewtSite reconciled", "siteID", site.Status.SiteID)

	if err := r.reconcileAutodiscover(ctx, site); err != nil {
		logger.Error(err, "autodiscover scan failed")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *Reconciler) createSite(ctx context.Context, site *pangolinv1alpha1.NewtSite) error {
	logger := log.FromContext(ctx)

	if err := r.patchStatus(ctx, site, func(s *pangolinv1alpha1.NewtSiteStatus) {
		s.Phase = pangolinv1alpha1.NewtSitePhaseCreating
	}); err != nil {
		return err
	}

	if site.Spec.Type == "local" {
		created, err := r.PangolinClient.CreateSite(ctx, pangolin.CreateSiteRequest{
			Name: site.Spec.Name,
			Type: "local",
		})
		if err != nil {
			if updateErr := r.patchStatus(ctx, site, func(s *pangolinv1alpha1.NewtSiteStatus) {
				s.Phase = pangolinv1alpha1.NewtSitePhaseError
			}); updateErr != nil {
				logger.Error(updateErr, "Failed to update status to Error after CreateSite failure")
			}
			return fmt.Errorf("CreateSite: %w", err)
		}
		logger.Info("Pangolin local site created", "siteID", created.SiteID, "niceID", created.NiceID)
		return r.patchStatus(ctx, site, func(s *pangolinv1alpha1.NewtSiteStatus) {
			s.SiteID = created.SiteID
			s.NiceID = created.NiceID
		})
	}

	defaults, err := r.PangolinClient.PickSiteDefaults(ctx)
	if err != nil {
		return fmt.Errorf("PickSiteDefaults: %w", err)
	}

	created, err := r.PangolinClient.CreateSite(ctx, pangolin.CreateSiteRequest{
		Name:    site.Spec.Name,
		Address: defaults.ClientAddress,
		Type:    "newt",
		NewtID:  defaults.NewtID,
		Secret:  defaults.NewtSecret,
	})
	if err != nil {
		if updateErr := r.patchStatus(ctx, site, func(s *pangolinv1alpha1.NewtSiteStatus) {
			s.Phase = pangolinv1alpha1.NewtSitePhaseError
		}); updateErr != nil {
			logger.Error(updateErr, "Failed to update status to Error after CreateSite failure")
		}
		return fmt.Errorf("CreateSite: %w", err)
	}
	logger.Info("Pangolin site created", "siteID", created.SiteID, "niceID", created.NiceID)

	secretName := site.Name + "-newt-credentials"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: site.Namespace,
		},
		StringData: map[string]string{
			"PANGOLIN_ENDPOINT": r.NewtEndpoint,
			"NEWT_ID":           defaults.NewtID,
			"NEWT_SECRET":       defaults.NewtSecret,
		},
	}
	if err := controllerutil.SetControllerReference(site, secret, r.Scheme); err != nil {
		return fmt.Errorf("set controller reference on secret: %w", err)
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		secret.StringData = map[string]string{
			"PANGOLIN_ENDPOINT": r.NewtEndpoint,
			"NEWT_ID":           defaults.NewtID,
			"NEWT_SECRET":       defaults.NewtSecret,
		}
		return nil
	}); err != nil {
		return fmt.Errorf("create/update newt credentials secret: %w", err)
	}

	return r.patchStatus(ctx, site, func(s *pangolinv1alpha1.NewtSiteStatus) {
		s.SiteID = created.SiteID
		s.NiceID = created.NiceID
		s.NewtSecretName = secretName
	})
}

func (r *Reconciler) updateSite(ctx context.Context, site *pangolinv1alpha1.NewtSite) error {
	live, err := r.PangolinClient.GetSite(ctx, site.Status.SiteID)
	if err != nil {
		return fmt.Errorf("GetSite: %w", err)
	}
	if live.Name != site.Spec.Name {
		if err := r.PangolinClient.UpdateSite(ctx, site.Status.SiteID, pangolin.UpdateSiteRequest{Name: site.Spec.Name}); err != nil {
			return fmt.Errorf("UpdateSite: %w", err)
		}
	}
	return nil
}

func (r *Reconciler) ensureServiceAccount(ctx context.Context, site *pangolinv1alpha1.NewtSite) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      site.Name + "-newtsite",
			Namespace: site.Namespace,
		},
	}
	if err := controllerutil.SetControllerReference(site, sa, r.Scheme); err != nil {
		return fmt.Errorf("set controller reference on serviceaccount: %w", err)
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error { return nil })
	return err
}

func (r *Reconciler) ensureDeployment(ctx context.Context, site *pangolinv1alpha1.NewtSite) error {
	secretName := site.Name + "-newt-credentials"
	desired := buildDeployment(site, secretName)
	if err := controllerutil.SetControllerReference(site, desired, r.Scheme); err != nil {
		return fmt.Errorf("set controller reference on deployment: %w", err)
	}

	desiredLabels := desired.Labels
	desiredSpec := desired.Spec

	existing := &appsv1.Deployment{}
	existing.Name = desired.Name
	existing.Namespace = desired.Namespace

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.Labels = desiredLabels
		existing.Spec = desiredSpec
		return controllerutil.SetControllerReference(site, existing, r.Scheme)
	})
	return err
}

func (r *Reconciler) cleanup(ctx context.Context, site *pangolinv1alpha1.NewtSite) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Cleaning up NewtSite", "name", site.Name)

	if site.Status.SiteID != 0 {
		if err := r.PangolinClient.DeleteSite(ctx, site.Status.SiteID); err != nil && !pangolin.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("delete Pangolin site %d: %w", site.Status.SiteID, err)
		}
		logger.Info("Deleted Pangolin site", "siteID", site.Status.SiteID)
	}

	controllerutil.RemoveFinalizer(site, NewtSiteFinalizer)
	if err := r.Update(ctx, site); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// patchStatus applies status mutations via a typed merge-from patch.
// The mutate function receives the status struct to modify in place.
func (r *Reconciler) patchStatus(ctx context.Context, site *pangolinv1alpha1.NewtSite, mutate func(*pangolinv1alpha1.NewtSiteStatus)) error {
	patch := client.MergeFrom(site.DeepCopy())
	mutate(&site.Status)
	return r.Status().Patch(ctx, site, patch)
}

// setCondition sets the Ready condition on the status.
func setCondition(s *pangolinv1alpha1.NewtSiteStatus, status metav1.ConditionStatus, reason, message string, generation int64) {
	apimeta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             status,
		ObservedGeneration: generation,
		Reason:             reason,
		Message:            message,
	})
}

func (r *Reconciler) ReconcileHTTPRoute(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var route gatewayv1.HTTPRoute
	if err := r.Get(ctx, req.NamespacedName, &route); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return r.deleteAllHTTPRouteResources(ctx, req.Name)
		}
		return ctrl.Result{}, err
	}

	annotations := route.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	site, cfg, prefix, ok, err := r.resolveSiteForHTTPRoute(ctx, annotations, &route)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !ok {
		return ctrl.Result{}, nil
	}

	if autodiscover.IsOptOut(annotations, prefix) {
		return r.deleteAllHTTPRouteResources(ctx, route.Name)
	}

	r.processHTTPRoute(ctx, site, cfg, &route)
	return ctrl.Result{}, nil
}

func (r *Reconciler) processHTTPRoute(ctx context.Context, site *pangolinv1alpha1.NewtSite, cfg *pangolinv1alpha1.AutoDiscoverSpec, route *gatewayv1.HTTPRoute) {
	logger := log.FromContext(ctx)
	for _, hostname := range route.Spec.Hostnames {
		host := string(hostname)
		spec, err := autodiscover.BuildHTTPRouteSpec(route, host, route.GetAnnotations(), cfg, site.Name)
		if err != nil {
			logger.Error(err, "skipping hostname", "hostname", host, "route", route.Name)
			continue
		}
		spec.SiteNamespace = site.Namespace
		resName := autodiscover.HostnameToResourceName(route.Name, host)
		if err := autodiscover.EnsureHTTPRouteResource(ctx, r.Client, site, route.Name, site.Namespace, resName, spec); err != nil {
			logger.Error(err, "failed to ensure PublicResource for HTTPRoute hostname", "hostname", host)
		}
	}
}

func (r *Reconciler) ReconcileService(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var svc corev1.Service
	if err := r.Get(ctx, req.NamespacedName, &svc); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return r.deleteAllServiceResources(ctx, req.Name)
		}
		return ctrl.Result{}, err
	}

	annotations := svc.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	site, cfg, prefix, ok, err := r.resolveSiteForService(ctx, annotations)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !ok {
		return ctrl.Result{}, nil
	}

	if autodiscover.IsOptOut(annotations, prefix) {
		return r.deleteAllServiceResources(ctx, svc.Name)
	}

	r.processService(ctx, site, cfg, prefix, &svc)
	return ctrl.Result{}, nil
}

func (r *Reconciler) processService(ctx context.Context, site *pangolinv1alpha1.NewtSite, cfg *pangolinv1alpha1.AutoDiscoverSpec, prefix string, svc *corev1.Service) {
	logger := log.FromContext(ctx)
	annotations := svc.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	if !cfg.EnableServiceDiscovery {
		return
	}

	siteRef, ok := annotations[prefix+"/site-ref"]
	if !ok || siteRef == "" {
		return
	}

	clusterHostname := fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, svc.Namespace)
	hasFullDomain := len(annotations[prefix+"/full-domain"]) > 0

	if !hasFullDomain && autodiscover.ResolveAllPorts(annotations, prefix, cfg) {
		specs := autodiscover.BuildAllPortSpecs(svc, annotations, prefix, siteRef, clusterHostname)
		for resName, spec := range specs {
			spec.SiteNamespace = site.Namespace
			if err := autodiscover.EnsureServiceResource(ctx, r.Client, site, svc.Name, svc.Namespace, resName, spec); err != nil {
				logger.Error(err, "failed to ensure PublicResource for Service port", "resource", resName)
			}
		}
	} else {
		resName, spec, ok := autodiscover.BuildSinglePortSpec(svc, annotations, prefix, siteRef, clusterHostname, cfg)
		if !ok {
			return
		}
		spec.SiteNamespace = site.Namespace
		if err := autodiscover.EnsureServiceResource(ctx, r.Client, site, svc.Name, svc.Namespace, resName, spec); err != nil {
			logger.Error(err, "failed to ensure PublicResource for Service", "resource", resName)
		}
	}
}

func (r *Reconciler) reconcileAutodiscover(ctx context.Context, site *pangolinv1alpha1.NewtSite) error {
	if site.Spec.AutoDiscover == nil {
		return nil
	}
	cfg := site.Spec.AutoDiscover
	p := cfg.AnnotationPrefix
	if p == "" {
		p = autodiscover.DefaultAnnotationPrefix
	}

	var routes gatewayv1.HTTPRouteList
	if cfg.EnableRouteDiscovery {
		if err := r.List(ctx, &routes); err != nil {
			return fmt.Errorf("list HTTPRoutes: %w", err)
		}
		for i := range routes.Items {
			route := &routes.Items[i]
			annotations := route.GetAnnotations()
			if annotations == nil {
				annotations = map[string]string{}
			}
			if autodiscover.IsOptOut(annotations, p) {
				continue
			}
			matched := false
			if siteRef := annotations[p+"/site-ref"]; siteRef == site.Name {
				if ns := annotations[p+"/site-namespace"]; ns == "" || ns == site.Namespace {
					matched = true
				}
			}
			if !matched && cfg.GatewayName != "" {
				matched = autodiscover.RouteReferencesGateway(route, cfg.GatewayName, cfg.GatewayNamespace)
			}
			if matched {
				r.processHTTPRoute(ctx, site, cfg, route)
			}
		}
	}

	if cfg.EnableServiceDiscovery {
		var svcs corev1.ServiceList
		if err := r.List(ctx, &svcs); err != nil {
			return fmt.Errorf("list Services: %w", err)
		}
		for i := range svcs.Items {
			svc := &svcs.Items[i]
			annotations := svc.GetAnnotations()
			if annotations == nil {
				annotations = map[string]string{}
			}
			if autodiscover.IsOptOut(annotations, p) {
				continue
			}
			siteRef, ok := annotations[p+"/site-ref"]
			if !ok || siteRef != site.Name {
				continue
			}
			if ns := annotations[p+"/site-namespace"]; ns != "" && ns != site.Namespace {
				continue
			}
			r.processService(ctx, site, cfg, p, svc)
		}
	}

	return nil
}

func (r *Reconciler) resolveSiteForHTTPRoute(ctx context.Context, annotations map[string]string, route *gatewayv1.HTTPRoute) (*pangolinv1alpha1.NewtSite, *pangolinv1alpha1.AutoDiscoverSpec, string, bool, error) {
	var siteList pangolinv1alpha1.NewtSiteList
	if err := r.List(ctx, &siteList); err != nil {
		return nil, nil, "", false, err
	}

	for i := range siteList.Items {
		site := &siteList.Items[i]
		if site.Spec.AutoDiscover == nil {
			continue
		}
		cfg := site.Spec.AutoDiscover
		if !cfg.EnableRouteDiscovery {
			continue
		}
		p := cfg.AnnotationPrefix
		if p == "" {
			p = autodiscover.DefaultAnnotationPrefix
		}

		if siteRef := annotations[p+"/site-ref"]; siteRef == site.Name {
			if ns := annotations[p+"/site-namespace"]; ns == "" || ns == site.Namespace {
				return site, cfg, p, true, nil
			}
		}

		if cfg.GatewayName != "" && route != nil && autodiscover.RouteReferencesGateway(route, cfg.GatewayName, cfg.GatewayNamespace) {
			return site, cfg, p, true, nil
		}
	}

	return nil, nil, "", false, nil
}

func (r *Reconciler) resolveSiteForService(ctx context.Context, annotations map[string]string) (*pangolinv1alpha1.NewtSite, *pangolinv1alpha1.AutoDiscoverSpec, string, bool, error) {
	var siteList pangolinv1alpha1.NewtSiteList
	if err := r.List(ctx, &siteList); err != nil {
		return nil, nil, "", false, err
	}

	for i := range siteList.Items {
		site := &siteList.Items[i]
		if site.Spec.AutoDiscover == nil {
			continue
		}
		cfg := site.Spec.AutoDiscover
		if !cfg.EnableServiceDiscovery {
			continue
		}
		p := cfg.AnnotationPrefix
		if p == "" {
			p = autodiscover.DefaultAnnotationPrefix
		}
		if siteRef := annotations[p+"/site-ref"]; siteRef == site.Name {
			if ns := annotations[p+"/site-namespace"]; ns == "" || ns == site.Namespace {
				return site, cfg, p, true, nil
			}
		}
	}

	return nil, nil, "", false, nil
}

func (r *Reconciler) deleteAllHTTPRouteResources(ctx context.Context, routeName string) (ctrl.Result, error) {
	// PublicResources are created in site.Namespace, not the HTTPRoute namespace, so list cluster-wide.
	var list pangolinv1alpha1.PublicResourceList
	if err := r.List(ctx, &list, client.MatchingLabels{
		"pangolin.home-operations.com/owner-kind": "httproute",
		"pangolin.home-operations.com/owner-name": routeName,
	}); err != nil {
		return ctrl.Result{}, err
	}
	for i := range list.Items {
		if err := r.Delete(ctx, &list.Items[i]); err != nil && client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) deleteAllServiceResources(ctx context.Context, svcName string) (ctrl.Result, error) {
	// PublicResources are created in site.Namespace, not the Service namespace, so list cluster-wide.
	var list pangolinv1alpha1.PublicResourceList
	if err := r.List(ctx, &list, client.MatchingLabels{
		"pangolin.home-operations.com/owner-kind": "service",
		"pangolin.home-operations.com/owner-name": svcName,
	}); err != nil {
		return ctrl.Result{}, err
	}
	for i := range list.Items {
		if err := r.Delete(ctx, &list.Items[i]); err != nil && client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&pangolinv1alpha1.NewtSite{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&pangolinv1alpha1.PublicResource{}).
		Complete(r); err != nil {
		return err
	}

	httpRoutePredicate := predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return hasParentRef(e.Object) },
		UpdateFunc:  func(e event.UpdateEvent) bool { return hasParentRef(e.ObjectNew) || hasParentRef(e.ObjectOld) },
		DeleteFunc:  func(e event.DeleteEvent) bool { return hasParentRef(e.Object) },
		GenericFunc: func(e event.GenericEvent) bool { return hasParentRef(e.Object) },
	}

	servicePredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool { return hasSiteRefAnnotation(e.Object) },
		UpdateFunc: func(e event.UpdateEvent) bool {
			return hasSiteRefAnnotation(e.ObjectNew) || hasSiteRefAnnotation(e.ObjectOld)
		},
		DeleteFunc:  func(e event.DeleteEvent) bool { return hasSiteRefAnnotation(e.Object) },
		GenericFunc: func(e event.GenericEvent) bool { return hasSiteRefAnnotation(e.Object) },
	}

	if err := ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.HTTPRoute{}, builder.WithPredicates(httpRoutePredicate)).
		Complete(reconcile.Func(r.ReconcileHTTPRoute)); err != nil {
		return fmt.Errorf("setup HTTPRoute controller: %w", err)
	}

	if err := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}, builder.WithPredicates(servicePredicate)).
		Complete(reconcile.Func(r.ReconcileService)); err != nil {
		return fmt.Errorf("setup Service controller: %w", err)
	}

	return nil
}

func hasSiteRefAnnotation(obj client.Object) bool {
	for k := range obj.GetAnnotations() {
		if strings.HasSuffix(k, "/site-ref") {
			return true
		}
	}
	return false
}

func hasParentRef(obj client.Object) bool {
	route, ok := obj.(*gatewayv1.HTTPRoute)
	if !ok {
		return false
	}
	return len(route.Spec.ParentRefs) > 0
}
