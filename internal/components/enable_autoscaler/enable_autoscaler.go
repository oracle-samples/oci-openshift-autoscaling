/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package enableautoscaler

import (
	"context"

	ocicapioperatorv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	"github.com/openshift/oci-capi-operator/internal/components"
	"github.com/openshift/oci-capi-operator/internal/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func GetComponents(ctx context.Context, client client.Client, managedResourceNamespace string, tokenServiceAccountNamespace string, clusterName string, capiServiceAccountName string, autoscaler *ocicapioperatorv1alpha1.OCIClusterAutoscaler, config Config, useInstancePrincipal bool) *components.Component {
	managedNamespace, managedNamespaceMutateFn := ManagedResourceNamespace(managedResourceNamespace, autoscaler)
	bootstrapConfigSecret, bootstrapConfigSecretMutateFn := BootstrapConfigSecret(ctx, client, managedResourceNamespace, clusterName, autoscaler)
	kubeConfigSecret, kubeConfigSecretMutateFn := KubeConfigSecret(ctx, client, managedResourceNamespace, managedResourceNamespace, tokenServiceAccountNamespace, clusterName, capiServiceAccountName, autoscaler)

	ociCluster, ociClusterMutateFn := OCICluster(managedResourceNamespace, clusterName, autoscaler, config, useInstancePrincipal)
	cluster, clusterMutateFn := CAPICluster(managedResourceNamespace, clusterName, autoscaler, config)
	machineTemplate, machineTemplateMutateFn := OCIMachineTemplate(managedResourceNamespace, clusterName, autoscaler, config)
	machineDeployment, machineDeploymentMutateFn := MachineDeployment(managedResourceNamespace, clusterName, autoscaler, config)
	machineHealthCheck, machineHealthCheckMutateFn := MachineHealthCheck(managedResourceNamespace, clusterName, autoscaler, config)

	subcomponents := components.SubcomponentList{
		{Name: "managedResourceNamespace", Object: managedNamespace, MutateFn: managedNamespaceMutateFn},
		{Name: "bootstrapConfigSecret", Object: bootstrapConfigSecret, MutateFn: bootstrapConfigSecretMutateFn},
		{Name: "kubeConfigSecret", Object: kubeConfigSecret, MutateFn: kubeConfigSecretMutateFn},
		{Name: "machineTemplate", Object: machineTemplate, MutateFn: machineTemplateMutateFn},
		{Name: "machineDeployment", Object: machineDeployment, MutateFn: machineDeploymentMutateFn},
		{Name: "machineHealthCheck", Object: machineHealthCheck, MutateFn: machineHealthCheckMutateFn},
		{Name: "ociCluster", Object: ociCluster, MutateFn: ociClusterMutateFn},
		{Name: "cluster", Object: cluster, MutateFn: clusterMutateFn},
	}
	if useInstancePrincipal {
		identity, identityMutateFn := OCIClusterIdentity(managedResourceNamespace, clusterName, autoscaler)
		subcomponents = append(subcomponents, components.Subcomponent{
			Name:     "ociClusterIdentity",
			Object:   identity,
			MutateFn: identityMutateFn,
		})
	}

	return &components.Component{
		InstanceName:  autoscaler.Name,
		Name:          "EnableAutoscaler",
		Subcomponents: subcomponents,
	}
}

func ManagedResourceNamespace(namespaceName string, instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler) (client.Object, controllerutil.MutateFn) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
	}
	mutateFn := func() error {
		utils.SetDefaultLabels(namespace, instance.Name)
		return nil
	}
	return namespace, mutateFn
}
