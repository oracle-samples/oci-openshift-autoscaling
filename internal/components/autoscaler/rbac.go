/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package autoscaler

import (
	"fmt"

	ocicapiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/oci-capi-operator/internal/utils"
)

func autoscalerInfrastructureReadResources() []string {
	return []string{
		"ociclusters",
		"ociclusteridentities",
		"ocimachinetemplates",
		"ocimachines",
	}
}

func ClusterRole(autoscalerName string, instance *ocicapiv1alpha1.OCIClusterAutoscaler) (client.Object, func() error) {
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-extra", autoscalerName),
		},
	}

	mutateFn := func() error {
		clusterRole.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{"infrastructure.cluster.x-k8s.io"},
				Resources: autoscalerInfrastructureReadResources(),
				Verbs:     []string{"get", "list", "watch"},
			},
		}
		utils.SetDefaultLabels(clusterRole, instance.Name)
		return nil
	}

	return clusterRole, mutateFn
}

func ClusterRoleBinding(values *AutoscalerDeploymentValues, instance *ocicapiv1alpha1.OCIClusterAutoscaler) (client.Object, func() error) {
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-extra", values.Name),
		},
	}

	mutateFn := func() error {
		clusterRoleBinding.RoleRef = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     fmt.Sprintf("%s-extra", values.Name),
		}
		clusterRoleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      values.ServiceAccountName,
				Namespace: values.Namespace,
			},
		}
		utils.SetDefaultLabels(clusterRoleBinding, instance.Name)
		return nil
	}

	return clusterRoleBinding, mutateFn
}
