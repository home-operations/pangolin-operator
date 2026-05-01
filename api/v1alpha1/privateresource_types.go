package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PrivateResourceSpec defines the desired state of a Pangolin private resource.
type PrivateResourceSpec struct {
	// SiteRef is the name of the NewtSite that owns this resource.
	// +kubebuilder:validation:Required
	SiteRef string `json:"siteRef"`

	// Name is the display name in Pangolin.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	Name string `json:"name"`

	// Mode is "host" (single IP/hostname), "cidr" (range), or "http"
	// (private HTTP resource reachable only via the Pangolin client).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=host;cidr;http
	Mode string `json:"mode"`

	// Destination is the IP, hostname, or CIDR to tunnel to. In http mode
	// this is the backend; pair with destinationPort.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Destination string `json:"destination"`

	// TcpPorts is the set of TCP ports to expose ("*" for all). Ignored when mode=http.
	// +kubebuilder:default="*"
	// +optional
	TcpPorts string `json:"tcpPorts,omitempty"`

	// UdpPorts is the set of UDP ports to expose ("*" for all). Ignored when mode=http.
	// +kubebuilder:default="*"
	// +optional
	UdpPorts string `json:"udpPorts,omitempty"`

	// FullDomain is the Pangolin-managed domain to expose (required when mode=http).
	// +optional
	FullDomain string `json:"fullDomain,omitempty"`

	// DestinationPort is the backend port (required when mode=http).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	DestinationPort int `json:"destinationPort,omitempty"`

	// Scheme is the backend protocol when mode=http.
	// +kubebuilder:validation:Enum=http;https
	// +kubebuilder:default=http
	// +optional
	Scheme string `json:"scheme,omitempty"`

	// Ssl enables a Pangolin TLS certificate on FullDomain when mode=http (default true).
	// +optional
	Ssl *bool `json:"ssl,omitempty"`

	// DisableIcmp disables ICMP (ping) tunnelling for this resource.
	// +kubebuilder:default=false
	// +optional
	DisableIcmp bool `json:"disableIcmp,omitempty"`

	// Alias is a fully-qualified domain name alias for the resource.
	// Required when mode is "host" and destination is a hostname (not an IP).
	// +optional
	Alias string `json:"alias,omitempty"`

	// RoleIds restricts access to OLM clients that have one of these Pangolin role IDs.
	// +optional
	RoleIds []int `json:"roleIds,omitempty"`

	// UserIds restricts access to these Pangolin user IDs.
	// +optional
	UserIds []string `json:"userIds,omitempty"`

	// ClientIds restricts access to these Pangolin client IDs.
	// +optional
	ClientIds []int `json:"clientIds,omitempty"`
}

// PrivateResourceStatus defines the observed state of a PrivateResource.
type PrivateResourceStatus struct {
	// Phase represents the current reconciliation phase.
	// +optional
	Phase PrivateResourcePhase `json:"phase,omitempty"`

	// SiteResourceID is the numeric resource ID returned by the Pangolin API.
	// +optional
	SiteResourceID int `json:"siteResourceId,omitempty"`

	// NiceID is the human-readable resource ID from Pangolin.
	// +optional
	NiceID string `json:"niceId,omitempty"`

	// FullDomain is the public domain assigned to this resource (mode=http).
	// +optional
	FullDomain string `json:"fullDomain,omitempty"`

	// ObservedGeneration reflects the last observed spec generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions standard Kubernetes conditions.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:validation:Enum=Pending;Creating;Ready;Deleting;Error
type PrivateResourcePhase string

const (
	PrivateResourcePhasePending  PrivateResourcePhase = "Pending"
	PrivateResourcePhaseCreating PrivateResourcePhase = "Creating"
	PrivateResourcePhaseReady    PrivateResourcePhase = "Ready"
	PrivateResourcePhaseDeleting PrivateResourcePhase = "Deleting"
	PrivateResourcePhaseError    PrivateResourcePhase = "Error"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=privr
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Mode",type="string",JSONPath=".spec.mode"
// +kubebuilder:printcolumn:name="Destination",type="string",JSONPath=".spec.destination"
// +kubebuilder:printcolumn:name="Domain",type="string",JSONPath=".status.fullDomain"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// PrivateResource is the Schema for Pangolin private resources.
// Each instance registers a host or CIDR range reachable through the referenced NewtSite tunnel,
// making it accessible to Pangolin OLM VPN clients.
type PrivateResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PrivateResourceSpec   `json:"spec,omitempty"`
	Status PrivateResourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PrivateResourceList contains a list of PrivateResource.
type PrivateResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PrivateResource `json:"items"`
}
