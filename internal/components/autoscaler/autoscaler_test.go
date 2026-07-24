/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package autoscaler

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	"github.com/openshift/oci-capi-operator/internal/utils"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

var _ = Describe("Autoscaler", func() {
	var (
		instance *capiv1alpha1.OCIClusterAutoscaler
		values   *AutoscalerDeploymentValues
		scheme   *runtime.Scheme
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		err := clientgoscheme.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())
		err = rbacv1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		instance = &capiv1alpha1.OCIClusterAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-autoscaler",
			},
		}

		values = &AutoscalerDeploymentValues{
			CloudProvider:        "oci",
			Name:                 "cluster-autoscaler",
			Namespace:            "kube-system",
			ServiceAccountName:   "cluster-autoscaler-sa",
			CreateRBAC:           true,
			CreateServiceAccount: true,
			RepositoryURL:        "https://kubernetes.github.io/autoscaler",
			Chart:                "cluster-autoscaler",
			Version:              "1.0.0",
		}
	})

	Context("GetComponents", func() {
		It("should return component with correct subcomponents", func() {
			component := GetComponents(values, instance, scheme)

			Expect(component.Name).To(Equal("Autoscaler"))
			Expect(component.InstanceName).To(Equal(instance.Name))
			Expect(component.Subcomponents).To(HaveLen(3))

			// Verify Namespace subcomponent
			namespace := component.Subcomponents[0]
			Expect(namespace.Name).To(Equal("namespace"))
			ns, ok := namespace.Object.(*corev1.Namespace)
			Expect(ok).To(BeTrue())
			Expect(ns.Name).To(Equal(values.Namespace))
			Expect(namespace.MutateFn).NotTo(BeNil())

			// Verify ClusterRole subcomponent
			clusterRole := component.Subcomponents[1]
			Expect(clusterRole.Name).To(Equal("clusterRole"))
			_, ok = clusterRole.Object.(*rbacv1.ClusterRole)
			Expect(ok).To(BeTrue())
			Expect(clusterRole.MutateFn).NotTo(BeNil())

			// Verify ClusterRoleBinding subcomponent
			clusterRoleBinding := component.Subcomponents[2]
			Expect(clusterRoleBinding.Name).To(Equal("clusterRoleBinding"))
			_, ok = clusterRoleBinding.Object.(*rbacv1.ClusterRoleBinding)
			Expect(ok).To(BeTrue())
			Expect(clusterRoleBinding.MutateFn).NotTo(BeNil())
		})

		It("should create subcomponents that can be mutated", func() {
			component := GetComponents(values, instance, scheme)

			// Test Namespace mutation
			namespace := component.Subcomponents[0]
			err := namespace.MutateFn()
			Expect(err).NotTo(HaveOccurred())
			ns := namespace.Object.(*corev1.Namespace)
			defaultLabels := utils.GetDefaultLabels(instance.Name)
			Expect(ns.Labels).To(Equal(defaultLabels))

			// Test ClusterRole mutation
			clusterRole := component.Subcomponents[1]
			err = clusterRole.MutateFn()
			Expect(err).NotTo(HaveOccurred())
			role := clusterRole.Object.(*rbacv1.ClusterRole)
			Expect(role.Rules).To(HaveLen(1))
			Expect(role.Labels).To(Equal(defaultLabels))

			// Test ClusterRoleBinding mutation
			clusterRoleBinding := component.Subcomponents[2]
			err = clusterRoleBinding.MutateFn()
			Expect(err).NotTo(HaveOccurred())
			binding := clusterRoleBinding.Object.(*rbacv1.ClusterRoleBinding)
			Expect(binding.Subjects).To(HaveLen(1))
			Expect(binding.Labels).To(Equal(defaultLabels))
		})
	})

})
