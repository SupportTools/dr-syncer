package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// ClusterMappingPhase represents the current phase of the cluster mapping
// +kubebuilder:validation:Enum=Pending;Connecting;Connected;Failed
type ClusterMappingPhase string

const (
	// ClusterMappingPhasePending indicates the cluster mapping is pending
	ClusterMappingPhasePending ClusterMappingPhase = "Pending"
	// ClusterMappingPhaseConnecting indicates the cluster mapping is connecting
	ClusterMappingPhaseConnecting ClusterMappingPhase = "Connecting"
	// ClusterMappingPhaseConnected indicates the cluster mapping is connected
	ClusterMappingPhaseConnected ClusterMappingPhase = "Connected"
	// ClusterMappingPhaseFailed indicates the cluster mapping failed
	ClusterMappingPhaseFailed ClusterMappingPhase = "Failed"
)

// ClusterMappingSSHKeySecretRef extends SSHKeySecretRef with additional fields
type ClusterMappingSSHKeySecretRef struct {
	// Name is the name of the secret
	Name string `json:"name"`

	// Namespace is the namespace of the secret
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// PublicKeyKey is the key in the secret for the public key
	// +optional
	// +kubebuilder:default=id_rsa.pub
	PublicKeyKey string `json:"publicKeyKey,omitempty"`

	// PrivateKeyKey is the key in the secret for the private key
	// +optional
	// +kubebuilder:default=id_rsa
	PrivateKeyKey string `json:"privateKeyKey,omitempty"`
}

// ClusterMappingSpec defines the desired state of ClusterMapping
type ClusterMappingSpec struct {
	// SourceCluster is the name of the source cluster
	SourceCluster string `json:"sourceCluster"`

	// TargetCluster is the name of the target cluster
	TargetCluster string `json:"targetCluster"`

	// SSHKeySecretRef references a secret containing SSH keys for connectivity
	// +optional
	SSHKeySecretRef *ClusterMappingSSHKeySecretRef `json:"sshKeySecretRef,omitempty"`

	// VerifyConnectivity determines whether to verify SSH connectivity between agents
	// +optional
	// +kubebuilder:default=true
	VerifyConnectivity *bool `json:"verifyConnectivity,omitempty"`

	// ConnectivityTimeoutSeconds is the timeout in seconds for connectivity verification
	// +optional
	// +kubebuilder:default=60
	ConnectivityTimeoutSeconds *int32 `json:"connectivityTimeoutSeconds,omitempty"`
}

// AgentConnectionDetail provides connection details for a specific agent
type AgentConnectionDetail struct {
	// SourceNode is the name of the source node
	SourceNode string `json:"sourceNode"`

	// TargetNode is the name of the target node
	TargetNode string `json:"targetNode"`

	// Connected indicates whether the connection was successful
	Connected bool `json:"connected"`

	// Error provides error information if the connection failed
	// +optional
	Error string `json:"error,omitempty"`
}

// ConnectionStatus provides information about agent connectivity
type ConnectionStatus struct {
	// TotalSourceAgents is the total number of agents in the source cluster
	TotalSourceAgents int32 `json:"totalSourceAgents"`

	// TotalTargetAgents is the total number of agents in the target cluster
	TotalTargetAgents int32 `json:"totalTargetAgents"`

	// ConnectedAgents is the number of successfully connected agents
	ConnectedAgents int32 `json:"connectedAgents"`

	// ConnectionDetails provides detailed connection information for each agent
	// +optional
	ConnectionDetails []AgentConnectionDetail `json:"connectionDetails,omitempty"`
}

