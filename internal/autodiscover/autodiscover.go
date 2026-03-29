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
	prefix := annotationPrefix(cfg)

	siteRef, ok := annotations[prefix+"/site-ref"]
	if !ok || siteRef == "" {
		if siteRefFallback == "" {
			return pangolinv1alpha1.PublicResourceSpec{}, fmt.Errorf("annotation %s/site-ref is required on HTTPRoute %s/%s", prefix, route.Namespace, route.Name)
		}
		siteRef = siteRefFallback
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
		ref := route.Spec.Rules[0].BackendRefs[0]
		refNamespace := route.Namespace
		if ref.Namespace != nil && *ref.Namespace != "" {
			refNamespace = string(*ref.Namespace)
		}
		targetHostname = fmt.Sprintf("%s.%s.svc.cluster.local", ref.Name, refNamespace)
		targetPort = 80
		if ref.Port != nil {
			targetPort = int(*ref.Port)
		}
		targetMethod = methodHTTP
	}

	// Per-annotation overrides for method (applies in both modes).
	if v, ok := annotations[prefix+"/method"]; ok && (v == methodHTTP || v == methodHTTPS || v == methodH2C) {
		targetMethod = v
	}

	ssl := cfg.SSL
	if v, ok := annotations[prefix+"/ssl"]; ok {
		ssl = isTruthy(v)
	}

	displayName := route.Name
	if v, ok := annotations[prefix+"/name"]; ok && v != "" {
		displayName = v
	}

	tlsServerName := hostname
	if v := strings.TrimSpace(annotations[prefix+"/tls-server-name"]); v != "" {
		tlsServerName = v
	}

	target := buildTargetExtras(pangolinv1alpha1.PublicTargetSpec{
		Hostname: targetHostname,
		Port:     targetPort,
		Method:   targetMethod,
	}, annotations, prefix)

	spec := pangolinv1alpha1.PublicResourceSpec{
		SiteRef:       siteRef,
		Name:          displayName,
		Protocol:      methodHTTP,
		FullDomain:    hostname,
		Ssl:           ssl,
		HostHeader:    annotations[prefix+"/host-header"],
		TlsServerName: tlsServerName,
		Headers:       buildHeaders(annotations, prefix),
		Auth:          buildAuth(annotations, cfg),
		Maintenance:   buildMaintenance(annotations, prefix),
		Rules:         buildRules(annotations, prefix, cfg),
		Targets:       []pangolinv1alpha1.PublicTargetSpec{target},
	}
	if v, ok := annotations[prefix+"/enabled"]; ok {
		spec.Enabled = isTruthy(v)
	}
	return spec, nil
}

func ResolveAllPorts(annotations map[string]string, prefix string, cfg *pangolinv1alpha1.AutoDiscoverSpec) bool {
	if v, ok := annotations[prefix+"/all-ports"]; ok {
		return isTruthy(v)
	}
	return cfg.AllPorts
}

func BuildAllPortSpecs(svc *corev1.Service, annotations map[string]string, prefix, siteRef, clusterHostname string) map[string]pangolinv1alpha1.PublicResourceSpec {
	if len(svc.Spec.Ports) == 0 {
		return nil
	}
	out := make(map[string]pangolinv1alpha1.PublicResourceSpec, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		portName := p.Name
		if portName == "" {
			portName = strconv.Itoa(int(p.Port))
		}
		proto := serviceProtocol(p.Protocol)
		key := ServiceResourceName(svc.Namespace, svc.Name, strconv.Itoa(int(p.Port)), proto)
		out[key] = pangolinv1alpha1.PublicResourceSpec{
			SiteRef:   siteRef,
			Name:      fmt.Sprintf("%s-%s", svc.Name, portName),
			Protocol:  proto,
			ProxyPort: int(p.Port),
			Targets: []pangolinv1alpha1.PublicTargetSpec{
				{Hostname: clusterHostname, Port: int(p.Port)},
			},
		}
	}
	return out
}

