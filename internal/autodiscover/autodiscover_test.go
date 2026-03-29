package autodiscover

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
)

func defaultCfg() *pangolinv1alpha1.AutoDiscoverSpec {
	return &pangolinv1alpha1.AutoDiscoverSpec{}
}

const (
	testNamespace   = "default"
	testHostname    = "app.example.com"
	testPathMatch   = "prefix"
	testProtocolTCP = "tcp"
	testSiteRef     = "my-site"
)

func newHTTPRoute(parentRefs []gatewayv1.ParentReference) *gatewayv1.HTTPRoute {
	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "my-route", Namespace: testNamespace},
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
		ObjectMeta: metav1.ObjectMeta{Name: "my-route", Namespace: testNamespace},
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
		{map[string]string{"pangolin-operator/enabled": "false"}, true},
		{map[string]string{"pangolin-operator/enabled": "0"}, true},
		{map[string]string{"pangolin-operator/enabled": "true"}, false},
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
		{map[string]string{"pangolin-operator/enabled": "true"}, true},
		{map[string]string{"pangolin-operator/enabled": "1"}, true},
		{map[string]string{"pangolin-operator/enabled": "false"}, false},
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
		{"valid DROP/country", pangolinv1alpha1.PublicRuleSpec{Action: "DROP", Match: "country", Value: "US"}, true},
		{"valid ACCEPT/ip", pangolinv1alpha1.PublicRuleSpec{Action: "ACCEPT", Match: "ip", Value: "1.2.3.4"}, true},
		{"valid PASS/cidr", pangolinv1alpha1.PublicRuleSpec{Action: "PASS", Match: "cidr", Value: "10.0.0.0/8"}, true},
		{"valid with priority", pangolinv1alpha1.PublicRuleSpec{Action: "DROP", Match: "country", Value: "US", Priority: 100}, true},
		{"invalid action", pangolinv1alpha1.PublicRuleSpec{Action: "block", Match: "ip", Value: "1.2.3.4"}, false},
		{"invalid match", pangolinv1alpha1.PublicRuleSpec{Action: "DROP", Match: "asn", Value: "12345"}, false},
		{"missing value", pangolinv1alpha1.PublicRuleSpec{Action: "DROP", Match: "country"}, false},
		{"priority zero (unset)", pangolinv1alpha1.PublicRuleSpec{Action: "DROP", Match: "ip", Value: "1.1.1.1", Priority: 0}, true},
		{"priority out of range", pangolinv1alpha1.PublicRuleSpec{Action: "DROP", Match: "ip", Value: "1.1.1.1", Priority: 1001}, false},
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
	ann := map[string]string{"pangolin-operator/maintenance-enabled": "false"}
	if buildMaintenance(ann, "pangolin-operator") != nil {
		t.Error("expected nil when explicitly disabled")
	}
}

func TestBuildMaintenance_Enabled(t *testing.T) {
	ann := map[string]string{
		"pangolin-operator/maintenance-enabled":        "true",
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
	if buildAuth(map[string]string{}, defaultCfg()) != nil {
		t.Error("expected nil auth when no auth annotations")
	}
}

func TestBuildAuth_WhitelistFromAnnotation(t *testing.T) {
	ann := map[string]string{"pangolin-operator/auth-whitelist-users": "a@b.com, c@d.com"}
	got := buildAuth(ann, defaultCfg())
	if got == nil || len(got.WhitelistUsers) != 2 || got.WhitelistUsers[0] != "a@b.com" {
		t.Errorf("unexpected auth: %+v", got)
	}
}

func TestBuildAuth_SecretRef(t *testing.T) {
	ann := map[string]string{"pangolin-operator/auth-secret": "my-secret"}
	got := buildAuth(ann, defaultCfg())
	if got == nil || got.AuthSecretRef != "my-secret" {
		t.Errorf("unexpected auth: %+v", got)
	}
}

func TestBuildAuth_SSO_DefaultsFromCfg(t *testing.T) {
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{
		AuthSSORoles: "admin",
		AuthSSOUsers: "owner@example.com",
		AuthSSOIDP:   5,
	}
	got := buildAuth(map[string]string{"pangolin-operator/auth-sso": "true"}, cfg)
	if got == nil || !got.SsoEnabled {
		t.Fatal("expected SSO enabled")
	}
	if len(got.SsoRoles) != 1 || got.SsoRoles[0] != "admin" {
		t.Errorf("unexpected SSO roles: %v", got.SsoRoles)
	}
	if got.AutoLoginIdp != 5 {
		t.Errorf("expected idp=5, got %d", got.AutoLoginIdp)
	}
}

func TestBuildAuth_SSO_AnnotationOverridesCfg(t *testing.T) {
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{AuthSSORoles: "admin", AuthSSOIDP: 5}
	ann := map[string]string{
		"pangolin-operator/auth-sso":       "true",
		"pangolin-operator/auth-sso-roles": "editor",
		"pangolin-operator/auth-sso-idp":   "7",
	}
	got := buildAuth(ann, cfg)
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
		"pangolin-operator/target-rewrite-match": "stripPrefix",
		"pangolin-operator/target-priority":      "10",
		"pangolin-operator/target-enabled":       "true",
	}
	got := buildTargetExtras(pangolinv1alpha1.PublicTargetSpec{}, ann, "pangolin-operator")
	if got.Path != "/api" || got.PathMatchType != testPathMatch || got.RewritePath != "/" || got.RewritePathType != "stripPrefix" {
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
		{Name: "my-gateway", Namespace: &ns},
	})

	tests := []struct {
		gatewayName string
		gatewayNS   string
		want        bool
	}{
		{"my-gateway", "", true},
		{"my-gateway", "infra", true},
		{"my-gateway", "other-ns", false},
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
	if RouteReferencesGateway(route, "my-gateway", "") {
		t.Error("expected false for route with no parentRefs")
	}
}

