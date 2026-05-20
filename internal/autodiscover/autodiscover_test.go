package autodiscover

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
)

func defaultCfg() *pangolinv1alpha1.AutoDiscoverSpec {
	return &pangolinv1alpha1.AutoDiscoverSpec{}
}

const (
	testNamespace   = "default"
	testHostname    = "app.example.com"
	testPathMatch   = "prefix"
	testProtocolTCP = protocolTCP
	testSiteRef     = "my-site"

	testRouteName     = "my-route"
	testTCPRouteName  = "my-tcp-route"
	testGatewayName   = "envoy-external"
	testGatewayNS     = "network"
	testGatewayHost   = "envoy-external.network.svc.cluster.local"
	testMyGateway     = "my-gateway"
	testRoleAdmin     = "admin"
	testPortMetrics   = "metrics"
	testSiteRefAnn    = "pangolin-operator/site-ref"
	testFullDomainAnn = "pangolin-operator/full-domain"
	testPortAnn       = "pangolin-operator/port"
	testEnabledAnn    = "pangolin-operator/enabled"
	testMethodAnn     = "pangolin-operator/method"
	testInvalidMethod = "grpc"
)

func newHTTPRoute(parentRefs []gatewayv1.ParentReference) *gatewayv1.HTTPRoute {
	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: testRouteName, Namespace: testNamespace},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs},
		},
	}
}

// newHTTPRouteWithBackendRef creates a minimal HTTPRoute with one rule + one backendRef.
// namespace may be empty (simulates no explicit namespace in the backendRef).
func newHTTPRouteWithBackendRef(svcName, namespace string, port *gatewayv1.PortNumber) *gatewayv1.HTTPRoute {
	var ns *gatewayv1.Namespace
	if namespace != "" {
		n := gatewayv1.Namespace(namespace)
		ns = &n
	}
	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: testRouteName, Namespace: testNamespace},
		Spec: gatewayv1.HTTPRouteSpec{
			Rules: []gatewayv1.HTTPRouteRule{
				{
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name:      gatewayv1.ObjectName(svcName),
									Namespace: ns,
									Port:      port,
								},
							},
						},
					},
				},
			},
		},
	}
}

func newService(ports []corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "my-svc", Namespace: testNamespace},
		Spec:       corev1.ServiceSpec{Ports: ports},
	}
}

func mapKeys(m map[string]pangolinv1alpha1.PublicResourceSpec) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestAnnotationPrefix_Default(t *testing.T) {
	if got := annotationPrefix(&pangolinv1alpha1.AutoDiscoverSpec{}); got != "pangolin-operator" {
		t.Errorf("expected default prefix, got %q", got)
	}
}

func TestAnnotationPrefix_Custom(t *testing.T) {
	if got := annotationPrefix(&pangolinv1alpha1.AutoDiscoverSpec{AnnotationPrefix: "my-prefix"}); got != "my-prefix" {
		t.Errorf("expected custom prefix, got %q", got)
	}
}

func TestIsOptOut(t *testing.T) {
	tests := []struct {
		ann  map[string]string
		want bool
	}{
		{map[string]string{testEnabledAnn: boolFalse}, true},
		{map[string]string{testEnabledAnn: "0"}, true},
		{map[string]string{testEnabledAnn: boolTrue}, false},
		{map[string]string{}, false},
	}
	for _, tt := range tests {
		if got := IsOptOut(tt.ann, "pangolin-operator"); got != tt.want {
			t.Errorf("IsOptOut(%v) = %v, want %v", tt.ann, got, tt.want)
		}
	}
}

