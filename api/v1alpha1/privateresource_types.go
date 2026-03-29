package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PrivateResourceSpec defines the desired state of a Pangolin private resource.
type PrivateResourceSpec struct {
	// SiteRef is the name of the NewtSite that owns this resource.
	// +kubebuilder:validation:Required
	SiteRef string `json:"siteRef"`

	// SiteNamespace is the namespace of the NewtSite.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	SiteNamespace string `json:"siteNamespace"`

	// Name is the display name in Pangolin.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	Name string `json:"name"`

	// Mode controls whether this resource tunnels to a single host, a CIDR range.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=host;cidr
	Mode string `json:"mode"`

	// Destination is the IP address, hostname, or CIDR range to tunnel to.
	// In cidr mode this must be a valid CIDR (e.g. 10.42.0.0/16).
	// In host mode this can be an IP address or a hostname (alias required for hostnames).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Destination string `json:"destination"`

	// TcpPorts is the set of TCP ports to expose. Defaults to "*" (all ports).
	// +kubebuilder:default="*"
	// +optional
	TcpPorts string `json:"tcpPorts,omitempty"`

	// UdpPorts is the set of UDP ports to expose. Defaults to "*" (all ports).
	// +kubebuilder:default="*"
	// +optional
	UdpPorts string `json:"udpPorts,omitempty"`

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

func init() {
	SchemeBuilder.Register(&PrivateResource{}, &PrivateResourceList{})
}
