package autodiscover

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
)

const (
	// DefaultAnnotationPrefix is the annotation prefix used when none is configured.
	DefaultAnnotationPrefix = "pangolin-operator"

	methodHTTP  = "http"
	methodHTTPS = "https"
	methodH2C   = "h2c"
)

func annotationPrefix(cfg *pangolinv1alpha1.AutoDiscoverSpec) string {
	if cfg.AnnotationPrefix != "" {
		return cfg.AnnotationPrefix
	}
	return DefaultAnnotationPrefix
}

func IsOptOut(annotations map[string]string, prefix string) bool {
	v, ok := annotations[prefix+"/enabled"]
	return ok && (v == "false" || v == "0")
}

func IsOptIn(annotations map[string]string, prefix string) bool {
	v, ok := annotations[prefix+"/enabled"]
	return ok && (v == "true" || v == "1")
}

func isTruthy(v string) bool { return v == "true" || v == "1" }

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for v := range strings.SplitSeq(s, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// annotationResolver centralizes annotation reading for autodiscovery.
// All Build*Spec functions use this to resolve annotations consistently,
// so common fields like Enabled cannot be accidentally omitted.
type annotationResolver struct {
	annotations map[string]string
	prefix      string
	cfg         *pangolinv1alpha1.AutoDiscoverSpec
}

func newResolver(annotations map[string]string, cfg *pangolinv1alpha1.AutoDiscoverSpec) annotationResolver {
	return annotationResolver{
		annotations: annotations,
		prefix:      annotationPrefix(cfg),
		cfg:         cfg,
	}
}

func (r annotationResolver) get(key string) string {
	return r.annotations[r.prefix+"/"+key]
}

func (r annotationResolver) lookup(key string) (string, bool) {
	v, ok := r.annotations[r.prefix+"/"+key]
	return v, ok
}

func (r annotationResolver) enabled() bool {
	return r.enabledOr(true)
}

func (r annotationResolver) enabledOr(def bool) bool {
	if v, ok := r.lookup("enabled"); ok {
		return isTruthy(v)
	}
	return def
}

func (r annotationResolver) name(defaultName string) string {
	if v, ok := r.lookup("name"); ok && v != "" {
		return v
	}
	return defaultName
}

func (r annotationResolver) siteRef(fallback string) (string, bool) {
	if v, ok := r.lookup("site-ref"); ok && v != "" {
		return v, true
	}
	if fallback != "" {
		return fallback, true
	}
	return "", false
}

func (r annotationResolver) ssl() bool {
	if v, ok := r.lookup("ssl"); ok {
		return isTruthy(v)
	}
	return r.cfg.SSL
}

func (r annotationResolver) method(defaultMethod string) string {
	if v, ok := r.lookup("method"); ok {
		v = strings.ToLower(strings.TrimSpace(v))
		switch v {
		case methodHTTP, methodHTTPS, methodH2C:
			return v
		}
	}
	return defaultMethod
}

func (r annotationResolver) tlsServerName(defaultName string) string {
	if v := strings.TrimSpace(r.get("tls-server-name")); v != "" {
		return v
	}
	return defaultName
}

func (r annotationResolver) proxyPort(defaultPort int) int {
	if v, ok := r.lookup("proxy-port"); ok {
		if p, err := strconv.Atoi(v); err == nil && p >= 1 && p <= 65535 {
			return p
		}
	}
	return defaultPort
}

func (r annotationResolver) protocol(defaultProto string) string {
	if v, ok := r.lookup("protocol"); ok {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "tcp" || v == "udp" {
			return v
		}
	}
	return defaultProto
}

func (r annotationResolver) headers() []pangolinv1alpha1.PublicHeaderSpec {
	return buildHeaders(r.annotations, r.prefix)
}

func (r annotationResolver) auth() *pangolinv1alpha1.PublicAuthSpec {
	return buildAuth(r.annotations, r.cfg)
}

func (r annotationResolver) maintenance() *pangolinv1alpha1.PublicMaintenanceSpec {
	return buildMaintenance(r.annotations, r.prefix)
}

func (r annotationResolver) rules() []pangolinv1alpha1.PublicRuleSpec {
	return buildRules(r.annotations, r.prefix, r.cfg)
}

func (r annotationResolver) targetExtras(base pangolinv1alpha1.PublicTargetSpec) pangolinv1alpha1.PublicTargetSpec {
	return buildTargetExtras(base, r.annotations, r.prefix)
}

// buildHTTPSpec builds a PublicResourceSpec for an HTTP protocol resource.
// Shared by HTTPRoute and Service (with full-domain) discovery.
// The enabled parameter controls the default: route discovery passes r.enabled()
// (default true), service discovery passes r.enabledOr(false).
func (r annotationResolver) buildHTTPSpec(siteRef, name, hostname string, enabled bool, target pangolinv1alpha1.PublicTargetSpec) pangolinv1alpha1.PublicResourceSpec {
	return pangolinv1alpha1.PublicResourceSpec{
		SiteRef:       siteRef,
		Name:          name,
		Protocol:      methodHTTP,
		FullDomain:    hostname,
		Ssl:           r.ssl(),
		Enabled:       enabled,
		HostHeader:    r.get("host-header"),
		TlsServerName: r.tlsServerName(hostname),
		Headers:       r.headers(),
		Auth:          r.auth(),
		Maintenance:   r.maintenance(),
		Rules:         r.rules(),
		Targets:       []pangolinv1alpha1.PublicTargetSpec{target},
	}
}

// buildTCPSpec builds a PublicResourceSpec for a TCP/UDP protocol resource.
// Shared by TCPRoute, Service (single-port), and Service (all-ports) discovery.
// The enabled parameter controls the default: route discovery passes r.enabled()
// (default true), service discovery passes r.enabledOr(false).
func (r annotationResolver) buildTCPSpec(siteRef, name, protocol string, proxyPort int, enabled bool, target pangolinv1alpha1.PublicTargetSpec) pangolinv1alpha1.PublicResourceSpec {
	return pangolinv1alpha1.PublicResourceSpec{
		SiteRef:   siteRef,
		Name:      name,
		Protocol:  protocol,
		ProxyPort: proxyPort,
		Enabled:   enabled,
		Targets:   []pangolinv1alpha1.PublicTargetSpec{target},
	}
}

// buildHeaders parses {prefix}/headers as a JSON array of {name,value} objects.
func buildHeaders(annotations map[string]string, prefix string) []pangolinv1alpha1.PublicHeaderSpec {
	raw, ok := annotations[prefix+"/headers"]
	if !ok || raw == "" {
		return nil
	}
	var out []pangolinv1alpha1.PublicHeaderSpec
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// buildRules parses {prefix}/rules as a JSON array and appends deny-country rules from cfg.
func buildRules(annotations map[string]string, prefix string, cfg *pangolinv1alpha1.AutoDiscoverSpec) []pangolinv1alpha1.PublicRuleSpec {
	var rules []pangolinv1alpha1.PublicRuleSpec
	if raw, ok := annotations[prefix+"/rules"]; ok && raw != "" {
		var parsed []pangolinv1alpha1.PublicRuleSpec
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			for _, r := range parsed {
				if isValidRule(r) {
					rules = append(rules, r)
				}
			}
		}
	}
	if cfg.DenyCountries != "" {
		for country := range strings.SplitSeq(cfg.DenyCountries, ",") {
			country = strings.TrimSpace(country)
			if country != "" {
				rules = append(rules, pangolinv1alpha1.PublicRuleSpec{Action: "DROP", Match: "country", Value: country})
			}
		}
	}
	if len(rules) == 0 {
		return nil
	}
	return rules
}

func isValidRule(r pangolinv1alpha1.PublicRuleSpec) bool {
	validActions := map[string]bool{"ACCEPT": true, "DROP": true, "PASS": true}
	validMatches := map[string]bool{"cidr": true, "ip": true, "path": true, "country": true}
	if !validActions[r.Action] || !validMatches[r.Match] || r.Value == "" {
		return false
	}
	if r.Priority != 0 && (r.Priority < 1 || r.Priority > 1000) {
		return false
	}
	return true
}

func buildMaintenance(annotations map[string]string, prefix string) *pangolinv1alpha1.PublicMaintenanceSpec {
	v, ok := annotations[prefix+"/maintenance-enabled"]
	if !ok || !isTruthy(v) {
		return nil
	}
	return &pangolinv1alpha1.PublicMaintenanceSpec{
		Enabled:       true,
		Type:          annotations[prefix+"/maintenance-type"],
		Title:         annotations[prefix+"/maintenance-title"],
		Message:       annotations[prefix+"/maintenance-message"],
		EstimatedTime: annotations[prefix+"/maintenance-estimated-time"],
	}
}

func buildAuth(annotations map[string]string, cfg *pangolinv1alpha1.AutoDiscoverSpec) *pangolinv1alpha1.PublicAuthSpec {
	prefix := annotationPrefix(cfg)

	ssoEnabled := false
	if v, ok := annotations[prefix+"/auth-sso"]; ok && isTruthy(v) {
		ssoEnabled = true
	}

	var ssoRoles, ssoUsers []string
	idp := 0
	if ssoEnabled {
		rolesRaw := cfg.AuthSSORoles
		if av, ok := annotations[prefix+"/auth-sso-roles"]; ok {
			rolesRaw = av
		}
		ssoRoles = splitCSV(rolesRaw)

		usersRaw := cfg.AuthSSOUsers
		if av, ok := annotations[prefix+"/auth-sso-users"]; ok {
			usersRaw = av
		}
		ssoUsers = splitCSV(usersRaw)

		idp = cfg.AuthSSOIDP
		if av, ok := annotations[prefix+"/auth-sso-idp"]; ok {
			if parsed, err := strconv.Atoi(av); err == nil && parsed > 0 {
				idp = parsed
			}
		}
	}

	whitelistRaw := cfg.AuthWhitelistUsers
	if av, ok := annotations[prefix+"/auth-whitelist-users"]; ok {
		whitelistRaw = av
	}
	whitelistUsers := splitCSV(whitelistRaw)

	var authSecretRef string
	if v, ok := annotations[prefix+"/auth-secret"]; ok && v != "" {
		authSecretRef = v
	}

	if !ssoEnabled && len(whitelistUsers) == 0 && authSecretRef == "" {
		return nil
	}

	return &pangolinv1alpha1.PublicAuthSpec{
		SsoEnabled:     ssoEnabled,
		SsoRoles:       ssoRoles,
		SsoUsers:       ssoUsers,
		WhitelistUsers: whitelistUsers,
		AutoLoginIdp:   idp,
		AuthSecretRef:  authSecretRef,
	}
}

func buildTargetExtras(base pangolinv1alpha1.PublicTargetSpec, annotations map[string]string, prefix string) pangolinv1alpha1.PublicTargetSpec {
	t := base
	if v := strings.TrimSpace(annotations[prefix+"/target-path"]); v != "" {
		t.Path = v
	}
	if v := strings.TrimSpace(annotations[prefix+"/target-path-match"]); v != "" {
		switch v {
		case "prefix", "exact", "regex":
			t.PathMatchType = v
		}
	}
	if v := strings.TrimSpace(annotations[prefix+"/target-rewrite-path"]); v != "" {
		t.RewritePath = v
	}
	if v := strings.TrimSpace(annotations[prefix+"/target-rewrite-match"]); v != "" {
		switch v {
		case "exact", "prefix", "regex", "stripPrefix":
			t.RewritePathType = v
		}
	}
	if v := strings.TrimSpace(annotations[prefix+"/target-priority"]); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 1 && parsed <= 1000 {
			t.Priority = parsed
		}
	}
	if v := strings.TrimSpace(annotations[prefix+"/target-enabled"]); v != "" {
		enabled := isTruthy(v)
		t.Enabled = &enabled
	}
	return t
}