func TestIsOptIn(t *testing.T) {
	tests := []struct {
		ann  map[string]string
		want bool
	}{
		{map[string]string{testEnabledAnn: boolTrue}, true},
		{map[string]string{testEnabledAnn: "1"}, true},
		{map[string]string{testEnabledAnn: boolFalse}, false},
		{map[string]string{}, false},
	}
	for _, tt := range tests {
		if got := IsOptIn(tt.ann, "pangolin-operator"); got != tt.want {
			t.Errorf("IsOptIn(%v) = %v, want %v", tt.ann, got, tt.want)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ", []string{"a", "b"}},
		{",,", nil},
	}
	for _, tt := range tests {
		got := splitCSV(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("splitCSV(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitCSV(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestBuildHeaders_Missing(t *testing.T) {
	if buildHeaders(map[string]string{}, "pangolin-operator") != nil {
		t.Error("expected nil for missing annotation")
	}
}

func TestBuildHeaders_InvalidJSON(t *testing.T) {
	ann := map[string]string{"pangolin-operator/headers": "not-json"}
	if buildHeaders(ann, "pangolin-operator") != nil {
		t.Error("expected nil for invalid JSON")
	}
}

func TestBuildHeaders_Valid(t *testing.T) {
	ann := map[string]string{
		"pangolin-operator/headers": `[{"name":"X-Foo","value":"bar"},{"name":"X-Baz","value":"qux"}]`,
	}
	got := buildHeaders(ann, "pangolin-operator")
	if len(got) != 2 || got[0].Name != "X-Foo" || got[0].Value != "bar" {
		t.Errorf("unexpected headers: %+v", got)
	}
}

func TestIsValidRule(t *testing.T) {
	tests := []struct {
		name string
		rule pangolinv1alpha1.PublicRuleSpec
		want bool
	}{
		{"valid DROP/country", pangolinv1alpha1.PublicRuleSpec{Action: actionDROP, Match: matchCountry, Value: "US"}, true},
		{"valid ACCEPT/ip", pangolinv1alpha1.PublicRuleSpec{Action: "ACCEPT", Match: "ip", Value: "1.2.3.4"}, true},
		{"valid PASS/cidr", pangolinv1alpha1.PublicRuleSpec{Action: "PASS", Match: "cidr", Value: "10.0.0.0/8"}, true},
		{"valid with priority", pangolinv1alpha1.PublicRuleSpec{Action: actionDROP, Match: matchCountry, Value: "US", Priority: 100}, true},
		{"invalid action", pangolinv1alpha1.PublicRuleSpec{Action: "block", Match: "ip", Value: "1.2.3.4"}, false},
		{"invalid match", pangolinv1alpha1.PublicRuleSpec{Action: actionDROP, Match: "asn", Value: "12345"}, false},
		{"missing value", pangolinv1alpha1.PublicRuleSpec{Action: actionDROP, Match: matchCountry}, false},
		{"priority zero (unset)", pangolinv1alpha1.PublicRuleSpec{Action: actionDROP, Match: "ip", Value: "1.1.1.1", Priority: 0}, true},
		{"priority out of range", pangolinv1alpha1.PublicRuleSpec{Action: actionDROP, Match: "ip", Value: "1.1.1.1", Priority: 1001}, false},
	}
	for _, tt := range tests {
		if got := isValidRule(tt.rule); got != tt.want {
			t.Errorf("%s: isValidRule() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestBuildRules_Empty(t *testing.T) {
	if buildRules(map[string]string{}, "pangolin-operator", defaultCfg()) != nil {
		t.Error("expected nil for no rules and no deny-countries")
	}
}

func TestBuildRules_FromAnnotation(t *testing.T) {
	ann := map[string]string{
		"pangolin-operator/rules": `[{"action":"DROP","match":"country","value":"CN"}]`,
	}
	got := buildRules(ann, "pangolin-operator", defaultCfg())
	if len(got) != 1 || got[0].Value != "CN" {
		t.Errorf("unexpected rules: %+v", got)
	}
}

func TestBuildRules_InvalidAnnotationRulesSkipped(t *testing.T) {
	ann := map[string]string{
		"pangolin-operator/rules": `[{"action":"bad","match":"ip","value":"1.1.1.1"}]`,
	}
	if buildRules(ann, "pangolin-operator", defaultCfg()) != nil {
		t.Error("expected nil when all annotation rules are invalid")
	}
}

func TestBuildRules_DenyCountriesFromCfg(t *testing.T) {
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{DenyCountries: "US, CN"}
	got := buildRules(map[string]string{}, "pangolin-operator", cfg)
	if len(got) != 2 || got[0].Value != "US" || got[1].Value != "CN" {
		t.Errorf("unexpected deny-country rules: %+v", got)
	}
}

func TestBuildMaintenance_AbsentOrDisabled(t *testing.T) {
	if buildMaintenance(map[string]string{}, "pangolin-operator") != nil {
		t.Error("expected nil when annotation absent")
	}
	ann := map[string]string{"pangolin-operator/maintenance-enabled": boolFalse}
	if buildMaintenance(ann, "pangolin-operator") != nil {
		t.Error("expected nil when explicitly disabled")
	}
}

func TestBuildMaintenance_Enabled(t *testing.T) {
	ann := map[string]string{
		"pangolin-operator/maintenance-enabled":        boolTrue,
		"pangolin-operator/maintenance-type":           "forced",
		"pangolin-operator/maintenance-title":          "Down for maintenance",
		"pangolin-operator/maintenance-message":        "Back soon",
		"pangolin-operator/maintenance-estimated-time": "30m",
	}
	got := buildMaintenance(ann, "pangolin-operator")
	if got == nil {
		t.Fatal("expected non-nil maintenance spec")
		return
	}
	if !got.Enabled || got.Type != "forced" || got.Title != "Down for maintenance" || got.EstimatedTime != "30m" {
		t.Errorf("unexpected maintenance spec: %+v", got)
	}
}

func TestBuildAuth_NilWhenEmpty(t *testing.T) {
	if buildAuth(map[string]string{}, DefaultAnnotationPrefix, defaultCfg()) != nil {
		t.Error("expected nil auth when no auth annotations")
	}
}

func TestBuildAuth_WhitelistFromAnnotation(t *testing.T) {
	ann := map[string]string{"pangolin-operator/auth-whitelist-users": "a@b.com, c@d.com"}
	got := buildAuth(ann, DefaultAnnotationPrefix, defaultCfg())
	if got == nil || len(got.WhitelistUsers) != 2 || got.WhitelistUsers[0] != "a@b.com" {
		t.Errorf("unexpected auth: %+v", got)
	}
}

func TestBuildAuth_SecretRef(t *testing.T) {
	ann := map[string]string{"pangolin-operator/auth-secret": "my-secret"}
	got := buildAuth(ann, DefaultAnnotationPrefix, defaultCfg())
	if got == nil || got.AuthSecretRef != "my-secret" {
		t.Errorf("unexpected auth: %+v", got)
	}
}

func TestBuildAuth_SSO_DefaultsFromCfg(t *testing.T) {
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{
		AuthSSORoles: testRoleAdmin,
		AuthSSOUsers: "owner@example.com",
		AuthSSOIDP:   5,
	}
	got := buildAuth(map[string]string{"pangolin-operator/auth-sso": boolTrue}, DefaultAnnotationPrefix, cfg)
	if got == nil || !got.SsoEnabled {
		t.Fatal("expected SSO enabled")
	}
	if len(got.SsoRoles) != 1 || got.SsoRoles[0] != testRoleAdmin {
		t.Errorf("unexpected SSO roles: %v", got.SsoRoles)
	}
	if got.AutoLoginIdp != 5 {
		t.Errorf("expected idp=5, got %d", got.AutoLoginIdp)
	}
}

func TestBuildAuth_SSO_AnnotationOverridesCfg(t *testing.T) {
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{AuthSSORoles: testRoleAdmin, AuthSSOIDP: 5}
	ann := map[string]string{
		"pangolin-operator/auth-sso":       boolTrue,
		"pangolin-operator/auth-sso-roles": "editor",
		"pangolin-operator/auth-sso-idp":   "7",
	}
	got := buildAuth(ann, DefaultAnnotationPrefix, cfg)
	if got == nil || len(got.SsoRoles) != 1 || got.SsoRoles[0] != "editor" || got.AutoLoginIdp != 7 {
		t.Errorf("unexpected auth: %+v", got)
	}
}

func TestBuildTargetExtras_BasePreserved(t *testing.T) {
	base := pangolinv1alpha1.PublicTargetSpec{Hostname: "svc", Port: 80}
	got := buildTargetExtras(base, map[string]string{}, "pangolin-operator")
	if got.Hostname != "svc" || got.Port != 80 || got.Path != "" || got.PathMatchType != "" || got.Priority != 0 {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestBuildTargetExtras_AllFields(t *testing.T) {
	ann := map[string]string{
		"pangolin-operator/target-path":          "/api",
		"pangolin-operator/target-path-match":    testPathMatch,
		"pangolin-operator/target-rewrite-path":  "/",
		"pangolin-operator/target-rewrite-match": rewriteStripPrefix,
		"pangolin-operator/target-priority":      "10",
		"pangolin-operator/target-enabled":       boolTrue,
	}
	got := buildTargetExtras(pangolinv1alpha1.PublicTargetSpec{}, ann, "pangolin-operator")
	if got.Path != "/api" || got.PathMatchType != testPathMatch || got.RewritePath != "/" || got.RewritePathType != rewriteStripPrefix {
		t.Errorf("unexpected path/rewrite fields: %+v", got)
	}
	if got.Priority != 10 {
		t.Errorf("unexpected priority: %+v", got)
	}
	if got.Enabled == nil || !*got.Enabled {
		t.Error("expected Enabled=true")
	}
}

func TestBuildTargetExtras_InvalidValuesIgnored(t *testing.T) {
	ann := map[string]string{
		"pangolin-operator/target-path-match": "wildcard",
		"pangolin-operator/target-priority":   "9999",
	}
	got := buildTargetExtras(pangolinv1alpha1.PublicTargetSpec{}, ann, "pangolin-operator")
	if got.PathMatchType != "" || got.Priority != 0 {
		t.Errorf("expected invalid values ignored: %+v", got)
	}
}

func TestRouteReferencesGateway(t *testing.T) {
	ns := gatewayv1.Namespace("infra")
	route := newHTTPRoute([]gatewayv1.ParentReference{
		{Name: testMyGateway, Namespace: &ns},
	})

	tests := []struct {
		gatewayName string
		gatewayNS   string
		want        bool
	}{
		{testMyGateway, "", true},
		{testMyGateway, "infra", true},
		{testMyGateway, "other-ns", false},
		{"other-gateway", "", false},
	}
	for _, tt := range tests {
		if got := RouteReferencesGateway(route, tt.gatewayName, tt.gatewayNS); got != tt.want {
			t.Errorf("RouteReferencesGateway(%q, %q) = %v, want %v", tt.gatewayName, tt.gatewayNS, got, tt.want)
		}
	}
}

func TestRouteReferencesGateway_NoParentRefs(t *testing.T) {
	route := newHTTPRoute(nil)
	if RouteReferencesGateway(route, testMyGateway, "") {
		t.Error("expected false for route with no parentRefs")
	}
}

func TestHostnameToResourceName(t *testing.T) {
	if got := HostnameToResourceName(testRouteName, testHostname); got != "my-route-app-example-com" {
		t.Errorf("unexpected resource name: %q", got)
	}
}

func TestHostnameToResourceName_Truncated(t *testing.T) {
	long := strings.Repeat("a", 200)
	if got := HostnameToResourceName("source", long); len(got) > 253 {
		t.Errorf("name not truncated: len=%d", len(got))
	}
}

func TestServiceResourceName(t *testing.T) {
	if got := ServiceResourceName(testNamespace, "my-svc", "80", testProtocolTCP); got != "default-my-svc-80-tcp" {
		t.Errorf("unexpected: %q", got)
	}
}

func TestBuildHTTPRouteSpec_MissingSiteRef(t *testing.T) {
	port := gatewayv1.PortNumber(3000)
	route := newHTTPRouteWithBackendRef("my-svc", testNamespace, &port)
	// Neither annotation nor fallback provided — must error.
	if _, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{}, defaultCfg(), ""); err == nil {
		t.Error("expected error when site-ref annotation is missing and no fallback")
	}
}

func TestBuildHTTPRouteSpec_NoBackendRefs(t *testing.T) {
	route := newHTTPRoute(nil)
	// Route has no rules/backendRefs — must error regardless of siteRef.
	if _, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{}, defaultCfg(), "homelab"); err == nil {
		t.Error("expected error when route has no backendRefs")
	}
}

func TestBuildHTTPRouteSpec_SiteRefFromFallback(t *testing.T) {
	port := gatewayv1.PortNumber(3000)
	route := newHTTPRouteWithBackendRef("forgejo-http", "forgejo", &port)
	// No site-ref annotation; fallback should be used.
	spec, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{}, defaultCfg(), "homelab")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.SiteRef != "homelab" {
		t.Errorf("expected SiteRef=homelab from fallback, got %q", spec.SiteRef)
	}
}

func TestBuildHTTPRouteSpec_BackendRefDerivedTarget(t *testing.T) {
	port := gatewayv1.PortNumber(3000)
	route := newHTTPRouteWithBackendRef("forgejo-http", "forgejo", &port)
	spec, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{}, defaultCfg(), "homelab")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spec.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(spec.Targets))
	}
	if spec.Targets[0].Hostname != "forgejo-http.forgejo.svc.cluster.local" {
		t.Errorf("unexpected hostname: %q", spec.Targets[0].Hostname)
	}
	if spec.Targets[0].Port != 3000 {
		t.Errorf("expected port 3000, got %d", spec.Targets[0].Port)
	}
	if spec.Targets[0].Method != methodHTTP {
		t.Errorf("expected method=http for cluster-internal target, got %q", spec.Targets[0].Method)
	}
}

func TestBuildHTTPRouteSpec_BackendRefNoNamespace(t *testing.T) {
	port := gatewayv1.PortNumber(8096)
	route := newHTTPRouteWithBackendRef("jellyfin", "", &port)
	// No namespace in backendRef — should fall back to route's own namespace.
	spec, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{}, defaultCfg(), "homelab")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Targets[0].Hostname != "jellyfin."+testNamespace+".svc.cluster.local" {
		t.Errorf("unexpected hostname: %q", spec.Targets[0].Hostname)
	}
}

func TestBuildHTTPRouteSpec_Defaults(t *testing.T) {
	port := gatewayv1.PortNumber(8080)
	route := newHTTPRouteWithBackendRef("my-svc", testNamespace, &port)
	spec, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{testSiteRefAnn: testSiteRef}, defaultCfg(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.SiteRef != testSiteRef || spec.FullDomain != testHostname || spec.Protocol != methodHTTP || spec.Name != testRouteName {
		t.Errorf("unexpected base fields: %+v", spec)
	}
	if len(spec.Targets) != 1 || spec.Targets[0].Port != 8080 || spec.Targets[0].Method != methodHTTP {
		t.Errorf("unexpected target defaults: %+v", spec.Targets)
	}
	if spec.TlsServerName != testHostname {
		t.Errorf("expected TlsServerName=hostname, got %q", spec.TlsServerName)
	}
	if !spec.Enabled {
		t.Error("HTTPRoute autodiscovery should default Enabled=true")
	}
}

func TestBuildHTTPRouteSpec_MethodAnnotationOverride(t *testing.T) {
	port := gatewayv1.PortNumber(8080)
	route := newHTTPRouteWithBackendRef("my-svc", testNamespace, &port)
	ann := map[string]string{testSiteRefAnn: testSiteRef, testMethodAnn: methodHTTPS}
	spec, err := BuildHTTPRouteSpec(route, testHostname, ann, defaultCfg(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Targets[0].Method != methodHTTPS {
		t.Errorf("expected method=https from annotation, got %q", spec.Targets[0].Method)
	}
}

func TestBuildHTTPRouteSpec_InvalidMethodFallsBackToDefault(t *testing.T) {
	port := gatewayv1.PortNumber(8080)
	route := newHTTPRouteWithBackendRef("my-svc", testNamespace, &port)
	ann := map[string]string{testSiteRefAnn: testSiteRef, testMethodAnn: testInvalidMethod}
	spec, err := BuildHTTPRouteSpec(route, testHostname, ann, defaultCfg(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Targets[0].Method != methodHTTP {
		t.Errorf("expected default method=http, got %q", spec.Targets[0].Method)
	}
}

func TestBuildHTTPRouteSpec_CustomName(t *testing.T) {
	port := gatewayv1.PortNumber(8080)
	route := newHTTPRouteWithBackendRef("my-svc", testNamespace, &port)
	ann := map[string]string{
		testSiteRefAnn:           testSiteRef,
		"pangolin-operator/name": "My App",
	}
	spec, err := BuildHTTPRouteSpec(route, testHostname, ann, defaultCfg(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Name != "My App" {
		t.Errorf("expected name=My App, got %q", spec.Name)
	}
}

func TestBuildHTTPRouteSpec_SSLFromCfg(t *testing.T) {
	port := gatewayv1.PortNumber(8080)
	route := newHTTPRouteWithBackendRef("my-svc", testNamespace, &port)
	spec, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{testSiteRefAnn: testSiteRef}, &pangolinv1alpha1.AutoDiscoverSpec{SSL: true}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !spec.Ssl {
		t.Error("expected Ssl=true from cfg")
	}
}

func TestBuildHTTPRouteSpec_SSLAnnotationOverridesCfg(t *testing.T) {
	port := gatewayv1.PortNumber(8080)
	route := newHTTPRouteWithBackendRef("my-svc", testNamespace, &port)
	ann := map[string]string{testSiteRefAnn: testSiteRef, "pangolin-operator/ssl": boolFalse}
	spec, err := BuildHTTPRouteSpec(route, testHostname, ann, &pangolinv1alpha1.AutoDiscoverSpec{SSL: true}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Ssl {
		t.Error("expected Ssl=false from annotation override")
	}
}

func TestBuildHTTPRouteSpec_EnabledAnnotation(t *testing.T) {
	port := gatewayv1.PortNumber(8080)
	route := newHTTPRouteWithBackendRef("my-svc", testNamespace, &port)
	ann := map[string]string{testSiteRefAnn: testSiteRef, testEnabledAnn: boolFalse}
	spec, err := BuildHTTPRouteSpec(route, testHostname, ann, defaultCfg(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Enabled {
		t.Error("expected Enabled=false")
	}
}

func TestBuildHTTPRouteSpec_CustomPrefix(t *testing.T) {
	port := gatewayv1.PortNumber(8080)
	route := newHTTPRouteWithBackendRef("my-svc", testNamespace, &port)
	spec, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{"myapp/site-ref": testSiteRef}, &pangolinv1alpha1.AutoDiscoverSpec{AnnotationPrefix: "myapp"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.SiteRef != testSiteRef {
		t.Errorf("expected SiteRef=my-site with custom prefix, got %q", spec.SiteRef)
	}
}

func TestBuildHTTPRouteSpec_GatewayMode_MissingNamespace(t *testing.T) {
	// GatewayName set but no GatewayNamespace and no explicit GatewayTargetHostname — must error.
	route := newHTTPRoute([]gatewayv1.ParentReference{{Name: testGatewayName}})
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{GatewayName: testGatewayName}
	if _, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{}, cfg, "homelab"); err == nil {
		t.Error("expected error when GatewayNamespace is empty and GatewayTargetHostname is not set")
	}
}

func TestBuildHTTPRouteSpec_GatewayMode_DerivedHostname(t *testing.T) {
	// When GatewayTargetHostname is empty, derive from GatewayName + GatewayNamespace.
	route := newHTTPRoute(nil)
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{
		GatewayName:      testGatewayName,
		GatewayNamespace: testGatewayNS,
	}
	spec, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{}, cfg, "homelab")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Targets[0].Hostname != testGatewayHost {
		t.Errorf("expected derived hostname, got %q", spec.Targets[0].Hostname)
	}
}

func TestBuildHTTPRouteSpec_GatewayMode_ExplicitHostnameOverridesDerived(t *testing.T) {
	// Explicit GatewayTargetHostname takes precedence over derived hostname.
	route := newHTTPRoute(nil)
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{
		GatewayName:           testGatewayName,
		GatewayNamespace:      testGatewayNS,
		GatewayTargetHostname: "custom-gateway.infra.svc.cluster.local",
	}
	spec, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{}, cfg, "homelab")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Targets[0].Hostname != "custom-gateway.infra.svc.cluster.local" {
		t.Errorf("expected explicit hostname, got %q", spec.Targets[0].Hostname)
	}
}

func TestBuildHTTPRouteSpec_GatewayMode_UsesGatewayTarget(t *testing.T) {
	// Gateway-mode: target must be the gateway service, not the backendRef.
	port := gatewayv1.PortNumber(3000)
	route := newHTTPRouteWithBackendRef("my-backend", testNamespace, &port)
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{
		GatewayName:           testGatewayName,
		GatewayTargetHostname: testGatewayHost,
		GatewayTargetPort:     443,
		GatewayTargetMethod:   methodHTTPS,
	}
	spec, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{}, cfg, "homelab")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spec.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(spec.Targets))
	}
	if spec.Targets[0].Hostname != testGatewayHost {
		t.Errorf("expected gateway hostname, got %q", spec.Targets[0].Hostname)
	}
	if spec.Targets[0].Port != 443 {
		t.Errorf("expected gateway port 443, got %d", spec.Targets[0].Port)
	}
	if spec.Targets[0].Method != methodHTTPS {
		t.Errorf("expected method=https, got %q", spec.Targets[0].Method)
	}
}

func TestBuildHTTPRouteSpec_GatewayMode_DefaultPortAndMethod(t *testing.T) {
	// When GatewayTargetPort and GatewayTargetMethod are zero/empty, defaults apply.
	route := newHTTPRoute(nil)
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{
		GatewayName:      testGatewayName,
		GatewayNamespace: testGatewayNS,
		// GatewayTargetPort and GatewayTargetMethod intentionally unset
	}
	spec, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{}, cfg, "homelab")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Targets[0].Port != 443 {
		t.Errorf("expected default port 443, got %d", spec.Targets[0].Port)
	}
	if spec.Targets[0].Method != methodHTTPS {
		t.Errorf("expected default method=https, got %q", spec.Targets[0].Method)
	}
}

func TestBuildHTTPRouteSpec_GatewayMode_MethodAnnotationOverride(t *testing.T) {
	// Per-annotation /method should override GatewayTargetMethod.
	route := newHTTPRoute(nil)
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{
		GatewayName:           testGatewayName,
		GatewayTargetHostname: testGatewayHost,
		GatewayTargetMethod:   methodHTTPS,
	}
	ann := map[string]string{testMethodAnn: methodHTTP}
	spec, err := BuildHTTPRouteSpec(route, testHostname, ann, cfg, "homelab")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Targets[0].Method != methodHTTP {
		t.Errorf("expected annotation to override method, got %q", spec.Targets[0].Method)
	}
}

func TestResolveAllPorts(t *testing.T) {
	tests := []struct {
		name string
		ann  map[string]string
		cfg  *pangolinv1alpha1.AutoDiscoverSpec
		want bool
	}{
		{"cfg true, no annotation", map[string]string{}, &pangolinv1alpha1.AutoDiscoverSpec{AllPorts: true}, true},
		{"cfg true, annotation false", map[string]string{"pangolin-operator/all-ports": boolFalse}, &pangolinv1alpha1.AutoDiscoverSpec{AllPorts: true}, false},
		{"cfg false, annotation true", map[string]string{"pangolin-operator/all-ports": boolTrue}, &pangolinv1alpha1.AutoDiscoverSpec{AllPorts: false}, true},
	}
	for _, tt := range tests {
		if got := ResolveAllPorts(tt.ann, "pangolin-operator", tt.cfg); got != tt.want {
			t.Errorf("%s: ResolveAllPorts() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestBuildAllPortSpecs_NoPorts(t *testing.T) {
	if BuildAllPortSpecs(newService(nil), map[string]string{}, defaultCfg(), testSiteRef, "host") != nil {
		t.Error("expected nil for service with no ports")
	}
}

func TestBuildAllPortSpecs_MultiplePorts(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: methodHTTP, Port: 80, Protocol: corev1.ProtocolTCP},
		{Name: testPortMetrics, Port: 9090, Protocol: corev1.ProtocolTCP},
	})
	out := BuildAllPortSpecs(svc, map[string]string{}, defaultCfg(), testSiteRef, "cluster.local")
	if len(out) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(out))
	}
	key := ServiceResourceName(testNamespace, "my-svc", "80", testProtocolTCP)
	spec, ok := out[key]
	if !ok {
		t.Fatalf("missing key %q", key)
	}
	if spec.ProxyPort != 80 || spec.SiteRef != testSiteRef {
		t.Errorf("unexpected spec: %+v", spec)
	}
}

func TestBuildAllPortSpecs_UnnamedPort(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Port: 5432, Protocol: corev1.ProtocolTCP},
	})
	out := BuildAllPortSpecs(svc, map[string]string{}, defaultCfg(), testSiteRef, "host")
	key := ServiceResourceName(testNamespace, "my-svc", "5432", testProtocolTCP)
	if spec, ok := out[key]; !ok || spec.Name != "my-svc-5432" {
		t.Errorf("unexpected result for unnamed port: %+v", out)
	}
}

func TestBuildAllPortSpecs_UDP(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: "dns", Port: 53, Protocol: corev1.ProtocolUDP},
	})
	out := BuildAllPortSpecs(svc, map[string]string{}, defaultCfg(), testSiteRef, "host")
	key := ServiceResourceName(testNamespace, "my-svc", "53", protocolUDP)
	if _, ok := out[key]; !ok {
		t.Errorf("expected UDP key %q, got: %v", key, mapKeys(out))
	}
}

