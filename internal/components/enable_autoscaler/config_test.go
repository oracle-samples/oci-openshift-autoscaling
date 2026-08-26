/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package enableautoscaler

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ocicapioperatorv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("Config", func() {
	var (
		ctx      context.Context
		instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler
		config   Config
	)

	BeforeEach(func() {
		ctx = context.Background()
		instance = &ocicapioperatorv1alpha1.OCIClusterAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-autoscaler",
			},
			Spec: ocicapioperatorv1alpha1.OCIClusterAutoscalerSpec{
				Autoscaling: ocicapioperatorv1alpha1.AutoscalingConfig{},
			},
		}

		config = Config{
			AutoScalingConfig: AutoScalingConfig{
				CPUs:                 2,
				Memory:               4,
				MinNodes:             1,
				MaxNodes:             3,
				Shape:                "oc3",
				ImageID:              "",
				DefinedTagsNamespace: "test-defined-tags-namespace",
			},
			ClusterConfig: ClusterConfig{
				CompartmentID: "test-compartment",
			},
			NetworkConfig: NetworkConfig{
				VCNID:                   "test-vcn",
				OCPSubnetID:             "test-ocp-subnet",
				OCPSubnetName:           "private",
				ComputeNsgName:          "ComputeNSG",
				BareMetalSubnetID:       "test-bm-subnet",
				BareMetalSubnetName:     "private-secondary",
				NetworkSecurityGroupID:  "test-nsg",
				ControlPlaneEndpoint:    "test-endpoint",
				APIServerLoadBalancerID: "test-lb",
				ClusterNetworkCIDRBlock: "10.0.0.0/16",
				ServiceNetworkCIDRBlock: "10.1.0.0/16",
			},
		}
	})

	Context("SetAutoScalingConfig", func() {
		It("should use default values when instance values are empty", func() {
			mockClient := &MockClient{}
			result, err := SetAutoScalingConfig(ctx, mockClient, instance, config)
			Expect(err).NotTo(HaveOccurred())

			Expect(result.AutoScalingConfig).To(Equal(config.AutoScalingConfig))
		})

		It("should override values from instance", func() {
			instance.Spec.Autoscaling = ocicapioperatorv1alpha1.AutoscalingConfig{
				MinNodes: ptr.To[int32](2),
				MaxNodes: ptr.To[int32](5),
				Shape:    "custom-shape",
				ImageID:  "custom-image",
				ShapeConfig: &ocicapioperatorv1alpha1.ShapeConfig{
					CPUs:   ptr.To[int32](4),
					Memory: ptr.To[int32](8),
				},
			}

			mockClient := &MockClient{}
			result, err := SetAutoScalingConfig(ctx, mockClient, instance, config)
			Expect(err).NotTo(HaveOccurred())

			Expect(result.AutoScalingConfig.CPUs).To(Equal(int32(4)))
			Expect(result.AutoScalingConfig.Memory).To(Equal(int32(8)))
			Expect(result.AutoScalingConfig.MinNodes).To(Equal(int32(2)))
			Expect(result.AutoScalingConfig.MaxNodes).To(Equal(int32(5)))
			Expect(result.AutoScalingConfig.Shape).To(Equal("custom-shape"))
			Expect(result.AutoScalingConfig.ImageID).To(Equal("custom-image"))
			// Networking config should be the same as the default config
			Expect(result.NetworkConfig.ClusterNetworkCIDRBlock).To(Equal("10.0.0.0/16"))
			Expect(result.NetworkConfig.ServiceNetworkCIDRBlock).To(Equal("10.1.0.0/16"))
			Expect(result.NetworkConfig.NetworkSecurityGroupID).To(Equal("test-nsg"))
			Expect(result.NetworkConfig.ControlPlaneEndpoint).To(Equal("test-endpoint"))
			Expect(result.NetworkConfig.APIServerLoadBalancerID).To(Equal("test-lb"))
			Expect(result.NetworkConfig.VCNID).To(Equal("test-vcn"))
			Expect(result.NetworkConfig.OCPSubnetID).To(Equal("test-ocp-subnet"))
		})

		It("should handle partial overrides", func() {
			instance.Spec.Autoscaling = ocicapioperatorv1alpha1.AutoscalingConfig{
				MinNodes: ptr.To[int32](2),
				Shape:    "custom-shape",
			}

			mockClient := &MockClient{}
			result, err := SetAutoScalingConfig(ctx, mockClient, instance, config)
			Expect(err).NotTo(HaveOccurred())

			// Overridden values
			Expect(result.AutoScalingConfig.MinNodes).To(Equal(int32(2)))
			Expect(result.AutoScalingConfig.Shape).To(Equal("custom-shape"))

			// Default values
			Expect(result.AutoScalingConfig.CPUs).To(Equal(int32(2)))
			Expect(result.AutoScalingConfig.Memory).To(Equal(int32(4)))
			Expect(result.AutoScalingConfig.MaxNodes).To(Equal(int32(3)))
			Expect(result.AutoScalingConfig.ImageID).To(Equal(""))
		})

		It("should reject explicit zero shape config values", func() {
			instance.Spec.Autoscaling = ocicapioperatorv1alpha1.AutoscalingConfig{
				ShapeConfig: &ocicapioperatorv1alpha1.ShapeConfig{
					CPUs:   ptr.To[int32](0),
					Memory: ptr.To[int32](8),
				},
			}

			mockClient := &MockClient{}
			_, err := SetAutoScalingConfig(ctx, mockClient, instance, config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("shapeConfig.cpus must be greater than 0"))
		})

		It("should allow partial shape config overrides", func() {
			instance.Spec.Autoscaling = ocicapioperatorv1alpha1.AutoscalingConfig{
				ShapeConfig: &ocicapioperatorv1alpha1.ShapeConfig{
					Memory: ptr.To[int32](16),
				},
			}

			mockClient := &MockClient{}
			result, err := SetAutoScalingConfig(ctx, mockClient, instance, config)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.AutoScalingConfig.CPUs).To(Equal(int32(2)))
			Expect(result.AutoScalingConfig.Memory).To(Equal(int32(16)))
		})

		It("should reject RDMA autoscaling without a Compute Cluster OCID", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Optimized3.36"
			cfg.AutoScalingConfig.EnableRDMA = true
			cfg.AutoScalingConfig.RDMAComputeClusterID = ""
			cfg.AutoScalingConfig.RDMAFailureDomain = "1"
			err := ValidateRDMAConfig(cfg)
			Expect(err).To(MatchError(ContainSubstring("RDMA compute cluster ID must not be empty")))
		})

		It("should reject RDMA autoscaling without a failure domain", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Optimized3.36"
			cfg.AutoScalingConfig.EnableRDMA = true
			cfg.AutoScalingConfig.RDMAComputeClusterID = "ocid1.computecluster.oc1..example"
			cfg.AutoScalingConfig.RDMAFailureDomain = ""
			err := ValidateRDMAConfig(cfg)
			Expect(err).To(MatchError(ContainSubstring("RDMA failure domain must not be empty")))
		})

		It("should reject non-bare-metal shapes for RDMA autoscaling", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "VM.Standard.E4.Flex"
			cfg.AutoScalingConfig.EnableRDMA = true
			cfg.AutoScalingConfig.RDMAComputeClusterID = "ocid1.computecluster.oc1..example"
			cfg.AutoScalingConfig.RDMAFailureDomain = "1"
			err := ValidateRDMAConfig(cfg)
			Expect(err).To(MatchError(ContainSubstring("bare-metal shape")))
		})

		It("should treat explicit zero node counts as overrides", func() {
			instance.Spec.Autoscaling = ocicapioperatorv1alpha1.AutoscalingConfig{
				MinNodes: ptr.To[int32](0),
				MaxNodes: ptr.To[int32](0),
			}

			mockClient := &MockClient{}
			result, err := SetAutoScalingConfig(ctx, mockClient, instance, config)
			Expect(err).NotTo(HaveOccurred())

			Expect(result.AutoScalingConfig.MinNodes).To(Equal(int32(0)))
			Expect(result.AutoScalingConfig.MaxNodes).To(Equal(int32(0)))
		})
	})

	Context("SetNetworkConfig", func() {
		It("should use existing values when CIDRs are set", func() {
			mockClient := &MockClient{}
			result, err := SetNetworkConfig(ctx, mockClient, config)
			Expect(err).NotTo(HaveOccurred())

			Expect(result.NetworkConfig.ClusterNetworkCIDRBlock).To(Equal("10.0.0.0/16"))
			Expect(result.NetworkConfig.ServiceNetworkCIDRBlock).To(Equal("10.1.0.0/16"))
		})

		It("should error when secondary subnet equals primary", func() {
			mockClient := &MockClient{}
			cfg := config
			cfg.NetworkConfig.BareMetalSubnetID = cfg.NetworkConfig.OCPSubnetID
			_, err := SetNetworkConfig(ctx, mockClient, cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("secondary subnet must differ from primary subnet"))
		})

		It("should accept a distinct secondary subnet and leave CIDRs unchanged", func() {
			mockClient := &MockClient{}
			cfg := config
			cfg.NetworkConfig.BareMetalSubnetID = "test-bm-subnet"
			result, err := SetNetworkConfig(ctx, mockClient, cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.NetworkConfig.BareMetalSubnetID).To(Equal("test-bm-subnet"))
			Expect(result.NetworkConfig.ClusterNetworkCIDRBlock).To(Equal("10.0.0.0/16"))
			Expect(result.NetworkConfig.ServiceNetworkCIDRBlock).To(Equal("10.1.0.0/16"))
		})
	})

	Context("ValidateDefinedTagsNamespace", func() {
		It("should allow BM shape when DefinedTagsNamespace is empty", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			cfg.AutoScalingConfig.DefinedTagsNamespace = ""
			err := ValidateDefinedTagsNamespace(cfg)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should allow BM shape when DefinedTagsNamespace is whitespace", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			cfg.AutoScalingConfig.DefinedTagsNamespace = "   "
			err := ValidateDefinedTagsNamespace(cfg)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should pass when DefinedTagsNamespace is set", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			cfg.AutoScalingConfig.DefinedTagsNamespace = "test.namespace_1"
			err := ValidateDefinedTagsNamespace(cfg)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should reject DefinedTagsNamespace with whitespace", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			cfg.AutoScalingConfig.DefinedTagsNamespace = "test namespace"
			err := ValidateDefinedTagsNamespace(cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("defined tags namespace"))
		})

		It("should reject DefinedTagsNamespace with path separators", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			cfg.AutoScalingConfig.DefinedTagsNamespace = "test/namespace"
			err := ValidateDefinedTagsNamespace(cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("defined tags namespace"))
		})

		It("should reject DefinedTagsNamespace longer than 100 characters", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			cfg.AutoScalingConfig.DefinedTagsNamespace = "a" + strings.Repeat("b", 100)
			err := ValidateDefinedTagsNamespace(cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("defined tags namespace"))
		})

		It("should pass for non-BM shape when DefinedTagsNamespace is empty", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "VM.Standard.E5.Flex"
			cfg.AutoScalingConfig.DefinedTagsNamespace = ""
			err := ValidateDefinedTagsNamespace(cfg)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("ValidateRequiredConfigFields", func() {
		It("should fail when CompartmentID is empty", func() {
			cfg := config
			cfg.ClusterConfig.CompartmentID = ""
			err := ValidateRequiredConfigFields(cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("compartment ID must not be empty"))
		})

		It("should fail when VCNID is empty", func() {
			cfg := config
			cfg.NetworkConfig.VCNID = ""
			err := ValidateRequiredConfigFields(cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("VCN ID must not be empty"))
		})

		It("should fail when OCPSubnetID is empty", func() {
			cfg := config
			cfg.NetworkConfig.OCPSubnetID = ""
			err := ValidateRequiredConfigFields(cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("OCP subnet ID must not be empty"))
		})

		It("should fail when OCPSubnetName is empty", func() {
			cfg := config
			cfg.NetworkConfig.OCPSubnetName = ""
			err := ValidateRequiredConfigFields(cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("OCP subnet name must not be empty"))
		})

		It("should fail when ComputeNsgName is empty", func() {
			cfg := config
			cfg.NetworkConfig.ComputeNsgName = ""
			err := ValidateRequiredConfigFields(cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("compute NSG name must not be empty"))
		})

		It("should fail when APIServerLoadBalancerID is empty", func() {
			cfg := config
			cfg.NetworkConfig.APIServerLoadBalancerID = ""
			err := ValidateRequiredConfigFields(cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("API server load balancer ID must not be empty"))
		})

		It("should fail when NetworkSecurityGroupID is empty", func() {
			cfg := config
			cfg.NetworkConfig.NetworkSecurityGroupID = ""
			err := ValidateRequiredConfigFields(cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("network security group ID must not be empty"))
		})

		It("should fail when ControlPlaneEndpoint is empty", func() {
			cfg := config
			cfg.NetworkConfig.ControlPlaneEndpoint = ""
			err := ValidateRequiredConfigFields(cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("control plane endpoint must not be empty"))
		})

		It("should pass when required fields are set", func() {
			cfg := config
			err := ValidateRequiredConfigFields(cfg)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("ValidateBareMetalSubnetConfig", func() {
		It("should fail when BareMetalSubnetID is empty for BM shape", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			cfg.NetworkConfig.BareMetalSubnetID = ""
			err := ValidateBareMetalSubnetConfig(cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bare metal subnet ID must not be empty for BM shapes"))
		})

		It("should fail when BareMetalSubnetName is empty for BM shape", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			cfg.NetworkConfig.BareMetalSubnetName = ""
			err := ValidateBareMetalSubnetConfig(cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bare metal subnet name must not be empty for BM shapes"))
		})

		It("should pass when BM subnets are set for BM shape", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			err := ValidateBareMetalSubnetConfig(cfg)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should pass when shape is not BM", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "VM.Standard.E4.Flex"
			cfg.NetworkConfig.BareMetalSubnetID = ""
			cfg.NetworkConfig.BareMetalSubnetName = ""
			err := ValidateBareMetalSubnetConfig(cfg)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