// resolveBackendRef extracts a target hostname and port from a Gateway API BackendObjectReference.
func resolveBackendRef(ref gatewayv1.BackendObjectReference, routeNamespace string, defaultPort int) (hostname string, port int) {
	refNamespace := routeNamespace
	if ref.Namespace != nil && *ref.Namespace != "" {
		refNamespace = string(*ref.Namespace)
	}
	hostname = fmt.Sprintf("%s.%s.svc.cluster.local", ref.Name, refNamespace)
	port = defaultPort
	if ref.Port != nil {
		port = int(*ref.Port)
	}
	return
}

func RouteReferencesGateway(route *gatewayv1.HTTPRoute, gatewayName, gatewayNamespace string) bool {
	for _, parent := range route.Spec.ParentRefs {
		if parent.Name != gatewayv1.ObjectName(gatewayName) {
			continue
		}
		if gatewayNamespace != "" && parent.Namespace != nil && string(*parent.Namespace) != gatewayNamespace {
			continue
		}
		return true
	}
	return false
}

func HostnameToResourceName(sourceName, hostname string) string {
	slug := strings.ReplaceAll(hostname, ".", "-")
	name := sourceName + "-" + slug
	if len(name) > 253 {
		name = name[:253]
	}
	return name
}

func ServiceResourceName(namespace, name, port, proto string) string {
	return fmt.Sprintf("%s-%s-%s-%s", namespace, name, port, proto)
}

