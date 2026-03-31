package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewtSiteSpec defines the desired state of NewtSite
type NewtSiteSpec struct {
	// Name is the site display name in Pangolin
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	Name string `json:"name"`

	// Type is the site connection type. Use "newt" (default) for a tunnelled site
	// managed by the newt Deployment, or "local" to expose resources running on the
	// same host as the Pangolin server without deploying a tunnel.
	// +kubebuilder:validation:Enum=newt;local
	// +kubebuilder:default="newt"
	// +optional
	Type string `json:"type,omitempty"`

	// Newt configures the newt tunnel container. Ignored when type is "local".
	// +optional
	Newt NewtSpec `json:"newt,omitempty"`

	// AutoDiscover enables operator-native HTTPRoute/Service discovery for this site.
	// When set, the operator watches HTTPRoutes and Services annotated with
	// <prefix>/site-ref: <newtsiteName> and creates PublicResource CRs owned by this NewtSite.
	// +optional
	AutoDiscover *AutoDiscoverSpec `json:"autoDiscover,omitempty"`
}

// NewtSpec configures the newt tunnel container.
type NewtSpec struct {
	// Image is the newt container image
	// +kubebuilder:default="ghcr.io/fosrl/newt"
	// +optional
	Image string `json:"image,omitempty"`
	// Tag overrides the image tag (defaults to "latest")
	// +kubebuilder:default="latest"
	// +optional
	Tag string `json:"tag,omitempty"`
	// Replicas for the Deployment
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// LogLevel for the newt container
	// +kubebuilder:validation:Enum=DEBUG;INFO;WARN;ERROR
	// +kubebuilder:default="INFO"
	// +optional
	LogLevel string `json:"logLevel,omitempty"`
	// Mtu is the MTU for the WireGuard tunnel (default 1280).
	// +kubebuilder:default=1280
	// +optional
	Mtu int `json:"mtu,omitempty"`
	// PingInterval is the WireGuard keepalive interval (e.g. "60s").
	// Maps to the PING_INTERVAL environment variable.
	// +optional
	PingInterval string `json:"pingInterval,omitempty"`
	// PingTimeout is the WireGuard ping timeout (e.g. "5s").
	// Maps to the PING_TIMEOUT environment variable.
	// +optional
	PingTimeout string `json:"pingTimeout,omitempty"`
	// Interface is the WireGuard interface name inside the pod (default "newt").
	// Only needs to be set when a custom name is required.
	// Maps to the INTERFACE environment variable.
	// +kubebuilder:default="newt"
	// +optional
	Interface string `json:"interface,omitempty"`
	// DNS is the custom DNS server address pushed into the tunnel.
	// Maps to the DNS environment variable.
	// +optional
	DNS string `json:"dns,omitempty"`
	// AcceptClients allows the newt tunnel to accept incoming VPN client connections.
	// Maps to the ACCEPT_CLIENTS environment variable.
	// +optional
	AcceptClients bool `json:"acceptClients,omitempty"`
	// Resources for the newt container
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// UseNativeInterface enables the WireGuard kernel interface instead of the
	// userspace implementation. When true the container runs as root with
	// NET_ADMIN and SYS_MODULE capabilities (privileged). Only use this when
	// the node kernel has the WireGuard module available.
	// +optional
	UseNativeInterface bool `json:"useNativeInterface,omitempty"`
	// HostNetwork grants the pod access to the host network namespace.
	// Only meaningful when UseNativeInterface is true.
	// +optional
	HostNetwork bool `json:"hostNetwork,omitempty"`
	// HostPID grants the pod access to the host PID namespace.
	// Only meaningful when UseNativeInterface is true.
	// +optional
	HostPID bool `json:"hostPID,omitempty"`
	// PodAnnotations are extra annotations added to the pod template.
	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`
	// NodeSelector constrains the pod to nodes matching the given labels.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Tolerations for the pod.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Affinity for the pod.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// TopologySpreadConstraints for the pod.
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	// PodSecurityContext overrides the pod-level security context.
	// When unset the operator applies sensible defaults (non-root, seccomp RuntimeDefault).
	// Setting this field replaces the operator defaults entirely.
	// +optional
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`
	// SecurityContext overrides the container-level security context.
	// When unset the operator applies sensible defaults.
	// Setting this field replaces the operator defaults entirely.
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
	// ExtraEnv injects additional environment variables into the newt container.
	// +optional
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`
	// ExtraVolumes adds extra volumes to the pod.
	// +optional
	ExtraVolumes []corev1.Volume `json:"extraVolumes,omitempty"`
	// ExtraVolumeMounts adds extra volume mounts to the newt container.
	// +optional
	ExtraVolumeMounts []corev1.VolumeMount `json:"extraVolumeMounts,omitempty"`
	// InitContainers adds init containers to the pod (run before the newt container).
	// +optional
	InitContainers []corev1.Container `json:"initContainers,omitempty"`
	// ExtraContainers adds sidecar containers alongside the newt container.
	// +optional
	ExtraContainers []corev1.Container `json:"extraContainers,omitempty"`
	// Metrics configures Prometheus metrics exposure for the newt container.
	// +optional
	Metrics *NewtMetricsSpec `json:"metrics,omitempty"`
}

// NewtMetricsSpec configures Prometheus metrics for the newt container.
type NewtMetricsSpec struct {
	// Port is the container port on which newt exposes metrics (default 9090).
	// Maps to the NEWT_ADMIN_ADDR environment variable (0.0.0.0:<Port>).
	// +kubebuilder:default=9090
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int `json:"port,omitempty"`
	// AdminAddr overrides the full admin address (host:port) bound by newt.
	// When empty, defaults to "0.0.0.0:<Port>".
	// Maps directly to the NEWT_ADMIN_ADDR environment variable.
	// +optional
	AdminAddr string `json:"adminAddr,omitempty"`
}

// AutoDiscoverSpec configures operator-native HTTPRoute/Service auto-discovery for a NewtSite.
// All fields are optional; missing fields fall back to built-in defaults.
type AutoDiscoverSpec struct {
	// AnnotationPrefix is the annotation prefix used for discovery annotations.
	// Default: "pangolin-operator"
	// +kubebuilder:default="pangolin-operator"
	// +optional
	AnnotationPrefix string `json:"annotationPrefix,omitempty"`

	// GatewayName filters HTTPRoutes by spec.parentRefs[].name.
	// When empty, all HTTPRoutes carrying the site-ref annotation are processed.
	// +optional
	GatewayName string `json:"gatewayName,omitempty"`

	// GatewayNamespace additionally filters HTTPRoutes by parentRef namespace.
	// When empty, any namespace matches.
	// +optional
	GatewayNamespace string `json:"gatewayNamespace,omitempty"`

	// GatewayTargetHostname is the cluster-internal hostname of the gateway service
	// that HTTPRoute traffic should be routed through (e.g. "envoy-external.network.svc.cluster.local").
	// Required when GatewayName is set.
	// +optional
	GatewayTargetHostname string `json:"gatewayTargetHostname,omitempty"`

	// GatewayTargetPort is the port on the gateway service to route traffic to.
	// Defaults to 443.
	// +kubebuilder:default=443
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	GatewayTargetPort int `json:"gatewayTargetPort,omitempty"`

	// GatewayTargetMethod is the internal protocol used to reach the gateway service (http|https|h2c).
	// Defaults to "https".
	// +kubebuilder:validation:Enum=http;https;h2c
	// +kubebuilder:default="https"
	// +optional
	GatewayTargetMethod string `json:"gatewayTargetMethod,omitempty"`

	// EnableRouteDiscovery enables HTTPRoute discovery for this site.
	// When false (default), HTTPRoutes are not auto-discovered even if annotated.
	// Set to true to enable event-driven and periodic HTTPRoute scanning.
	// +kubebuilder:default=false
	// +optional
	EnableRouteDiscovery bool `json:"enableRouteDiscovery,omitempty"`

	// EnableServiceDiscovery enables Service discovery for this site.
	// When false (default), Services are not auto-discovered even if annotated.
	// Set to true to enable event-driven and periodic Service scanning.
	// +kubebuilder:default=false
	// +optional
	EnableServiceDiscovery bool `json:"enableServiceDiscovery,omitempty"`

	// EnableTCPRouteDiscovery enables Gateway API TCPRoute discovery for this site.
	// When false (default), TCPRoutes are not auto-discovered even if annotated.
	// Set to true to enable event-driven and periodic TCPRoute scanning.
	// +kubebuilder:default=false
	// +optional
	EnableTCPRouteDiscovery bool `json:"enableTCPRouteDiscovery,omitempty"`

	// AllPorts exposes all TCP/UDP ports of a Service as individual PublicResources.
	// +optional
	AllPorts bool `json:"allPorts,omitempty"`

	// SSL is the default SSL setting for HTTP resources created from annotations.
	// +kubebuilder:default=true
	// +optional
	SSL bool `json:"ssl,omitempty"`

	// DenyCountries is a comma-separated list of country codes to deny by default on all resources.
	// +optional
	DenyCountries string `json:"denyCountries,omitempty"`

	// AuthSSORoles is the default comma-separated Pangolin roles for SSO-enabled resources.
	// +optional
	AuthSSORoles string `json:"authSsoRoles,omitempty"`

	// AuthSSOUsers is the default comma-separated user emails for SSO-enabled resources.
	// +optional
	AuthSSOUsers string `json:"authSsoUsers,omitempty"`

	// AuthSSOIDP is the default Pangolin IdP ID for auto-login-idp (0 = not set).
	// +optional
	AuthSSOIDP int `json:"authSsoIdp,omitempty"`

	// AuthWhitelistUsers is the default comma-separated user emails for whitelist-users.
	// +optional
	AuthWhitelistUsers string `json:"authWhitelistUsers,omitempty"`
}

// NewtSiteStatus defines the observed state of NewtSite.
type NewtSiteStatus struct {
	// Phase represents the current reconciliation phase
	// +optional
	Phase NewtSitePhase `json:"phase,omitempty"`
	// SiteID is the numeric site ID returned by Pangolin API
	// +optional
	SiteID int `json:"siteId,omitempty"`
	// NiceID is the human-readable site ID from Pangolin
	// +optional
	NiceID string `json:"niceId,omitempty"`
	// NewtSecretName is the auto-created Secret name containing PANGOLIN_ENDPOINT/NEWT_ID/NEWT_SECRET
	// +optional
	NewtSecretName string `json:"newtSecretName,omitempty"`
	// Online tracks the tunnel connection status
	// +optional
	Online bool `json:"online,omitempty"`
	// ObservedGeneration reflects the last observed spec generation
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions standard Kubernetes conditions
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:validation:Enum=Pending;Creating;Ready;Deleting;Error
type NewtSitePhase string

const (
	NewtSitePhasePending  NewtSitePhase = "Pending"
	NewtSitePhaseCreating NewtSitePhase = "Creating"
	NewtSitePhaseReady    NewtSitePhase = "Ready"
	NewtSitePhaseDeleting NewtSitePhase = "Deleting"
	NewtSitePhaseError    NewtSitePhase = "Error"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=nsite
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Site ID",type="string",JSONPath=".status.niceId"
// +kubebuilder:printcolumn:name="Online",type="boolean",JSONPath=".status.online"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// NewtSite is the Schema for the newtsites API.
// It provisions a Pangolin site and manages the associated newt tunnel Deployment.
type NewtSite struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NewtSiteSpec   `json:"spec,omitempty"`
	Status NewtSiteStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NewtSiteList contains a list of NewtSite.
type NewtSiteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NewtSite `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NewtSite{}, &NewtSiteList{})
}