func TestBuildSinglePortSpec_NoMatchingPort(t *testing.T) {
	svc := newService([]corev1.ServicePort{{Name: testInvalidMethod, Port: 9000}})
	_, _, ok := BuildSinglePortSpec(svc, map[string]string{testPortAnn: "8080"}, defaultCfg(), testSiteRef, "host")
	if ok {
		t.Error("expected ok=false when no port matches annotation")
	}
}

func TestBuildSinglePortSpec_SinglePort(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: testInvalidMethod, Port: 9000, Protocol: corev1.ProtocolTCP},
	})
	resName, spec, ok := BuildSinglePortSpec(svc, map[string]string{}, defaultCfg(), testSiteRef, "host")
	if !ok {
		t.Fatal("expected ok=true for single-port service")
	}
	if spec.Protocol != testProtocolTCP || spec.ProxyPort != 9000 {
		t.Errorf("unexpected spec: %+v", spec)
	}
	if resName != ServiceResourceName(testNamespace, "my-svc", "9000", testProtocolTCP) {
		t.Errorf("unexpected resource name: %q", resName)
	}
}

func TestBuildSinglePortSpec_SelectsHTTPPortByName(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: testPortMetrics, Port: 9090, Protocol: corev1.ProtocolTCP},
		{Name: methodHTTP, Port: 80, Protocol: corev1.ProtocolTCP},
	})
	_, spec, ok := BuildSinglePortSpec(svc, map[string]string{}, defaultCfg(), testSiteRef, "host")
	if !ok || spec.ProxyPort != 80 {
		t.Errorf("expected port 80 selected by name, got: ok=%v spec=%+v", ok, spec)
	}
}

