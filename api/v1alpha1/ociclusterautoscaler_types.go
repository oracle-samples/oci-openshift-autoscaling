/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	// ConditionReady reports whether the autoscaler stack is ready for normal operation.
	ConditionReady = "Ready"
	// ConditionPolicyAccepted reports whether the requested autoscaler policy is accepted.
	ConditionPolicyAccepted = "PolicyAccepted"
	// ConditionProvidersReady reports whether CAPI and CAPOCI provider dependencies are ready.
	ConditionProvidersReady = "ProvidersReady"
	// ConditionAutoscalerReady reports whether the cluster-autoscaler deployment is ready.
	ConditionAutoscalerReady = "AutoscalerReady"
	// ConditionScalingResourcesReady reports whether managed scaling resources are reconciled.
	ConditionScalingResourcesReady = "ScalingResourcesReady"
	// ConditionCleanupSucceeded reports the latest cleanup result during deletion.
	ConditionCleanupSucceeded = "CleanupSucceeded"
)

// OCIClusterAutoscalerSpec defines the desired state of OCIClusterAutoscaler
// +kubebuilder:validation:XValidation:rule="!has(self.capi) || !has(self.capi.clusterName) || size(self.capi.clusterName) == 0 || ((!has(self.autoscaling.poolIdentifier) || size(self.autoscaling.poolIdentifier) == 0) ? size(self.capi.clusterName) <= 51 : size(self.capi.clusterName) + size(self.autoscaling.poolIdentifier) + 1 <= 51)",message="spec.capi.clusterName and spec.autoscaling.poolIdentifier must produce a node pool name no longer than 51 characters"
type OCIClusterAutoscalerSpec struct {
	// Autoscaling configuration
	Autoscaling AutoscalingConfig `json:"autoscaling"`

	// CAPI deployment configuration
	CAPI CAPIConfig `json:"capi,omitempty"`

	// ClusterAutoscaler deployment configuration
	ClusterAutoscaler ClusterAutoscalerConfig `json:"clusterAutoscaler,omitempty"`
}

// AutoscalingConfig contains optional autoscaling configuration
// +kubebuilder:validation:XValidation:rule="!has(self.minNodes) || !has(self.maxNodes) || self.minNodes <= self.maxNodes",message="minNodes must be less than or equal to maxNodes"
type AutoscalingConfig struct {
	// minNodes is the minimum number of nodes in the autoscaling group
	// +kubebuilder:validation:Minimum=0
	MinNodes *int32 `json:"minNodes,omitempty"`

	// maxNodes is the maximum number of nodes in the autoscaling group
	// +kubebuilder:validation:Minimum=0
	MaxNodes *int32 `json:"maxNodes,omitempty"`

	// nodeShape is the OCI compute shape for autoscaling nodes
	Shape string `json:"shape,omitempty"`

	// ShapeConfig contains flexible shape configuration
	ShapeConfig *ShapeConfig `json:"shapeConfig,omitempty"`

	// ImageID is the OCID of the custom RHCOS image for deploying new nodes during autoscaling
	ImageID string `json:"imageId,omitempty"`

	// PoolIdentifier is an optional lowercase identifier appended to the CAPI cluster name
	// when naming autoscaler node pool resources.
	// +kubebuilder:validation:MaxLength=5
	// +kubebuilder:validation:Pattern=`^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="poolIdentifier is immutable"
	PoolIdentifier string `json:"poolIdentifier,omitempty"`

	// EnableRDMA places autoscaled nodes in an OCI Compute Cluster and enables the
	// Compute HPC RDMA Authentication Oracle Cloud Agent plugin.
	EnableRDMA bool `json:"enableRdma,omitempty"`

	// RDMAComputeClusterID is the OCID of the OCI Compute Cluster used by
	// the RDMA worker pool.
	RDMAComputeClusterID string `json:"rdmaComputeClusterId,omitempty"`

	// RDMAFailureDomain is the CAPI failure-domain key for the availability
	// domain that contains the OCI Compute Cluster (for example, "1").
	RDMAFailureDomain string `json:"rdmaFailureDomain,omitempty"`
}

// ShapeConfig contains OCI flexible shape configuration
type ShapeConfig struct {
	// CPUs is the number of OCPUs
	// +kubebuilder:validation:Minimum=1
	CPUs *int32 `json:"cpus,omitempty"`

	// Memory is the amount of memory in GB
	// +kubebuilder:validation:Minimum=1
	Memory *int32 `json:"memory,omitempty"`
}

// CAPIConfig contains Cluster API configuration
type CAPIConfig struct {
	// ClusterName is the name of the CAPI cluster
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="clusterName is immutable"
	ClusterName string `json:"clusterName,omitempty"`
}

// ClusterAutoscalerConfig is the configuration for the deployment of the cluster-autoscaler
type ClusterAutoscalerConfig struct {
	// RepositoryURL is the URL for the helm chart of the cluster-autoscaler
	RepositoryURL string `json:"repositoryURL,omitempty"`

	// Name is the name of the cluster-autoscaler deployment
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="name is immutable"
	Name string `json:"name,omitempty"`

	// ServiceAccountName is the name of the service account the cluster-autoscaler deployment will use
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// CloudProvider is the cloud provider to use for the helm chart
	CloudProvider string `json:"cloudProvider,omitempty"`

	// CreateRBAC is whether or not to create the RBAC resources from the helm chart
	// for the cluster-autoscaler
	CreateRBAC *bool `json:"createRBAC,omitempty"`

	// CreateServiceAccount is whether or not to create the service account
	// from the helm chart for the cluster-autoscaler
	CreateServiceAccount *bool `json:"createServiceAccount,omitempty"`

	// Version is the helm chart version of the cluster-autoscaler to install
	Version string `json:"version,omitempty"`
}

// OCIClusterAutoscalerStatus defines the observed state of OCIClusterAutoscaler
type OCIClusterAutoscalerStatus struct {
	// Conditions represent the latest available observations of the autoscaler's current state.
	// Ready reflects the steady-state install/runtime path; CleanupSucceeded reports cleanup separately.
	// Stable condition types are Ready, PolicyAccepted, ProvidersReady, AutoscalerReady,
	// ScalingResourcesReady, and CleanupSucceeded.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase represents the current phase of the autoscaler
	Phase string `json:"phase,omitempty"`

	// CAPIInstalled indicates whether CAPI components are installed
	CAPIInstalled bool `json:"capiInstalled,omitempty"`

	// ClusterAutoscalerDeployed indicates whether cluster-autoscaler is deployed
	ClusterAutoscalerDeployed bool `json:"clusterAutoscalerDeployed,omitempty"`

	// ObservedGeneration is the last generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// OCIClusterAutoscaler is the Schema for the ociclusterautoscalers API.
// The controller manages singleton cluster-scoped resources, so only one
// OCIClusterAutoscaler may actively reconcile in a cluster.
type OCIClusterAutoscaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OCIClusterAutoscalerSpec   `json:"spec,omitempty"`
	Status OCIClusterAutoscalerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OCIClusterAutoscalerList contains a list of OCIClusterAutoscaler
type OCIClusterAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OCIClusterAutoscaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OCIClusterAutoscaler{}, &OCIClusterAutoscalerList{})
}
