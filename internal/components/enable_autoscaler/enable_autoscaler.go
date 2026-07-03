/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package enableautoscaler

import (
	"context"

	ocicapioperatorv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	"github.com/openshift/oci-capi-operator/internal/components"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetComponents(ctx context.Context, client client.Client, capiSystemNamespace string, clusterName string, capiServiceAccountName string, autoscaler *ocicapioperatorv1alpha1.OCIClusterAutoscaler, config Config, useInstancePrincipal bool) *components.Component {
	bootstrapConfigSecret, bootstrapConfigSecretMutateFn := BootstrapConfigSecret(ctx, client, capiSystemNamespace, clusterName, autoscaler)
	kubeConfigSecret, kubeConfigSecretMutateFn := KubeConfigSecret(ctx, client, capiSystemNamespace, clusterName, capiServiceAccountName, autoscaler)

	ociCluster, ociClusterMutateFn := OCICluster(capiSystemNamespace, clusterName, autoscaler, config, useInstancePrincipal)
	cluster, clusterMutateFn := CAPICluster(capiSystemNamespace, clusterName, autoscaler, config)
	machineTemplate, machineTemplateMutateFn := OCIMachineTemplate(capiSystemNamespace, clusterName, autoscaler, config)
	machineDeployment, machineDeploymentMutateFn := MachineDeployment(capiSystemNamespace, clusterName, autoscaler, config)
	machineHealthCheck, machineHealthCheckMutateFn := MachineHealthCheck(capiSystemNamespace, clusterName, autoscaler, config)

	subcomponents := components.SubcomponentList{
		{Name: "bootstrapConfigSecret", Object: bootstrapConfigSecret, MutateFn: bootstrapConfigSecretMutateFn},
		{Name: "kubeConfigSecret", Object: kubeConfigSecret, MutateFn: kubeConfigSecretMutateFn},
		{Name: "machineTemplate", Object: machineTemplate, MutateFn: machineTemplateMutateFn},
		{Name: "machineDeployment", Object: machineDeployment, MutateFn: machineDeploymentMutateFn},
		{Name: "machineHealthCheck", Object: machineHealthCheck, MutateFn: machineHealthCheckMutateFn},
		{Name: "ociCluster", Object: ociCluster, MutateFn: ociClusterMutateFn},
		{Name: "cluster", Object: cluster, MutateFn: clusterMutateFn},
	}
	if useInstancePrincipal {
		identity, identityMutateFn := OCIClusterIdentity(capiSystemNamespace, clusterName, autoscaler)
		subcomponents = append(subcomponents, components.Subcomponent{
			Name:     "ociClusterIdentity",
			Object:   identity,
			MutateFn: identityMutateFn,
		})
	}

	return &components.Component{
		Name:          "EnableAutoscaler",
		Subcomponents: subcomponents,
	}
}