func TestHostnameToResourceName(t *testing.T) {
	if got := HostnameToResourceName("my-route", testHostname); got != "my-route-app-example-com" {
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
	spec, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{"pangolin-operator/site-ref": testSiteRef}, defaultCfg(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.SiteRef != testSiteRef || spec.FullDomain != testHostname || spec.Protocol != methodHTTP || spec.Name != "my-route" {
		t.Errorf("unexpected base fields: %+v", spec)
	}
	if len(spec.Targets) != 1 || spec.Targets[0].Port != 8080 || spec.Targets[0].Method != methodHTTP {
		t.Errorf("unexpected target defaults: %+v", spec.Targets)
	}
	if spec.TlsServerName != testHostname {
		t.Errorf("expected TlsServerName=hostname, got %q", spec.TlsServerName)
	}
}

func TestBuildHTTPRouteSpec_MethodAnnotationOverride(t *testing.T) {
	port := gatewayv1.PortNumber(8080)
	route := newHTTPRouteWithBackendRef("my-svc", testNamespace, &port)
	ann := map[string]string{"pangolin-operator/site-ref": testSiteRef, "pangolin-operator/method": "https"}
	spec, err := BuildHTTPRouteSpec(route, testHostname, ann, defaultCfg(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Targets[0].Method != "https" {
		t.Errorf("expected method=https from annotation, got %q", spec.Targets[0].Method)
	}
}

func TestBuildHTTPRouteSpec_InvalidMethodFallsBackToDefault(t *testing.T) {
	port := gatewayv1.PortNumber(8080)
	route := newHTTPRouteWithBackendRef("my-svc", testNamespace, &port)
	ann := map[string]string{"pangolin-operator/site-ref": testSiteRef, "pangolin-operator/method": "grpc"}
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
		"pangolin-operator/site-ref": testSiteRef,
		"pangolin-operator/name":     "My App",
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
	spec, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{"pangolin-operator/site-ref": testSiteRef}, &pangolinv1alpha1.AutoDiscoverSpec{SSL: true}, "")
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
	ann := map[string]string{"pangolin-operator/site-ref": testSiteRef, "pangolin-operator/ssl": "false"}
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
	ann := map[string]string{"pangolin-operator/site-ref": testSiteRef, "pangolin-operator/enabled": "false"}
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
	route := newHTTPRoute([]gatewayv1.ParentReference{{Name: "envoy-external"}})
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{GatewayName: "envoy-external"}
	if _, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{}, cfg, "homelab"); err == nil {
		t.Error("expected error when GatewayNamespace is empty and GatewayTargetHostname is not set")
	}
}

func TestBuildHTTPRouteSpec_GatewayMode_DerivedHostname(t *testing.T) {
	// When GatewayTargetHostname is empty, derive from GatewayName + GatewayNamespace.
	route := newHTTPRoute(nil)
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{
		GatewayName:      "envoy-external",
		GatewayNamespace: "network",
	}
	spec, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{}, cfg, "homelab")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Targets[0].Hostname != "envoy-external.network.svc.cluster.local" {
		t.Errorf("expected derived hostname, got %q", spec.Targets[0].Hostname)
	}
}

func TestBuildHTTPRouteSpec_GatewayMode_ExplicitHostnameOverridesDerived(t *testing.T) {
	// Explicit GatewayTargetHostname takes precedence over derived hostname.
	route := newHTTPRoute(nil)
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{
		GatewayName:           "envoy-external",
		GatewayNamespace:      "network",
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
		GatewayName:           "envoy-external",
		GatewayTargetHostname: "envoy-external.network.svc.cluster.local",
		GatewayTargetPort:     443,
		GatewayTargetMethod:   "https",
	}
	spec, err := BuildHTTPRouteSpec(route, testHostname, map[string]string{}, cfg, "homelab")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spec.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(spec.Targets))
	}
	if spec.Targets[0].Hostname != "envoy-external.network.svc.cluster.local" {
		t.Errorf("expected gateway hostname, got %q", spec.Targets[0].Hostname)
	}
	if spec.Targets[0].Port != 443 {
		t.Errorf("expected gateway port 443, got %d", spec.Targets[0].Port)
	}
	if spec.Targets[0].Method != "https" {
		t.Errorf("expected method=https, got %q", spec.Targets[0].Method)
	}
}