// BuildHTTPRouteSpec builds a PublicResourceSpec for one hostname of an HTTPRoute.
// siteRefFallback is used when no site-ref annotation is present (gateway-based discovery).
//
// When cfg.GatewayName is set (gateway-based discovery), the target is the gateway service
// described by cfg.GatewayTargetHostname/GatewayTargetPort/GatewayTargetMethod — traffic
// flows through the gateway, not directly to the backend. The backendRef is not used.
//
// When cfg.GatewayName is empty (annotation-based discovery), the target is derived from
// the HTTPRoute's first backendRef as before.
func BuildHTTPRouteSpec(route *gatewayv1.HTTPRoute, hostname string, annotations map[string]string, cfg *pangolinv1alpha1.AutoDiscoverSpec, siteRefFallback string) (pangolinv1alpha1.PublicResourceSpec, error) {
	r := newResolver(annotations, cfg)

	siteRef, ok := r.siteRef(siteRefFallback)
	if !ok {
		return pangolinv1alpha1.PublicResourceSpec{}, fmt.Errorf("annotation %s/site-ref is required on HTTPRoute %s/%s", r.prefix, route.Namespace, route.Name)
	}

	var targetHostname string
	var targetPort int
	var targetMethod string

	if cfg.GatewayName != "" {
		// Gateway-based discovery: route through the gateway service.
		// GatewayTargetHostname can be set explicitly; if not, derive it from
		// GatewayName and GatewayNamespace (<name>.<namespace>.svc.cluster.local).
		targetHostname = cfg.GatewayTargetHostname
		if targetHostname == "" {
			if cfg.GatewayNamespace == "" {
				return pangolinv1alpha1.PublicResourceSpec{}, fmt.Errorf("autoDiscover.gatewayNamespace is required to derive gateway target hostname (HTTPRoute %s/%s)", route.Namespace, route.Name)
			}
			targetHostname = fmt.Sprintf("%s.%s.svc.cluster.local", cfg.GatewayName, cfg.GatewayNamespace)
		}
		targetPort = cfg.GatewayTargetPort
		if targetPort == 0 {
			targetPort = 443
		}
		targetMethod = cfg.GatewayTargetMethod
		if targetMethod == "" {
			targetMethod = methodHTTPS
		}
	} else {
		// Annotation-based discovery: route directly to the backendRef service.
		if len(route.Spec.Rules) == 0 || len(route.Spec.Rules[0].BackendRefs) == 0 {
			return pangolinv1alpha1.PublicResourceSpec{}, fmt.Errorf("HTTPRoute %s/%s has no backendRefs", route.Namespace, route.Name)
		}
		ref := route.Spec.Rules[0].BackendRefs[0].BackendObjectReference
		targetHostname, targetPort = resolveBackendRef(ref, route.Namespace, 80)
		targetMethod = methodHTTP
	}

	target := r.targetExtras(pangolinv1alpha1.PublicTargetSpec{
		Hostname: targetHostname,
		Port:     targetPort,
		Method:   r.method(targetMethod),
	})

	return r.buildHTTPSpec(siteRef, r.name(route.Name), hostname, r.enabled(), target), nil
}

