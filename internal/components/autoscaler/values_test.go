/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package autoscaler

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
)

type AutoscalerValues struct {
	CloudProvider    string `json:"cloudProvider"`
	FullnameOverride string `json:"fullnameOverride"`
	AutoDiscovery    struct {
		Namespace string `json:"namespace"`
	} `json:"autoDiscovery"`
	ExtraArgs struct {
		ScanInterval string `json:"scan-interval"`
	} `json:"extraArgs"`
	RBAC struct {
		Create         bool `json:"create"`
		ServiceAccount struct {
			Create bool   `json:"create"`
			Name   string `json:"name"`
		} `json:"serviceAccount"`
	} `json:"rbac"`
}

var _ = Describe("Autoscaler Values", func() {
	var (
		defaultValues AutoscalerDeploymentValues
	)

	BeforeEach(func() {
		defaultValues = AutoscalerDeploymentValues{
			CloudProvider:          "oci",
			Name:                   "cluster-autoscaler",
			Namespace:              "autoscaler-system",
			AutoDiscoveryNamespace: "managed-capi-system",
			ServiceAccountName:     "cluster-autoscaler",
			CreateRBAC:             false,
			CreateServiceAccount:   false,
			RepositoryURL:          "https://kubernetes.github.io/autoscaler",
			Chart:                  "cluster-autoscaler",
			Version:                "1.0.0",
		}
	})

	Context("GetValuesString", func() {
		It("should generate valid YAML", func() {
			valuesStr := GetValuesString(&defaultValues)

			// Parse the YAML to verify it's valid
			var values AutoscalerValues
			err := yaml.Unmarshal([]byte(valuesStr), &values)
			Expect(err).NotTo(HaveOccurred(), "Values should be valid YAML")

			// Verify the parsed values match what we expect
			Expect(values.CloudProvider).To(Equal("oci"))
			Expect(values.FullnameOverride).To(Equal("cluster-autoscaler"))
			Expect(values.AutoDiscovery.Namespace).To(Equal("managed-capi-system"))
			Expect(values.ExtraArgs.ScanInterval).To(Equal("60s"))
			Expect(values.RBAC.Create).To(BeFalse())
			Expect(values.RBAC.ServiceAccount.Create).To(BeFalse())
			Expect(values.RBAC.ServiceAccount.Name).To(Equal("cluster-autoscaler"))
		})

		It("should generate YAML with proper indentation", func() {
			valuesStr := GetValuesString(&defaultValues)

			// Re-marshal the YAML to get canonical formatting
			var values interface{}
			err := yaml.Unmarshal([]byte(valuesStr), &values)
			Expect(err).NotTo(HaveOccurred())

			formattedYAML, err := yaml.Marshal(values)
			Expect(err).NotTo(HaveOccurred())

			// The formatted YAML should match our original after normalization
			var originalValues interface{}
			err = yaml.Unmarshal([]byte(valuesStr), &originalValues)
			Expect(err).NotTo(HaveOccurred())

			var formattedValues interface{}
			err = yaml.Unmarshal(formattedYAML, &formattedValues)
			Expect(err).NotTo(HaveOccurred())

			Expect(formattedValues).To(Equal(originalValues))
		})

		It("should handle boolean values correctly", func() {
			defaultValues.CreateRBAC = true
			defaultValues.CreateServiceAccount = true
			valuesStr := GetValuesString(&defaultValues)

			var values AutoscalerValues
			err := yaml.Unmarshal([]byte(valuesStr), &values)
			Expect(err).NotTo(HaveOccurred())

			Expect(values.RBAC.Create).To(BeTrue())
			Expect(values.RBAC.ServiceAccount.Create).To(BeTrue())
		})

		It("should fall back to the install namespace when discovery namespace is empty", func() {
			defaultValues.AutoDiscoveryNamespace = ""
			valuesStr := GetValuesString(&defaultValues)

			var values AutoscalerValues
			err := yaml.Unmarshal([]byte(valuesStr), &values)
			Expect(err).NotTo(HaveOccurred())
			Expect(values.AutoDiscovery.Namespace).To(Equal("autoscaler-system"))
		})
	})

	Context("GetAutoscalerDeploymentValues", func() {
		It("should use default values when instance values are empty", func() {
			instance := &capiv1alpha1.OCIClusterAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-autoscaler",
				},
				Spec: capiv1alpha1.OCIClusterAutoscalerSpec{
					ClusterAutoscaler: capiv1alpha1.ClusterAutoscalerConfig{},
				},
			}

			values := GetAutoscalerDeploymentValues(defaultValues, instance)
			Expect(values).To(Equal(defaultValues))
		})

		It("should override default values with instance values", func() {
			instance := &capiv1alpha1.OCIClusterAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-autoscaler",
				},
				Spec: capiv1alpha1.OCIClusterAutoscalerSpec{
					ClusterAutoscaler: capiv1alpha1.ClusterAutoscalerConfig{
						CloudProvider:        "custom-provider",
						Name:                 "custom-autoscaler",
						ServiceAccountName:   "custom-sa",
						CreateRBAC:           ptr.To(true),
						CreateServiceAccount: ptr.To(true),
						RepositoryURL:        "https://custom.repo",
						Version:              "2.0.0",
					},
				},
			}

			values := GetAutoscalerDeploymentValues(defaultValues, instance)

			// Verify overrides
			Expect(values.CloudProvider).To(Equal("custom-provider"))
			Expect(values.Name).To(Equal("custom-autoscaler"))
			Expect(values.Namespace).To(Equal(defaultValues.Namespace))
			Expect(values.ServiceAccountName).To(Equal("custom-sa"))
			Expect(values.CreateRBAC).To(BeTrue())
			Expect(values.CreateServiceAccount).To(BeTrue())
			Expect(values.RepositoryURL).To(Equal("https://custom.repo"))
			Expect(values.Version).To(Equal("2.0.0"))

			// Verify unchanged fields
			Expect(values.Chart).To(Equal(defaultValues.Chart))

			// Verify the generated YAML is valid
			valuesStr := GetValuesString(&values)
			var parsedValues AutoscalerValues
			err := yaml.Unmarshal([]byte(valuesStr), &parsedValues)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should override true boolean defaults with explicit false values", func() {
			defaultValues.CreateRBAC = true
			defaultValues.CreateServiceAccount = true
			instance := &capiv1alpha1.OCIClusterAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-autoscaler",
				},
				Spec: capiv1alpha1.OCIClusterAutoscalerSpec{
					ClusterAutoscaler: capiv1alpha1.ClusterAutoscalerConfig{
						CreateRBAC:           ptr.To(false),
						CreateServiceAccount: ptr.To(false),
					},
				},
			}

			values := GetAutoscalerDeploymentValues(defaultValues, instance)

			Expect(values.CreateRBAC).To(BeFalse())
			Expect(values.CreateServiceAccount).To(BeFalse())

			valuesStr := GetValuesString(&values)
			var parsedValues AutoscalerValues
			err := yaml.Unmarshal([]byte(valuesStr), &parsedValues)
			Expect(err).NotTo(HaveOccurred())
			Expect(parsedValues.RBAC.Create).To(BeFalse())
			Expect(parsedValues.RBAC.ServiceAccount.Create).To(BeFalse())
		})

		It("should handle partial overrides", func() {
			instance := &capiv1alpha1.OCIClusterAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-autoscaler",
				},
				Spec: capiv1alpha1.OCIClusterAutoscalerSpec{
					ClusterAutoscaler: capiv1alpha1.ClusterAutoscalerConfig{
						Name:               "custom-autoscaler",
						ServiceAccountName: "custom-sa",
					},
				},
			}

			values := GetAutoscalerDeploymentValues(defaultValues, instance)

			// Verify overridden fields
			Expect(values.Name).To(Equal("custom-autoscaler"))
			Expect(values.ServiceAccountName).To(Equal("custom-sa"))

			// Verify unchanged fields
			Expect(values.CloudProvider).To(Equal(defaultValues.CloudProvider))
			Expect(values.Namespace).To(Equal(defaultValues.Namespace))
			Expect(values.CreateRBAC).To(Equal(defaultValues.CreateRBAC))
			Expect(values.CreateServiceAccount).To(Equal(defaultValues.CreateServiceAccount))
			Expect(values.RepositoryURL).To(Equal(defaultValues.RepositoryURL))
			Expect(values.Version).To(Equal(defaultValues.Version))
			Expect(values.Chart).To(Equal(defaultValues.Chart))

			// Verify the generated YAML is valid
			valuesStr := GetValuesString(&values)
			var parsedValues AutoscalerValues
			err := yaml.Unmarshal([]byte(valuesStr), &parsedValues)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should allow explicit false RBAC and service account overrides", func() {
			defaultValues.CreateRBAC = true
			defaultValues.CreateServiceAccount = true
			instance := &capiv1alpha1.OCIClusterAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-autoscaler",
				},
				Spec: capiv1alpha1.OCIClusterAutoscalerSpec{
					ClusterAutoscaler: capiv1alpha1.ClusterAutoscalerConfig{
						CreateRBAC:           ptr.To(false),
						CreateServiceAccount: ptr.To(false),
					},
				},
			}

			values := GetAutoscalerDeploymentValues(defaultValues, instance)
			Expect(values.CreateRBAC).To(BeFalse())
			Expect(values.CreateServiceAccount).To(BeFalse())
		})
	})
})