func TestBuildHTTPRouteSpec_GatewayMode_DefaultPortAndMethod(t *testing.T) {
	// When GatewayTargetPort and GatewayTargetMethod are zero/empty, defaults apply.
	route := newHTTPRoute(nil)
	cfg := &pangolinv1alpha1.AutoDiscoverSpec{
		GatewayName:      "envoy-external",
		GatewayNamespace: "network",
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
		GatewayName:           "envoy-external",
		GatewayTargetHostname: "envoy-external.network.svc.cluster.local",
		GatewayTargetMethod:   "https",
	}
	ann := map[string]string{"pangolin-operator/method": "http"}
	spec, err := BuildHTTPRouteSpec(route, testHostname, ann, cfg, "homelab")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Targets[0].Method != "http" {
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
		{"cfg true, annotation false", map[string]string{"pangolin-operator/all-ports": "false"}, &pangolinv1alpha1.AutoDiscoverSpec{AllPorts: true}, false},
		{"cfg false, annotation true", map[string]string{"pangolin-operator/all-ports": "true"}, &pangolinv1alpha1.AutoDiscoverSpec{AllPorts: false}, true},
	}
	for _, tt := range tests {
		if got := ResolveAllPorts(tt.ann, "pangolin-operator", tt.cfg); got != tt.want {
			t.Errorf("%s: ResolveAllPorts() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestBuildAllPortSpecs_NoPorts(t *testing.T) {
	if BuildAllPortSpecs(newService(nil), map[string]string{}, "pangolin-operator", testSiteRef, "host") != nil {
		t.Error("expected nil for service with no ports")
	}
}

func TestBuildAllPortSpecs_MultiplePorts(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
		{Name: "metrics", Port: 9090, Protocol: corev1.ProtocolTCP},
	})
	out := BuildAllPortSpecs(svc, map[string]string{}, "pangolin-operator", testSiteRef, "cluster.local")
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
	out := BuildAllPortSpecs(svc, map[string]string{}, "pangolin-operator", testSiteRef, "host")
	key := ServiceResourceName(testNamespace, "my-svc", "5432", testProtocolTCP)
	if spec, ok := out[key]; !ok || spec.Name != "my-svc-5432" {
		t.Errorf("unexpected result for unnamed port: %+v", out)
	}
}

func TestBuildAllPortSpecs_UDP(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: "dns", Port: 53, Protocol: corev1.ProtocolUDP},
	})
	out := BuildAllPortSpecs(svc, map[string]string{}, "pangolin-operator", testSiteRef, "host")
	key := ServiceResourceName(testNamespace, "my-svc", "53", "udp")
	if _, ok := out[key]; !ok {
		t.Errorf("expected UDP key %q, got: %v", key, mapKeys(out))
	}
}