func TestBuildSinglePortSpec_SelectsByName(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: methodHTTP, Port: 80, Protocol: corev1.ProtocolTCP},
		{Name: testPortMetrics, Port: 9090, Protocol: corev1.ProtocolTCP},
	})
	_, spec, ok := BuildSinglePortSpec(svc, map[string]string{testPortAnn: testPortMetrics}, defaultCfg(), testSiteRef, "host")
	if !ok || spec.ProxyPort != 9090 {
		t.Errorf("expected port 9090 selected by name, got: ok=%v spec=%+v", ok, spec)
	}
}

func TestBuildSinglePortSpec_SelectsByNumber(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: methodHTTP, Port: 80, Protocol: corev1.ProtocolTCP},
		{Name: testPortMetrics, Port: 9090, Protocol: corev1.ProtocolTCP},
	})
	_, spec, ok := BuildSinglePortSpec(svc, map[string]string{testPortAnn: "9090"}, defaultCfg(), testSiteRef, "host")
	if !ok || spec.ProxyPort != 9090 {
		t.Errorf("expected port 9090 selected by number, got: ok=%v spec=%+v", ok, spec)
	}
}

func TestBuildSinglePortSpec_FullDomain(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: methodHTTP, Port: 80, Protocol: corev1.ProtocolTCP},
	})
	ann := map[string]string{testFullDomainAnn: testHostname}
	resName, spec, ok := BuildSinglePortSpec(svc, ann, defaultCfg(), testSiteRef, "cluster.local")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if spec.FullDomain != testHostname || spec.Protocol != methodHTTP || spec.Targets[0].Hostname != "cluster.local" {
		t.Errorf("unexpected spec: %+v", spec)
	}
	if resName != HostnameToResourceName("my-svc", testHostname) {
		t.Errorf("unexpected resource name: %q", resName)
	}
}

