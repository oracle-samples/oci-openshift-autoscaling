/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package autoscaler

import (
	"fmt"

	capiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
)

type AutoscalerDeploymentValues struct {
	CloudProvider          string
	Name                   string
	Namespace              string
	AutoDiscoveryNamespace string
	ServiceAccountName     string
	CreateRBAC             bool
	CreateServiceAccount   bool
	RepositoryURL          string
	Chart                  string
	Version                string
	ScanInterval           string
}

var valuesFmt = `
cloudProvider: %s
fullnameOverride: %s
autoDiscovery:
  namespace: %s
extraArgs:
  scan-interval: %s
rbac:
  create: %t
  serviceAccount:
    create: %t
    name: %s`

const (
	defaultScanInterval = "60s"
)

// GetValuesString gets the value overrides as a string for the autoscaler helm chart deployment
func GetValuesString(values *AutoscalerDeploymentValues) string {
	scanInterval := values.ScanInterval
	if scanInterval == "" {
		scanInterval = defaultScanInterval
	}
	autoDiscoveryNamespace := values.AutoDiscoveryNamespace
	if autoDiscoveryNamespace == "" {
		autoDiscoveryNamespace = values.Namespace
	}

	return fmt.Sprintf(valuesFmt,
		values.CloudProvider,
		values.Name,
		autoDiscoveryNamespace,
		scanInterval,
		values.CreateRBAC,
		values.CreateServiceAccount,
		values.ServiceAccountName,
	)
}

// GetAutoscalerDeploymentValues gets the autoscaler deployment values from the instance if it's set
func GetAutoscalerDeploymentValues(originalValues AutoscalerDeploymentValues, instance *capiv1alpha1.OCIClusterAutoscaler) AutoscalerDeploymentValues {
	if instance.Spec.ClusterAutoscaler.CloudProvider != "" {
		originalValues.CloudProvider = instance.Spec.ClusterAutoscaler.CloudProvider
	}
	if instance.Spec.ClusterAutoscaler.Name != "" {
		originalValues.Name = instance.Spec.ClusterAutoscaler.Name
	}
	if instance.Spec.ClusterAutoscaler.ServiceAccountName != "" {
		originalValues.ServiceAccountName = instance.Spec.ClusterAutoscaler.ServiceAccountName
	}
	if instance.Spec.ClusterAutoscaler.CreateRBAC != nil {
		originalValues.CreateRBAC = *instance.Spec.ClusterAutoscaler.CreateRBAC
	}
	if instance.Spec.ClusterAutoscaler.CreateServiceAccount != nil {
		originalValues.CreateServiceAccount = *instance.Spec.ClusterAutoscaler.CreateServiceAccount
	}
	if instance.Spec.ClusterAutoscaler.RepositoryURL != "" {
		originalValues.RepositoryURL = instance.Spec.ClusterAutoscaler.RepositoryURL
	}
	if instance.Spec.ClusterAutoscaler.Version != "" {
		originalValues.Version = instance.Spec.ClusterAutoscaler.Version
	}
	return originalValues
}