func TestBuildSinglePortSpec_NoMatchingPort(t *testing.T) {
	svc := newService([]corev1.ServicePort{{Name: "grpc", Port: 9000}})
	_, _, ok := BuildSinglePortSpec(svc, map[string]string{"pangolin-operator/port": "8080"}, "pangolin-operator", testSiteRef, "host", defaultCfg())
	if ok {
		t.Error("expected ok=false when no port matches annotation")
	}
}

func TestBuildSinglePortSpec_SinglePort(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: "grpc", Port: 9000, Protocol: corev1.ProtocolTCP},
	})
	resName, spec, ok := BuildSinglePortSpec(svc, map[string]string{}, "pangolin-operator", testSiteRef, "host", defaultCfg())
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
		{Name: "metrics", Port: 9090, Protocol: corev1.ProtocolTCP},
		{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
	})
	_, spec, ok := BuildSinglePortSpec(svc, map[string]string{}, "pangolin-operator", testSiteRef, "host", defaultCfg())
	if !ok || spec.ProxyPort != 80 {
		t.Errorf("expected port 80 selected by name, got: ok=%v spec=%+v", ok, spec)
	}
}

func TestBuildSinglePortSpec_SelectsByName(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
		{Name: "metrics", Port: 9090, Protocol: corev1.ProtocolTCP},
	})
	_, spec, ok := BuildSinglePortSpec(svc, map[string]string{"pangolin-operator/port": "metrics"}, "pangolin-operator", testSiteRef, "host", defaultCfg())
	if !ok || spec.ProxyPort != 9090 {
		t.Errorf("expected port 9090 selected by name, got: ok=%v spec=%+v", ok, spec)
	}
}

func TestBuildSinglePortSpec_SelectsByNumber(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
		{Name: "metrics", Port: 9090, Protocol: corev1.ProtocolTCP},
	})
	_, spec, ok := BuildSinglePortSpec(svc, map[string]string{"pangolin-operator/port": "9090"}, "pangolin-operator", testSiteRef, "host", defaultCfg())
	if !ok || spec.ProxyPort != 9090 {
		t.Errorf("expected port 9090 selected by number, got: ok=%v spec=%+v", ok, spec)
	}
}

func TestBuildSinglePortSpec_FullDomain(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
	})
	ann := map[string]string{"pangolin-operator/full-domain": testHostname}
	resName, spec, ok := BuildSinglePortSpec(svc, ann, "pangolin-operator", testSiteRef, "cluster.local", defaultCfg())
	if !ok {
		t.Fatal("expected ok=true")
	}
	if spec.FullDomain != testHostname || spec.Protocol != "http" || spec.Targets[0].Hostname != "cluster.local" {
		t.Errorf("unexpected spec: %+v", spec)
	}
	if resName != HostnameToResourceName("my-svc", testHostname) {
		t.Errorf("unexpected resource name: %q", resName)
	}
}

func TestBuildSinglePortSpec_FullDomain_MethodHTTPS(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: "http", Port: 443, Protocol: corev1.ProtocolTCP},
	})
	ann := map[string]string{
		"pangolin-operator/full-domain": testHostname,
		"pangolin-operator/method":      methodHTTPS,
	}
	_, spec, ok := BuildSinglePortSpec(svc, ann, "pangolin-operator", testSiteRef, "cluster.local", defaultCfg())
	if !ok || spec.Targets[0].Method != methodHTTPS {
		t.Errorf("expected method=https, got: ok=%v method=%q", ok, spec.Targets[0].Method)
	}
}

func TestBuildSinglePortSpec_AmbiguousMultiPort(t *testing.T) {
	svc := newService([]corev1.ServicePort{
		{Name: "grpc", Port: 9000},
		{Name: "metrics", Port: 9090},
	})
	_, _, ok := BuildSinglePortSpec(svc, map[string]string{}, "pangolin-operator", testSiteRef, "host", defaultCfg())
	if ok {
		t.Error("expected ok=false for ambiguous multi-port service with no selection annotation")
	}
}

func TestServiceProtocol(t *testing.T) {
	tests := []struct {
		proto corev1.Protocol
		want  string
	}{
		{corev1.ProtocolTCP, "tcp"},
		{corev1.ProtocolUDP, "udp"},
		{corev1.ProtocolSCTP, "tcp"},
	}
	for _, tt := range tests {
		if got := serviceProtocol(tt.proto); got != tt.want {
			t.Errorf("serviceProtocol(%v) = %q, want %q", tt.proto, got, tt.want)
		}
	}
}
