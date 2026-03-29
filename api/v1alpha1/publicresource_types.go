package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PublicResourcePhase represents the current lifecycle phase of a PublicResource.
// +kubebuilder:validation:Enum=Pending;Creating;Ready;Deleting;Error
type PublicResourcePhase string

const (
	PublicResourcePhasePending  PublicResourcePhase = "Pending"
	PublicResourcePhaseCreating PublicResourcePhase = "Creating"
	PublicResourcePhaseReady    PublicResourcePhase = "Ready"
	PublicResourcePhaseDeleting PublicResourcePhase = "Deleting"
	PublicResourcePhaseError    PublicResourcePhase = "Error"
)

// PublicHeaderSpec defines a custom HTTP header.
type PublicHeaderSpec struct {
	// Name is the header name.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Value is the header value.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value"`
}

// PublicRuleSpec defines an access control rule.
type PublicRuleSpec struct {
	// Action is the rule action.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=ACCEPT;DROP;PASS
	Action string `json:"action"`

	// Match is the attribute to match against.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=ip;cidr;path;country
	Match string `json:"match"`

	// Value is the match value (IP, CIDR, path pattern, or 2-letter country code).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value"`

	// Priority controls evaluation order (lower = higher priority). Defaults to 100.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +optional
	Priority int `json:"priority,omitempty"`
}

// PublicAuthSpec defines the authentication configuration for a public resource.
// Sensitive values (pincode, password, basic-auth) must be stored in a Kubernetes
// Secret and referenced via AuthSecretRef.
type PublicAuthSpec struct {
	// SsoEnabled enables Pangolin SSO authentication.
	// +kubebuilder:default=false
	// +optional
	SsoEnabled bool `json:"ssoEnabled,omitempty"`

	// SsoRoles restricts access to users that have one of these Pangolin roles.
	// +optional
	SsoRoles []string `json:"ssoRoles,omitempty"`

	// SsoUsers restricts access to these Pangolin user email addresses (SSO).
	// +optional
	SsoUsers []string `json:"ssoUsers,omitempty"`

	// WhitelistUsers allows only these Pangolin user email addresses.
	// +optional
	WhitelistUsers []string `json:"whitelistUsers,omitempty"`

	// AutoLoginIdp is the Pangolin IdP ID for auto-login. 0 means not set.
	// +optional
	AutoLoginIdp int `json:"autoLoginIdp,omitempty"`

	// AuthSecretRef is the name of a Kubernetes Secret in the same namespace containing
	// sensitive auth values. Well-known keys: pincode, password, basic-auth-user, basic-auth-password.
	// +optional
	AuthSecretRef string `json:"authSecretRef,omitempty"`
}