// ClusterMappingStatus defines the observed state of ClusterMapping
type ClusterMappingStatus struct {
	// Phase represents the current phase of the cluster mapping
	// +optional
	Phase ClusterMappingPhase `json:"phase,omitempty"`

	// Message provides additional status information
	// +optional
	Message string `json:"message,omitempty"`

	// LastVerified is when connectivity was last verified
	// +optional
	LastVerified *metav1.Time `json:"lastVerified,omitempty"`

	// ConnectionStatus provides information about agent connectivity
	// +optional
	ConnectionStatus *ConnectionStatus `json:"connectionStatus,omitempty"`

	// Conditions represent the latest available observations of the mapping's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastAttemptTime is when the last reconciliation attempt was made
	// +optional
	LastAttemptTime *metav1.Time `json:"lastAttemptTime,omitempty"`

	// ConsecutiveFailures tracks the number of consecutive reconciliation failures
	// +optional
	// +kubebuilder:default=0
	ConsecutiveFailures int `json:"consecutiveFailures,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Source Cluster",type="string",JSONPath=".spec.sourceCluster"
// +kubebuilder:printcolumn:name="Target Cluster",type="string",JSONPath=".spec.targetCluster"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Connected Agents",type="string",JSONPath=".status.connectionStatus.connectedAgents"
// +kubebuilder:printcolumn:name="Last Verified",type="date",JSONPath=".status.lastVerified"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=cm
type ClusterMapping struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterMappingSpec   `json:"spec"`
	Status ClusterMappingStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ClusterMappingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterMapping `json:"items"`
}

// DeepCopy creates a deep copy of ClusterMapping
func (c *ClusterMapping) DeepCopy() *ClusterMapping {
	if c == nil {
		return nil
	}
	out := new(ClusterMapping)
	c.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all properties of ClusterMapping into another instance
func (c *ClusterMapping) DeepCopyInto(out *ClusterMapping) {
	*out = *c
	out.TypeMeta = c.TypeMeta
	c.ObjectMeta.DeepCopyInto(&out.ObjectMeta)

	// Deep copy spec
	out.Spec = c.Spec
	if c.Spec.SSHKeySecretRef != nil {
		in, out := &c.Spec.SSHKeySecretRef, &out.Spec.SSHKeySecretRef
		*out = new(ClusterMappingSSHKeySecretRef)
		**out = **in
	}
	if c.Spec.VerifyConnectivity != nil {
		in, out := &c.Spec.VerifyConnectivity, &out.Spec.VerifyConnectivity
		*out = new(bool)
		**out = **in
	}
	if c.Spec.ConnectivityTimeoutSeconds != nil {
		in, out := &c.Spec.ConnectivityTimeoutSeconds, &out.Spec.ConnectivityTimeoutSeconds
		*out = new(int32)
		**out = **in
	}

	// Deep copy status
	if c.Status.LastVerified != nil {
		in, out := &c.Status.LastVerified, &out.Status.LastVerified
		*out = new(metav1.Time)
		**out = **in
	}
	if c.Status.ConnectionStatus != nil {
		in, out := &c.Status.ConnectionStatus, &out.Status.ConnectionStatus
		*out = new(ConnectionStatus)
		**out = **in
		if (*in).ConnectionDetails != nil {
			in, out := &(*in).ConnectionDetails, &(*out).ConnectionDetails
			*out = make([]AgentConnectionDetail, len(*in))
			copy(*out, *in)
		}
	}
	if c.Status.Conditions != nil {
		in, out := &c.Status.Conditions, &out.Status.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	// Deep copy new backoff-related fields
	if c.Status.LastAttemptTime != nil {
		in, out := &c.Status.LastAttemptTime, &out.Status.LastAttemptTime
		*out = new(metav1.Time)
		**out = **in
	}
}

// DeepCopyObject implements runtime.Object interface
func (c *ClusterMapping) DeepCopyObject() runtime.Object {
	if c == nil {
		return nil
	}
	return c.DeepCopy()
}

// DeepCopy creates a deep copy of ClusterMappingList
func (c *ClusterMappingList) DeepCopy() *ClusterMappingList {
	if c == nil {
		return nil
	}
	out := new(ClusterMappingList)
	c.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all properties of ClusterMappingList into another instance
func (c *ClusterMappingList) DeepCopyInto(out *ClusterMappingList) {
	*out = *c
	out.TypeMeta = c.TypeMeta
	out.ListMeta = c.ListMeta
	if c.Items != nil {
		out.Items = make([]ClusterMapping, len(c.Items))
		for i := range c.Items {
			c.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopyObject implements runtime.Object interface
func (c *ClusterMappingList) DeepCopyObject() runtime.Object {
	if c == nil {
		return nil
	}
	return c.DeepCopy()
}

func init() {
	SchemeBuilder.Register(&ClusterMapping{}, &ClusterMappingList{})
}
