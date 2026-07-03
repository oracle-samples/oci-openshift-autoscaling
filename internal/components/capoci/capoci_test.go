/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package capoci

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	"github.com/openshift/oci-capi-operator/internal/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var _ = Describe("CAPOCI Components", func() {
	var (
		instance *capiv1alpha1.OCIClusterAutoscaler
		auth     *CAPOCICredentials
	)

	BeforeEach(func() {
		instance = &capiv1alpha1.OCIClusterAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-autoscaler",
			},
		}

		auth = &CAPOCICredentials{
			TenancyID:            "test-tenancy",
			UserID:               "test-user",
			Region:               "test-region",
			Fingerprint:          "test-fingerprint",
			PrivateKey:           "test-key",
			UseInstancePrincipal: "false",
			Passphrase:           "test-passphrase",
		}
	})

	Context("Namespace", func() {
		It("should create a namespace with correct configuration", func() {
			obj, mutateFn := Namespace("capoci-system", instance)
			namespace, ok := obj.(*corev1.Namespace)
			Expect(ok).To(BeTrue(), "Object should be a Namespace")

			// Verify initial state
			Expect(namespace.Name).To(Equal("capoci-system"))
			Expect(namespace.Labels).To(BeEmpty())

			// Apply mutation
			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			// Verify labels
			defaultLabels := utils.GetDefaultLabels(instance.Name)
			Expect(namespace.Labels).To(Equal(defaultLabels))
		})
	})

	Context("AuthConfigSecret", func() {
		It("should create a full secret for key-based auth", func() {
			obj, mutateFn := AuthConfigSecret(instance, "capoci-system", auth)
			secret, ok := obj.(*corev1.Secret)
			Expect(ok).To(BeTrue(), "Object should be a Secret")

			// Verify initial state
			Expect(secret.Name).To(Equal("capoci-auth-config"))
			Expect(secret.Namespace).To(Equal("capoci-system"))
			Expect(secret.Labels).To(BeEmpty())
			Expect(secret.Data).To(BeEmpty())

			// Apply mutation
			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			// Verify labels
			defaultLabels := utils.GetDefaultLabels(instance.Name)
			Expect(secret.Labels).To(Equal(defaultLabels))

			// Verify secret data
			Expect(secret.Data).To(HaveLen(7))
			Expect(string(secret.Data["tenancy"])).To(Equal(auth.TenancyID))
			Expect(string(secret.Data["user"])).To(Equal(auth.UserID))
			Expect(string(secret.Data["region"])).To(Equal(auth.Region))
			Expect(string(secret.Data["fingerprint"])).To(Equal(auth.Fingerprint))
			Expect(string(secret.Data["key"])).To(Equal(auth.PrivateKey))
			Expect(string(secret.Data["useInstancePrincipal"])).To(Equal(auth.UseInstancePrincipal))
			Expect(string(secret.Data["passphrase"])).To(Equal(auth.Passphrase))
		})

		It("should create a minimal secret for instance-principal auth", func() {
			instancePrincipalAuth := &CAPOCICredentials{
				Region:               "test-region",
				UseInstancePrincipal: "true",
			}
			obj, mutateFn := AuthConfigSecret(instance, "capoci-system", instancePrincipalAuth)
			secret, ok := obj.(*corev1.Secret)
			Expect(ok).To(BeTrue())

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			Expect(secret.Data).To(HaveLen(2))
			Expect(string(secret.Data["region"])).To(Equal(instancePrincipalAuth.Region))
			Expect(string(secret.Data["useInstancePrincipal"])).To(Equal(instancePrincipalAuth.UseInstancePrincipal))
			Expect(secret.Data).NotTo(HaveKey("tenancy"))
			Expect(secret.Data).NotTo(HaveKey("user"))
			Expect(secret.Data).NotTo(HaveKey("fingerprint"))
			Expect(secret.Data).NotTo(HaveKey("key"))
			Expect(secret.Data).NotTo(HaveKey("passphrase"))
		})

		It("should omit an empty passphrase for key-based auth", func() {
			keyBasedAuthWithoutPassphrase := &CAPOCICredentials{
				TenancyID:            "test-tenancy",
				UserID:               "test-user",
				Region:               "test-region",
				Fingerprint:          "test-fingerprint",
				PrivateKey:           "test-key",
				UseInstancePrincipal: "false",
			}
			obj, mutateFn := AuthConfigSecret(instance, "capoci-system", keyBasedAuthWithoutPassphrase)
			secret, ok := obj.(*corev1.Secret)
			Expect(ok).To(BeTrue())

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			Expect(secret.Data).To(HaveLen(6))
			Expect(secret.Data).NotTo(HaveKey("passphrase"))
		})
	})

	Context("GetComponents", func() {
		It("should return component with all subcomponents", func() {
			component := GetComponents("capoci-system", instance, auth)

			Expect(component.Name).To(Equal("CAPOCI"))
			Expect(component.Subcomponents).To(HaveLen(2))

			// Verify Namespace subcomponent
			ns := component.Subcomponents[0]
			Expect(ns.Name).To(Equal("namespace"))
			_, ok := ns.Object.(*corev1.Namespace)
			Expect(ok).To(BeTrue())
			Expect(ns.MutateFn).NotTo(BeNil())

			// Verify AuthConfigSecret subcomponent
			secret := component.Subcomponents[1]
			Expect(secret.Name).To(Equal("authConfigSecret"))
			_, ok = secret.Object.(*corev1.Secret)
			Expect(ok).To(BeTrue())
			Expect(secret.MutateFn).NotTo(BeNil())

			// Test that all mutation functions work
			for _, sub := range component.Subcomponents {
				err := sub.MutateFn()
				Expect(err).NotTo(HaveOccurred())
			}
		})
	})

	Context("setStartupArgs", func() {
		It("should disable eager OCI client initialization when using instance principal", func() {
			deployment := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{
										"args": []interface{}{
											"--leader-elect",
											"--init-oci-clients-on-startup=true",
										},
									},
								},
							},
						},
					},
				},
			}

			auth.UseInstancePrincipal = "true"
			err := setStartupArgs(deployment, auth, true)
			Expect(err).NotTo(HaveOccurred())

			containers, found, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			args, found, err := unstructured.NestedStringSlice(containers[0].(map[string]interface{}), "args")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(args).To(ContainElement("--enable-instance-metadata-service-lookup=true"))
			Expect(args).To(ContainElement("--init-oci-clients-on-startup=false"))

			hostNetwork, found, err := unstructured.NestedBool(deployment.Object, "spec", "template", "spec", "hostNetwork")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(hostNetwork).To(BeTrue())

			dnsPolicy, found, err := unstructured.NestedString(deployment.Object, "spec", "template", "spec", "dnsPolicy")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(dnsPolicy).To(Equal("ClusterFirstWithHostNet"))
		})

		It("should preserve eager OCI client initialization when not using instance principal", func() {
			deployment := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{
										"args": []interface{}{
											"--leader-elect",
											"--init-oci-clients-on-startup=true",
										},
									},
								},
							},
						},
					},
				},
			}

			auth.UseInstancePrincipal = "false"
			err := setStartupArgs(deployment, auth, false)
			Expect(err).NotTo(HaveOccurred())

			containers, found, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			args, found, err := unstructured.NestedStringSlice(containers[0].(map[string]interface{}), "args")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(args).To(ContainElement("--enable-instance-metadata-service-lookup=false"))
			Expect(args).To(ContainElement("--init-oci-clients-on-startup=true"))

			hostNetwork, found, err := unstructured.NestedBool(deployment.Object, "spec", "template", "spec", "hostNetwork")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(hostNetwork).To(BeFalse())

			dnsPolicy, found, err := unstructured.NestedString(deployment.Object, "spec", "template", "spec", "dnsPolicy")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(dnsPolicy).To(Equal("ClusterFirst"))
		})
	})
})