func TestBuildSinglePortSpec_FullDomain_MethodHTTPS(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: methodHTTP, Port: 443, Protocol: corev1.ProtocolTCP},
	})
	ann := map[string]string{
		testFullDomainAnn: testHostname,
		testMethodAnn:     methodHTTPS,
	}
	_, spec, ok := BuildSinglePortSpec(svc, ann, defaultCfg(), testSiteRef, "cluster.local")
	if !ok || spec.Targets[0].Method != methodHTTPS {
		t.Errorf("expected method=https, got: ok=%v method=%q", ok, spec.Targets[0].Method)
	}
}

func TestBuildSinglePortSpec_AmbiguousMultiPort(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: testInvalidMethod, Port: 9000},
		{Name: testPortMetrics, Port: 9090},
	})
	_, _, ok := BuildSinglePortSpec(svc, map[string]string{}, defaultCfg(), testSiteRef, "host")
	if ok {
		t.Error("expected ok=false for ambiguous multi-port service with no selection annotation")
	}
}

func TestBuildSinglePortSpec_EnabledAnnotation(t *testing.T) {
	svc := newService([]corev1.ServicePort{{Name: methodHTTP, Port: 80, Protocol: corev1.ProtocolTCP}})
	ann := map[string]string{testEnabledAnn: boolFalse}
	_, spec, ok := BuildSinglePortSpec(svc, ann, defaultCfg(), testSiteRef, "host")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if spec.Enabled {
		t.Error("expected Enabled=false")
	}
}