func ResolveAllPorts(annotations map[string]string, prefix string, cfg *pangolinv1alpha1.AutoDiscoverSpec) bool {
	if v, ok := annotations[prefix+"/all-ports"]; ok {
		return isTruthy(v)
	}
	return cfg.AllPorts
}

func BuildAllPortSpecs(svc *corev1.Service, annotations map[string]string, cfg *pangolinv1alpha1.AutoDiscoverSpec, siteRef, clusterHostname string) map[string]pangolinv1alpha1.PublicResourceSpec {
	if len(svc.Spec.Ports) == 0 {
		return nil
	}
	r := newResolver(annotations, cfg)

	out := make(map[string]pangolinv1alpha1.PublicResourceSpec, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		portName := p.Name
		if portName == "" {
			portName = strconv.Itoa(int(p.Port))
		}
		proto := serviceProtocol(p.Protocol)
		target := pangolinv1alpha1.PublicTargetSpec{Hostname: clusterHostname, Port: int(p.Port)}
		key := ServiceResourceName(svc.Namespace, svc.Name, strconv.Itoa(int(p.Port)), proto)
		out[key] = r.buildTCPSpec(siteRef, fmt.Sprintf("%s-%s", svc.Name, portName), proto, int(p.Port), r.enabledOr(false), target)
	}
	return out
}