// BuildSinglePortSpec returns a PublicResourceSpec for the selected port of a Service.
// ok is false when no suitable port can be determined.
func BuildSinglePortSpec(svc *corev1.Service, annotations map[string]string, prefix, siteRef, clusterHostname string, cfg *pangolinv1alpha1.AutoDiscoverSpec) (string, pangolinv1alpha1.PublicResourceSpec, bool) {
	fullDomain := strings.TrimSpace(annotations[prefix+"/full-domain"])

	selected, ok := selectPort(svc, annotations, prefix)
	if !ok {
		return "", pangolinv1alpha1.PublicResourceSpec{}, false
	}

	portName := selected.Name
	if portName == "" {
		portName = strconv.Itoa(int(selected.Port))
	}
	displayName := fmt.Sprintf("%s-%s", svc.Name, portName)
	if v, ok := annotations[prefix+"/name"]; ok && v != "" {
		displayName = v
	}

	if fullDomain != "" {
		method := methodHTTP
		if v, ok := annotations[prefix+"/method"]; ok {
			v = strings.ToLower(strings.TrimSpace(v))
			if v == methodHTTPS || v == methodH2C {
				method = v
			}
		}
		ssl := cfg.SSL
		if v, ok := annotations[prefix+"/ssl"]; ok {
			ssl = isTruthy(v)
		}
		tlsServerName := strings.TrimSpace(annotations[prefix+"/tls-server-name"])
		if tlsServerName == "" {
			tlsServerName = fullDomain
		}
		extras := buildTargetExtras(pangolinv1alpha1.PublicTargetSpec{}, annotations, prefix)
		target := pangolinv1alpha1.PublicTargetSpec{
			Hostname:        clusterHostname,
			Port:            int(selected.Port),
			Method:          method,
			Enabled:         extras.Enabled,
			Path:            extras.Path,
			PathMatchType:   extras.PathMatchType,
			RewritePath:     extras.RewritePath,
			RewritePathType: extras.RewritePathType,
			Priority:        extras.Priority,
		}
		resName := HostnameToResourceName(svc.Name, fullDomain)
		return resName, pangolinv1alpha1.PublicResourceSpec{
			SiteRef:       siteRef,
			Name:          displayName,
			Protocol:      methodHTTP,
			FullDomain:    fullDomain,
			Ssl:           ssl,
			HostHeader:    annotations[prefix+"/host-header"],
			TlsServerName: tlsServerName,
			Headers:       buildHeaders(annotations, prefix),
			Auth:          buildAuth(annotations, cfg),
			Maintenance:   buildMaintenance(annotations, prefix),
			Rules:         buildRules(annotations, prefix, cfg),
			Targets:       []pangolinv1alpha1.PublicTargetSpec{target},
		}, true
	}

	proto := serviceProtocol(selected.Protocol)
	if v, ok := annotations[prefix+"/protocol"]; ok {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "tcp" || v == "udp" {
			proto = v
		}
	}
	resName := ServiceResourceName(svc.Namespace, svc.Name, strconv.Itoa(int(selected.Port)), proto)
	return resName, pangolinv1alpha1.PublicResourceSpec{
		SiteRef:   siteRef,
		Name:      displayName,
		Protocol:  proto,
		ProxyPort: int(selected.Port),
		Targets: []pangolinv1alpha1.PublicTargetSpec{
			{Hostname: clusterHostname, Port: int(selected.Port)},
		},
	}, true
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
	return c.Patch(ctx, existing, patch)
}

func DeleteOwnedPublicResources(ctx context.Context, c client.Client, namespace, ownerKind, ownerName, siteName string) error {
	var list pangolinv1alpha1.PublicResourceList
	if err := c.List(ctx, &list, client.InNamespace(namespace), client.MatchingLabels{
		"pangolin.home-operations.com/owner-kind": ownerKind,
		"pangolin.home-operations.com/owner-name": ownerName,
		"pangolin.home-operations.com/site":       siteName,
	}); err != nil {
		return err
	}
	for i := range list.Items {
		if err := c.Delete(ctx, &list.Items[i]); err != nil && client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}
