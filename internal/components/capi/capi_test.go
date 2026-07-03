/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package capi

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	securityv1 "github.com/openshift/api/security/v1"
	capiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	"github.com/openshift/oci-capi-operator/internal/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("CAPI Components", func() {
	var (
		instance *capiv1alpha1.OCIClusterAutoscaler
	)

	BeforeEach(func() {
		instance = &capiv1alpha1.OCIClusterAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-autoscaler",
			},
		}
	})

	Context("CAPINamespace", func() {
		It("should create a namespace with correct configuration", func() {
			obj, mutateFn := CAPINamespace("oci-openshift-autoscaling-operator", instance)
			namespace, ok := obj.(*corev1.Namespace)
			Expect(ok).To(BeTrue(), "Object should be a Namespace")

			// Verify initial state
			Expect(namespace.Name).To(Equal("oci-openshift-autoscaling-operator"))
			Expect(namespace.Labels).To(BeEmpty())

			// Apply mutation
			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			// Verify labels
			defaultLabels := utils.GetDefaultLabels(instance.Name)
			Expect(namespace.Labels).To(Equal(defaultLabels))
		})
	})

	Context("SecurityContextConstraints", func() {
		It("should create SCC with correct configuration", func() {
			obj, mutateFn := SecurityContextConstraints(
				"oci-openshift-autoscaling-operator",
				"capoci-system",
				"capoci-sa",
				"capi-sa",
				instance,
				false,
			)
			scc, ok := obj.(*securityv1.SecurityContextConstraints)
			Expect(ok).To(BeTrue(), "Object should be a SecurityContextConstraints")

			// Verify initial state
			Expect(scc.Name).To(Equal("oci-capi"))

			// Apply mutation
			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			// Verify configuration
			Expect(scc.RunAsUser.Type).To(Equal(securityv1.RunAsUserStrategyRunAsAny))
			Expect(scc.SELinuxContext.Type).To(Equal(securityv1.SELinuxStrategyRunAsAny))
			Expect(scc.AllowHostNetwork).To(BeFalse())
			Expect(scc.AllowHostPorts).To(BeFalse())
			Expect(scc.SeccompProfiles).To(ConsistOf("runtime/default"))

			// Verify service account users
			expectedUsers := []string{
				"system:serviceaccount:capoci-system:capoci-sa",
				"system:serviceaccount:oci-openshift-autoscaling-operator:capi-sa",
			}
			Expect(scc.Users).To(ConsistOf(expectedUsers))

			// Verify labels
			defaultLabels := utils.GetDefaultLabels(instance.Name)
			Expect(scc.Labels).To(Equal(defaultLabels))
		})

		It("should allow host networking when requested for CAPOCI", func() {
			obj, mutateFn := SecurityContextConstraints(
				"oci-openshift-autoscaling-operator",
				"capoci-system",
				"capoci-sa",
				"capi-sa",
				instance,
				true,
			)
			scc, ok := obj.(*securityv1.SecurityContextConstraints)
			Expect(ok).To(BeTrue())

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			Expect(scc.AllowHostNetwork).To(BeTrue())
			Expect(scc.AllowHostPorts).To(BeTrue())
		})
	})

	Context("GetComponents", func() {
		It("should return component with all subcomponents", func() {
			component := GetComponents(
				"oci-openshift-autoscaling-operator",
				"capoci-system",
				"capi-sa",
				"capoci-sa",
				instance,
				false,
			)

			Expect(component.Name).To(Equal("CAPI"))
			Expect(component.Subcomponents).To(HaveLen(2))

			// Verify SCC subcomponent
			scc := component.Subcomponents[0]
			Expect(scc.Name).To(Equal("scc"))
			_, ok := scc.Object.(*securityv1.SecurityContextConstraints)
			Expect(ok).To(BeTrue())
			Expect(scc.MutateFn).NotTo(BeNil())

			// Verify Namespace subcomponent
			ns := component.Subcomponents[1]
			Expect(ns.Name).To(Equal("namespace"))
			_, ok = ns.Object.(*corev1.Namespace)
			Expect(ok).To(BeTrue())
			Expect(ns.MutateFn).NotTo(BeNil())

			// Test that all mutation functions work
			for _, sub := range component.Subcomponents {
				err := sub.MutateFn()
				Expect(err).NotTo(HaveOccurred())
			}
		})
	})
})
