/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package controllers

const (
	FinalizerName  = "ociclusterautoscaler.capi.openshift.io/finalizer"
	ManagedByLabel = "capi.openshift.io/managed-by"

	OCICAPIClusterName = "oci-capi-cluster"

	DefaultOperatorNamespace            = "oci-openshift-autoscaling-operator"
	DefaultCAPIProviderNamespace        = DefaultOperatorNamespace
	DefaultCAPOCIProviderNamespace      = DefaultOperatorNamespace
	DefaultManagedResourceNamespace     = DefaultOperatorNamespace
	DefaultAutoscalerNamespace          = DefaultOperatorNamespace
	DefaultAutoscalerDiscoveryNamespace = DefaultManagedResourceNamespace

	CAPIDeploymentName   = "capi-manager"
	CAPOCIDeploymentName = "capoci-controller-manager"

	CAPIServiceAccountName   = "capi-manager"
	CAPOCIServiceAccountName = "capoci-controller-manager"

	// These are the default names of the webhook services when using clusterctl
	CAPIWebhookServiceName   = "capi-webhook-service"
	CAPOCIWebhookServiceName = "capoci-webhook-service"

	AutoscalerRepoURL        = "https://kubernetes.github.io/autoscaler"
	AutoscalerChartName      = "oci-cluster-autoscaler/cluster-autoscaler"
	AutoscalerDeploymentName = "oci-cluster-autoscaler"
	AutoScalerCloudProvider  = "clusterapi"

	ReasonSingletonConflict = "SingletonConflict"
)