// BuildSinglePortSpec returns a PublicResourceSpec for the selected port of a Service.
// ok is false when no suitable port can be determined.
func BuildSinglePortSpec(svc *corev1.Service, annotations map[string]string, cfg *pangolinv1alpha1.AutoDiscoverSpec, siteRef, clusterHostname string) (string, pangolinv1alpha1.PublicResourceSpec, bool) {
	r := newResolver(annotations, cfg)
	fullDomain := strings.TrimSpace(r.get("full-domain"))

	selected, ok := selectPort(svc, annotations, r.prefix)
	if !ok {
		return "", pangolinv1alpha1.PublicResourceSpec{}, false
	}

	portName := selected.Name
	if portName == "" {
		portName = strconv.Itoa(int(selected.Port))
	}
	displayName := r.name(fmt.Sprintf("%s-%s", svc.Name, portName))

	if fullDomain != "" {
		extras := r.targetExtras(pangolinv1alpha1.PublicTargetSpec{})
		target := pangolinv1alpha1.PublicTargetSpec{
			Hostname:        clusterHostname,
			Port:            int(selected.Port),
			Method:          r.method(methodHTTP),
			Enabled:         extras.Enabled,
			Path:            extras.Path,
			PathMatchType:   extras.PathMatchType,
			RewritePath:     extras.RewritePath,
			RewritePathType: extras.RewritePathType,
			Priority:        extras.Priority,
		}
		resName := HostnameToResourceName(svc.Name, fullDomain)
		return resName, r.buildHTTPSpec(siteRef, displayName, fullDomain, r.enabledOr(false), target), true
	}

	proto := r.protocol(serviceProtocol(selected.Protocol))
	target := pangolinv1alpha1.PublicTargetSpec{Hostname: clusterHostname, Port: int(selected.Port)}
	resName := ServiceResourceName(svc.Namespace, svc.Name, strconv.Itoa(int(selected.Port)), proto)
	return resName, r.buildTCPSpec(siteRef, displayName, proto, int(selected.Port), r.enabledOr(false), target), true
}

func selectPort(svc *corev1.Service, annotations map[string]string, prefix string) (*corev1.ServicePort, bool) {
	if v, ok := annotations[prefix+"/port"]; ok {
		v = strings.TrimSpace(v)
		for i := range svc.Spec.Ports {
			p := &svc.Spec.Ports[i]
			if strconv.Itoa(int(p.Port)) == v || p.Name == v {
				return p, true
			}
		}
		return nil, false
	}
	if len(svc.Spec.Ports) == 1 {
		return &svc.Spec.Ports[0], true
	}
	for i := range svc.Spec.Ports {
		if svc.Spec.Ports[i].Name == methodHTTP {
			return &svc.Spec.Ports[i], true
		}
	}
	return nil, false
}