// PublicMaintenanceSpec defines the maintenance page configuration.
type PublicMaintenanceSpec struct {
	// Enabled activates the maintenance page.
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`

	// Type is the maintenance type: forced or automatic.
	// +kubebuilder:validation:Enum=forced;automatic
	// +optional
	Type string `json:"type,omitempty"`

	// Title is the maintenance page title.
	// +optional
	Title string `json:"title,omitempty"`

	// Message is the maintenance page message.
	// +optional
	Message string `json:"message,omitempty"`

	// EstimatedTime is the estimated maintenance duration.
	// +optional
	EstimatedTime string `json:"estimatedTime,omitempty"`
}

// PublicHealthCheckSpec defines the health check configuration for a target.
type PublicHealthCheckSpec struct {
	// Hostname is the hostname to health-check.
	// +kubebuilder:validation:Required
	Hostname string `json:"hostname"`

	// Port is the port to health-check.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int `json:"port"`

	// Enabled controls whether health checking is active.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Path is the HTTP path to request.
	// +optional
	Path string `json:"path,omitempty"`

	// Scheme is the protocol scheme.
	// +optional
	Scheme string `json:"scheme,omitempty"`

	// Mode is the health check mode.
	// +optional
	Mode string `json:"mode,omitempty"`

	// Interval is the seconds between health checks.
	// +kubebuilder:validation:Minimum=1
	// +optional
	Interval int `json:"interval,omitempty"`

	// UnhealthyInterval is the seconds between checks when unhealthy.
	// +kubebuilder:validation:Minimum=1
	// +optional
	UnhealthyInterval int `json:"unhealthyInterval,omitempty"`

	// Timeout is the health check timeout in seconds.
	// +kubebuilder:validation:Minimum=1
	// +optional
	Timeout int `json:"timeout,omitempty"`

	// Headers are extra HTTP headers to include in the health check request.
	// +optional
	Headers []PublicHeaderSpec `json:"headers,omitempty"`

	// FollowRedirects controls whether to follow HTTP redirects.
	// +optional
	FollowRedirects *bool `json:"followRedirects,omitempty"`

	// Method is the HTTP method to use.
	// +optional
	Method string `json:"method,omitempty"`

	// Status is the expected HTTP status code.
	// +optional
	Status int `json:"status,omitempty"`
}

// PublicTargetSpec defines a backend target for a public resource.
type PublicTargetSpec struct {
	// Hostname is the backend hostname.
	// +kubebuilder:validation:Required
	Hostname string `json:"hostname"`

	// Port is the backend port.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int `json:"port"`

	// Method is the internal protocol to reach the backend (required for http resources).
	// +kubebuilder:validation:Enum=http;https;h2c
	// +optional
	Method string `json:"method,omitempty"`

	// Enabled controls whether this target is active.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Path is the URL path or pattern for this target.
	// +optional
	Path string `json:"path,omitempty"`

	// PathMatchType is the path matching type.
	// +kubebuilder:validation:Enum=prefix;exact;regex
	// +optional
	PathMatchType string `json:"pathMatchType,omitempty"`

	// RewritePath is the path to rewrite the request to.
	// +optional
	RewritePath string `json:"rewritePath,omitempty"`

	// RewritePathType is the rewrite match type.
	// +kubebuilder:validation:Enum=exact;prefix;regex;stripPrefix
	// +optional
	RewritePathType string `json:"rewritePathType,omitempty"`

	// Priority is the load-balancing priority (1–1000).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +optional
	Priority int `json:"priority,omitempty"`

	// Healthcheck is the health check configuration for this target.
	// +optional
	Healthcheck *PublicHealthCheckSpec `json:"healthcheck,omitempty"`
}

// PublicResourceSpec defines the desired state of a PublicResource.
type PublicResourceSpec struct {
	// SiteRef is the name of the NewtSite that owns this resource.
	// +kubebuilder:validation:Required
	SiteRef string `json:"siteRef"`

	// SiteNamespace is the namespace of the NewtSite.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	SiteNamespace string `json:"siteNamespace"`

	// Name is the display name of the resource in Pangolin.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Protocol is the resource protocol.
	// +kubebuilder:default="http"
	// +kubebuilder:validation:Enum=http;tcp;udp
	Protocol string `json:"protocol"`

	// FullDomain is the public domain to expose (required for http protocol).
	// +optional
	FullDomain string `json:"fullDomain,omitempty"`

	// ProxyPort is the Pangolin-side port (required for tcp/udp protocol).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	ProxyPort int `json:"proxyPort,omitempty"`

	// Enabled controls whether this resource is active.
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled"`

	// Ssl enables SSL on the Pangolin resource (http only).
	// +kubebuilder:default=true
	// +optional
	Ssl bool `json:"ssl,omitempty"`

	// HostHeader sets a custom Host header on the Pangolin resource.
	// +optional
	HostHeader string `json:"hostHeader,omitempty"`

	// TlsServerName overrides the SNI name for the backend TLS connection.
	// Defaults to FullDomain when empty.
	// +optional
	TlsServerName string `json:"tlsServerName,omitempty"`

	// Headers are extra HTTP headers to pass to Pangolin.
	// +optional
	Headers []PublicHeaderSpec `json:"headers,omitempty"`

	// Auth configures authentication for this resource (http only).
	// +optional
	Auth *PublicAuthSpec `json:"auth,omitempty"`

	// Maintenance configures the maintenance page.
	// +optional
	Maintenance *PublicMaintenanceSpec `json:"maintenance,omitempty"`

	// Rules are access control rules applied to this resource (http only).
	// +optional
	Rules []PublicRuleSpec `json:"rules,omitempty"`

	// Targets are the backend targets for this resource.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Targets []PublicTargetSpec `json:"targets"`
}

// PublicResourceStatus defines the observed state of a PublicResource.
type PublicResourceStatus struct {
	// Phase represents the current lifecycle phase of the resource.
	// +optional
	Phase PublicResourcePhase `json:"phase,omitempty"`

	// ResourceID is the numeric resource ID assigned by the Pangolin API.
	// +optional
	ResourceID int `json:"resourceId,omitempty"`

	// NiceID is the human-readable resource identifier from Pangolin.
	// +optional
	NiceID string `json:"niceId,omitempty"`

	// FullDomain is the fully-qualified public domain assigned to this resource.
	// +optional
	FullDomain string `json:"fullDomain,omitempty"`

	// TargetIDs are the numeric IDs of the backend targets registered in Pangolin.
	// +optional
	TargetIDs []int `json:"targetIds,omitempty"`

	// TargetsHash is a hash of spec.targets used to detect changes and trigger target reconciliation.
	// +optional
	TargetsHash string `json:"targetsHash,omitempty"`

	// RuleIDs are the numeric IDs of the access control rules registered in Pangolin.
	// +optional
	RuleIDs []int `json:"ruleIds,omitempty"`

	// RulesHash is a hash of spec.rules used to detect changes and trigger rule reconciliation.
	// +optional
	RulesHash string `json:"rulesHash,omitempty"`

	// ObservedGeneration reflects the .metadata.generation most recently reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions holds standard Kubernetes condition objects for this resource.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=pubr
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Site",type="string",JSONPath=".spec.siteRef"
// +kubebuilder:printcolumn:name="Domain",type="string",JSONPath=".status.fullDomain"
// +kubebuilder:printcolumn:name="Protocol",type="string",JSONPath=".spec.protocol"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// PublicResource is the Schema for Pangolin public resources.
type PublicResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PublicResourceSpec   `json:"spec,omitempty"`
	Status PublicResourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PublicResourceList contains a list of PublicResource.
type PublicResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PublicResource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PublicResource{}, &PublicResourceList{})
}