func TestBuildSinglePortSpec_FullDomain_EnabledAnnotation(t *testing.T) {
	svc := newService([]corev1.ServicePort{{Name: methodHTTP, Port: 80, Protocol: corev1.ProtocolTCP}})
	ann := map[string]string{
		testFullDomainAnn: testHostname,
		testEnabledAnn:    boolFalse,
	}
	_, spec, ok := BuildSinglePortSpec(svc, ann, defaultCfg(), testSiteRef, "cluster.local")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if spec.Enabled {
		t.Error("expected Enabled=false for full-domain service")
	}
}

func TestBuildAllPortSpecs_EnabledAnnotation(t *testing.T) {
	svc := newService([]corev1.ServicePort{{Name: methodHTTP, Port: 80, Protocol: corev1.ProtocolTCP}})
	ann := map[string]string{testEnabledAnn: boolFalse}
	out := BuildAllPortSpecs(svc, ann, defaultCfg(), testSiteRef, "host")
	key := ServiceResourceName(testNamespace, "my-svc", "80", testProtocolTCP)
	spec, ok := out[key]
	if !ok {
		t.Fatalf("missing key %q", key)
	}
	if spec.Enabled {
		t.Error("expected Enabled=false")
	}
}

func TestBuildSinglePortSpec_DefaultEnabledTrue(t *testing.T) {
	svc := newService([]corev1.ServicePort{{Name: methodHTTP, Port: 80, Protocol: corev1.ProtocolTCP}})
	_, spec, ok := BuildSinglePortSpec(svc, map[string]string{}, defaultCfg(), testSiteRef, "host")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !spec.Enabled {
		t.Error("service autodiscovery should default Enabled=true")
	}
}