func serviceProtocol(p corev1.Protocol) string {
	if p == corev1.ProtocolUDP {
		return "udp"
	}
	return "tcp"
}

func TCPRouteReferencesGateway(route *gatewayv1alpha2.TCPRoute, gatewayName, gatewayNamespace string) bool {
	for _, parent := range route.Spec.ParentRefs {
		if parent.Name != gatewayv1.ObjectName(gatewayName) {
			continue
		}
		if gatewayNamespace != "" && parent.Namespace != nil && string(*parent.Namespace) != gatewayNamespace {
			continue
		}
		return true
	}
	return false
}

func BuildTCPRouteSpec(route *gatewayv1alpha2.TCPRoute, annotations map[string]string, cfg *pangolinv1alpha1.AutoDiscoverSpec, siteRefFallback string) (pangolinv1alpha1.PublicResourceSpec, error) {
	r := newResolver(annotations, cfg)

	siteRef, ok := r.siteRef(siteRefFallback)
	if !ok {
		return pangolinv1alpha1.PublicResourceSpec{}, fmt.Errorf("annotation %s/site-ref is required on TCPRoute %s/%s", r.prefix, route.Namespace, route.Name)
	}

	if len(route.Spec.Rules) == 0 || len(route.Spec.Rules[0].BackendRefs) == 0 {
		return pangolinv1alpha1.PublicResourceSpec{}, fmt.Errorf("TCPRoute %s/%s has no backendRefs", route.Namespace, route.Name)
	}

	ref := route.Spec.Rules[0].BackendRefs[0].BackendObjectReference
	targetHostname, targetPort := resolveBackendRef(ref, route.Namespace, 0)
	target := pangolinv1alpha1.PublicTargetSpec{Hostname: targetHostname, Port: targetPort}

	return r.buildTCPSpec(siteRef, r.name(route.Name), "tcp", r.proxyPort(targetPort), r.enabled(), target), nil
}

func EnsureTCPRouteResource(ctx context.Context, c client.Client, owner metav1.Object, routeName, namespace, resName string, spec pangolinv1alpha1.PublicResourceSpec) error {
	return ensureOwnedPublicResource(ctx, c, owner, "tcproute", routeName, namespace, resName, spec)
}

func EnsureHTTPRouteResource(ctx context.Context, c client.Client, owner metav1.Object, routeName, namespace, resName string, spec pangolinv1alpha1.PublicResourceSpec) error {
	return ensureOwnedPublicResource(ctx, c, owner, "httproute", routeName, namespace, resName, spec)
}

func EnsureServiceResource(ctx context.Context, c client.Client, owner metav1.Object, svcName, namespace, resName string, spec pangolinv1alpha1.PublicResourceSpec) error {
	return ensureOwnedPublicResource(ctx, c, owner, "service", svcName, namespace, resName, spec)
}

func ensureOwnedPublicResource(ctx context.Context, c client.Client, owner metav1.Object, ownerKind, ownerName, namespace, resName string, spec pangolinv1alpha1.PublicResourceSpec) error {
	existing := &pangolinv1alpha1.PublicResource{}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: resName}, existing)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		pr := &pangolinv1alpha1.PublicResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resName,
				Namespace: namespace,
				Labels: map[string]string{
					"pangolin.home-operations.com/owner-kind": ownerKind,
					"pangolin.home-operations.com/owner-name": ownerName,
					"pangolin.home-operations.com/site":       owner.GetName(),
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         "pangolin.home-operations.com/v1alpha1",
						Kind:               "NewtSite",
						Name:               owner.GetName(),
						UID:                owner.GetUID(),
						Controller:         new(true),
						BlockOwnerDeletion: new(true),
					},
				},
			},
			Spec: spec,
		}
		return c.Create(ctx, pr)
	}
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec = spec
	if existing.Labels == nil {
		existing.Labels = make(map[string]string, 3)
	}
	existing.Labels["pangolin.home-operations.com/owner-kind"] = ownerKind
	existing.Labels["pangolin.home-operations.com/owner-name"] = ownerName
	existing.Labels["pangolin.home-operations.com/site"] = owner.GetName()
	return c.Patch(ctx, existing, patch)
}