func TestBuildSinglePortSpec_FullDomain_DefaultEnabledTrue(t *testing.T) {
	svc := newService([]corev1.ServicePort{{Name: methodHTTP, Port: 80, Protocol: corev1.ProtocolTCP}})
	ann := map[string]string{testFullDomainAnn: testHostname}
	_, spec, ok := BuildSinglePortSpec(svc, ann, defaultCfg(), testSiteRef, "cluster.local")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !spec.Enabled {
		t.Error("service autodiscovery should default Enabled=true")
	}
}

func TestBuildSinglePortSpec_EnabledAnnotationTrue(t *testing.T) {
	svc := newService([]corev1.ServicePort{{Name: methodHTTP, Port: 80, Protocol: corev1.ProtocolTCP}})
	ann := map[string]string{testEnabledAnn: boolTrue}
	_, spec, ok := BuildSinglePortSpec(svc, ann, defaultCfg(), testSiteRef, "host")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !spec.Enabled {
		t.Error("expected Enabled=true when annotation is set")
	}
}

func TestBuildAllPortSpecs_DefaultEnabledTrue(t *testing.T) {
	svc := newService([]corev1.ServicePort{{Name: methodHTTP, Port: 80, Protocol: corev1.ProtocolTCP}})
	out := BuildAllPortSpecs(svc, map[string]string{}, defaultCfg(), testSiteRef, "host")
	key := ServiceResourceName(testNamespace, "my-svc", "80", testProtocolTCP)
	spec, ok := out[key]
	if !ok {
		t.Fatalf("missing key %q", key)
	}
	if !spec.Enabled {
		t.Error("service autodiscovery should default Enabled=true")
	}
}

func TestServiceProtocol(t *testing.T) {
	tests := []struct {
		proto corev1.Protocol
		want  string
	}{
		{corev1.ProtocolTCP, testProtocolTCP},
		{corev1.ProtocolUDP, protocolUDP},
		{corev1.ProtocolSCTP, testProtocolTCP},
	}
	for _, tt := range tests {
		if got := serviceProtocol(tt.proto); got != tt.want {
			t.Errorf("serviceProtocol(%v) = %q, want %q", tt.proto, got, tt.want)
		}
	}
}

func newTCPRouteWithBackendRef(svcName, namespace string, port *gatewayv1.PortNumber) *gatewayv1alpha2.TCPRoute {
	var ns *gatewayv1.Namespace
	if namespace != "" {
		n := gatewayv1.Namespace(namespace)
		ns = &n
	}
	return &gatewayv1alpha2.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: testTCPRouteName, Namespace: testNamespace},
		Spec: gatewayv1alpha2.TCPRouteSpec{
			Rules: []gatewayv1alpha2.TCPRouteRule{
				{
					BackendRefs: []gatewayv1alpha2.BackendRef{
						{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name:      gatewayv1.ObjectName(svcName),
								Namespace: ns,
								Port:      port,
							},
						},
					},
				},
			},
		},
	}
}

func newTCPRoute(parentRefs []gatewayv1.ParentReference) *gatewayv1alpha2.TCPRoute {
	return &gatewayv1alpha2.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: testTCPRouteName, Namespace: testNamespace},
		Spec: gatewayv1alpha2.TCPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs},
		},
	}
}

func TestTCPRouteReferencesGateway(t *testing.T) {
	ns := gatewayv1.Namespace("infra")
	route := newTCPRoute([]gatewayv1.ParentReference{
		{Name: testMyGateway, Namespace: &ns},
	})

	tests := []struct {
		gatewayName string
		gatewayNS   string
		want        bool
	}{
		{testMyGateway, "", true},
		{testMyGateway, "infra", true},
		{testMyGateway, "other-ns", false},
		{"other-gateway", "", false},
	}
	for _, tt := range tests {
		if got := TCPRouteReferencesGateway(route, tt.gatewayName, tt.gatewayNS); got != tt.want {
			t.Errorf("TCPRouteReferencesGateway(%q, %q) = %v, want %v", tt.gatewayName, tt.gatewayNS, got, tt.want)
		}
	}
}

func TestTCPRouteReferencesGateway_NoParentRefs(t *testing.T) {
	route := newTCPRoute(nil)
	if TCPRouteReferencesGateway(route, testMyGateway, "") {
		t.Error("expected false for route with no parentRefs")
	}
}

func TestBuildTCPRouteSpec_MissingSiteRef(t *testing.T) {
	port := gatewayv1.PortNumber(5432)
	route := newTCPRouteWithBackendRef("my-db", testNamespace, &port)
	if _, err := BuildTCPRouteSpec(route, map[string]string{}, defaultCfg(), ""); err == nil {
		t.Error("expected error when site-ref annotation is missing and no fallback")
	}
}

func TestBuildTCPRouteSpec_NoBackendRefs(t *testing.T) {
	route := newTCPRoute(nil)
	if _, err := BuildTCPRouteSpec(route, map[string]string{}, defaultCfg(), "homelab"); err == nil {
		t.Error("expected error when route has no backendRefs")
	}
}

func TestBuildTCPRouteSpec_SiteRefFromFallback(t *testing.T) {
	port := gatewayv1.PortNumber(5432)
	route := newTCPRouteWithBackendRef("postgres", "db", &port)
	spec, err := BuildTCPRouteSpec(route, map[string]string{}, defaultCfg(), "homelab")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.SiteRef != "homelab" {
		t.Errorf("expected SiteRef=homelab from fallback, got %q", spec.SiteRef)
	}
}

func TestBuildTCPRouteSpec_Defaults(t *testing.T) {
	port := gatewayv1.PortNumber(5432)
	route := newTCPRouteWithBackendRef("postgres", "db", &port)
	ann := map[string]string{testSiteRefAnn: testSiteRef}
	spec, err := BuildTCPRouteSpec(route, ann, defaultCfg(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.SiteRef != testSiteRef {
		t.Errorf("expected SiteRef=%q, got %q", testSiteRef, spec.SiteRef)
	}
	if spec.Protocol != testProtocolTCP {
		t.Errorf("expected Protocol=tcp, got %q", spec.Protocol)
	}
	if spec.ProxyPort != 5432 {
		t.Errorf("expected ProxyPort=5432, got %d", spec.ProxyPort)
	}
	if spec.Name != testTCPRouteName {
		t.Errorf("expected Name=my-tcp-route, got %q", spec.Name)
	}
	if len(spec.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(spec.Targets))
	}
	if spec.Targets[0].Hostname != "postgres.db.svc.cluster.local" {
		t.Errorf("unexpected target hostname: %q", spec.Targets[0].Hostname)
	}
	if spec.Targets[0].Port != 5432 {
		t.Errorf("expected target port 5432, got %d", spec.Targets[0].Port)
	}
	if !spec.Enabled {
		t.Error("TCPRoute autodiscovery should default Enabled=true")
	}
}

func TestBuildTCPRouteSpec_BackendRefNoNamespace(t *testing.T) {
	port := gatewayv1.PortNumber(3306)
	route := newTCPRouteWithBackendRef("mysql", "", &port)
	spec, err := BuildTCPRouteSpec(route, map[string]string{}, defaultCfg(), "homelab")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Targets[0].Hostname != "mysql."+testNamespace+".svc.cluster.local" {
		t.Errorf("unexpected hostname: %q", spec.Targets[0].Hostname)
	}
}

func TestBuildTCPRouteSpec_ProxyPortAnnotation(t *testing.T) {
	port := gatewayv1.PortNumber(5432)
	route := newTCPRouteWithBackendRef("postgres", "db", &port)
	ann := map[string]string{
		testSiteRefAnn:                 testSiteRef,
		"pangolin-operator/proxy-port": "15432",
	}
	spec, err := BuildTCPRouteSpec(route, ann, defaultCfg(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.ProxyPort != 15432 {
		t.Errorf("expected ProxyPort=15432 from annotation, got %d", spec.ProxyPort)
	}
	if spec.Targets[0].Port != 5432 {
		t.Errorf("expected target port to remain 5432, got %d", spec.Targets[0].Port)
	}
}

func TestBuildTCPRouteSpec_CustomName(t *testing.T) {
	port := gatewayv1.PortNumber(5432)
	route := newTCPRouteWithBackendRef("postgres", "db", &port)
	ann := map[string]string{
		testSiteRefAnn:           testSiteRef,
		"pangolin-operator/name": "My Database",
	}
	spec, err := BuildTCPRouteSpec(route, ann, defaultCfg(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Name != "My Database" {
		t.Errorf("expected name=My Database, got %q", spec.Name)
	}
}

func TestBuildTCPRouteSpec_EnabledAnnotation(t *testing.T) {
	port := gatewayv1.PortNumber(5432)
	route := newTCPRouteWithBackendRef("postgres", "db", &port)
	ann := map[string]string{
		testSiteRefAnn: testSiteRef,
		testEnabledAnn: boolFalse,
	}
	spec, err := BuildTCPRouteSpec(route, ann, defaultCfg(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Enabled {
		t.Error("expected Enabled=false")
	}
}

func TestBuildTCPRouteSpec_CustomPrefix(t *testing.T) {
	port := gatewayv1.PortNumber(5432)
	route := newTCPRouteWithBackendRef("postgres", "db", &port)
	ann := map[string]string{"myapp/site-ref": testSiteRef}
	spec, err := BuildTCPRouteSpec(route, ann, &pangolinv1alpha1.AutoDiscoverSpec{AnnotationPrefix: "myapp"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.SiteRef != testSiteRef {
		t.Errorf("expected SiteRef=%q with custom prefix, got %q", testSiteRef, spec.SiteRef)
	}
}
