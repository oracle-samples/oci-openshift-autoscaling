/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package enableautoscaler

import (
	"strings"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ocicapioperatorv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	"github.com/openshift/oci-capi-operator/internal/utils"
	infrastructurev1beta2 "github.com/oracle/cluster-api-provider-oci/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
)

var _ = Describe("CAPI Components", func() {
	var (
		instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler
		config   Config
	)

	BeforeEach(func() {
		instance = &ocicapioperatorv1alpha1.OCIClusterAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-autoscaler",
			},
		}

		config = Config{
			ClusterConfig: ClusterConfig{
				CompartmentID: "test-compartment",
			},
			NetworkConfig: NetworkConfig{
				VCNID:                   "test-vcn",
				OCPSubnetID:             "test-ocp-subnet",
				OCPSubnetName:           "private",
				ComputeNsgName:          "ComputeNSG",
				BareMetalSubnetID:       "",
				BareMetalSubnetName:     "private-secondary",
				NetworkSecurityGroupID:  "test-nsg",
				ControlPlaneEndpoint:    "test-endpoint",
				APIServerLoadBalancerID: "test-lb",
				ClusterNetworkCIDRBlock: "10.0.0.0/16",
				ServiceNetworkCIDRBlock: "10.1.0.0/16",
			},
			AutoScalingConfig: AutoScalingConfig{
				CPUs:                 2,
				Memory:               4,
				MinNodes:             1,
				MaxNodes:             3,
				Shape:                "oc3",
				ImageID:              "test-image",
				DefinedTagsNamespace: "test-defined-tags-namespace",
			},
		}
	})

	Context("NodePoolName", func() {
		It("should validate the generated node pool name length", func() {
			instance.Spec.Autoscaling.PoolIdentifier = "pool1"
			nodePoolName := NodePoolName(strings.Repeat("a", 47), instance)

			err := ValidateNodePoolName(nodePoolName)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("at most 51 characters"))
		})

		It("should accept node pool names within the generated resource name budget", func() {
			instance.Spec.Autoscaling.PoolIdentifier = "pool1"
			nodePoolName := NodePoolName(strings.Repeat("a", 45), instance)

			err := ValidateNodePoolName(nodePoolName)

			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("OCICluster", func() {
		It("should create OCICluster with correct configuration", func() {
			obj, mutateFn := OCICluster("oci-openshift-autoscaling-operator", "test-cluster", instance, config, false)
			ociCluster, ok := obj.(*infrastructurev1beta2.OCICluster)
			Expect(ok).To(BeTrue(), "Object should be an OCICluster")

			// Verify initial state
			Expect(ociCluster.Name).To(Equal("test-cluster"))
			Expect(ociCluster.Namespace).To(Equal("oci-openshift-autoscaling-operator"))

			// Apply mutation
			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			// Verify labels and annotations
			defaultLabels := utils.GetDefaultLabels(instance.Name)
			Expect(ociCluster.Labels).To(Equal(defaultLabels))
			Expect(ociCluster.Annotations).To(HaveKeyWithValue("cluster.x-k8s.io/skip-apiserver-lb-management", "true"))

			// Verify spec
			Expect(ociCluster.Spec.CompartmentId).To(Equal(config.ClusterConfig.CompartmentID))
			Expect(ociCluster.Spec.ControlPlaneEndpoint.Host).To(Equal(config.NetworkConfig.ControlPlaneEndpoint))
			Expect(ociCluster.Spec.ControlPlaneEndpoint.Port).To(Equal(int32(6443)))

			// Verify network spec (primary only)
			Expect(*ociCluster.Spec.NetworkSpec.APIServerLB.LoadBalancerId).To(Equal(config.NetworkConfig.APIServerLoadBalancerID))
			Expect(ociCluster.Spec.NetworkSpec.SkipNetworkManagement).To(BeTrue())
			Expect(*ociCluster.Spec.NetworkSpec.Vcn.ID).To(Equal(config.NetworkConfig.VCNID))
			Expect(ociCluster.Spec.NetworkSpec.Vcn.Subnets).To(HaveLen(1))
			Expect(*ociCluster.Spec.NetworkSpec.Vcn.Subnets[0].ID).To(Equal(config.NetworkConfig.OCPSubnetID))
			Expect(ociCluster.Spec.NetworkSpec.Vcn.NetworkSecurityGroup.List).To(HaveLen(1))
			Expect(*ociCluster.Spec.NetworkSpec.Vcn.NetworkSecurityGroup.List[0].ID).To(Equal(config.NetworkConfig.NetworkSecurityGroupID))
		})

		It("should preserve existing annotations", func() {
			obj, mutateFn := OCICluster("oci-openshift-autoscaling-operator", "test-cluster", instance, config, false)
			ociCluster := obj.(*infrastructurev1beta2.OCICluster)

			// Add existing annotation
			ociCluster.Annotations = map[string]string{
				"existing-key": "existing-value",
			}

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			Expect(ociCluster.Annotations).To(HaveKeyWithValue("existing-key", "existing-value"))
			Expect(ociCluster.Annotations).To(HaveKeyWithValue("cluster.x-k8s.io/skip-apiserver-lb-management", "true"))
		})

		It("should set identityRef when instance principal is enabled", func() {
			obj, mutateFn := OCICluster("oci-openshift-autoscaling-operator", "test-cluster", instance, config, true)
			ociCluster := obj.(*infrastructurev1beta2.OCICluster)

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			Expect(ociCluster.Spec.IdentityRef).NotTo(BeNil())
			Expect(ociCluster.Spec.IdentityRef.Kind).To(Equal("OCIClusterIdentity"))
			Expect(ociCluster.Spec.IdentityRef.Name).To(Equal("test-cluster-identity"))
			Expect(ociCluster.Spec.IdentityRef.Namespace).To(Equal("oci-openshift-autoscaling-operator"))
		})
	})

	Context("OCIClusterIdentity", func() {
		It("should create OCIClusterIdentity for instance principal", func() {
			obj, mutateFn := OCIClusterIdentity("oci-openshift-autoscaling-operator", "test-cluster", instance)
			identity, ok := obj.(*infrastructurev1beta2.OCIClusterIdentity)
			Expect(ok).To(BeTrue(), "Object should be an OCIClusterIdentity")

			Expect(identity.Name).To(Equal("test-cluster-identity"))
			Expect(identity.Namespace).To(Equal("oci-openshift-autoscaling-operator"))

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			Expect(identity.Spec.Type).To(Equal(infrastructurev1beta2.InstancePrincipal))
			Expect(identity.Spec.AllowedNamespaces).NotTo(BeNil())
			Expect(identity.Spec.AllowedNamespaces.NamespaceList).To(ConsistOf("oci-openshift-autoscaling-operator"))
		})
	})

	Context("CAPICluster", func() {
		It("should build infrastructure refs with apiGroup for Cluster API v1beta2", func() {
			ref, err := infrastructureRef(ociInfrastructureAPIVersion, ociClusterKind, "test-namespace", "test-cluster")
			Expect(err).NotTo(HaveOccurred())
			Expect(ref).To(Equal(map[string]interface{}{
				"apiGroup": "infrastructure.cluster.x-k8s.io",
				"kind":     "OCICluster",
				"name":     "test-cluster",
			}))
		})

		It("should create Cluster with correct configuration", func() {
			obj, mutateFn := CAPICluster("oci-openshift-autoscaling-operator", "test-cluster", instance, config)
			cluster, ok := obj.(*unstructured.Unstructured)
			Expect(ok).To(BeTrue(), "Object should be a Cluster")

			// Verify initial state
			Expect(cluster.GetName()).To(Equal("test-cluster"))
			Expect(cluster.GetNamespace()).To(Equal("oci-openshift-autoscaling-operator"))
			Expect(cluster.GetAPIVersion()).To(Equal("cluster.x-k8s.io/v1beta2"))

			// Apply mutation
			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			// Verify labels
			defaultLabels := utils.GetDefaultLabels(instance.Name)
			Expect(cluster.GetLabels()).To(Equal(defaultLabels))

			// Verify spec
			podCIDRs, found, err := unstructured.NestedStringSlice(cluster.Object, "spec", "clusterNetwork", "pods", "cidrBlocks")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(podCIDRs).To(ConsistOf(config.NetworkConfig.ClusterNetworkCIDRBlock))

			svcCIDRs, found, err := unstructured.NestedStringSlice(cluster.Object, "spec", "clusterNetwork", "services", "cidrBlocks")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(svcCIDRs).To(ConsistOf(config.NetworkConfig.ServiceNetworkCIDRBlock))

			serviceDomain, found, err := unstructured.NestedString(cluster.Object, "spec", "clusterNetwork", "serviceDomain")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(serviceDomain).To(Equal("cluster.local"))

			// Verify infrastructure ref
			infraRefKind, found, err := unstructured.NestedString(cluster.Object, "spec", "infrastructureRef", "kind")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(infraRefKind).To(Equal("OCICluster"))

			infraRefName, found, err := unstructured.NestedString(cluster.Object, "spec", "infrastructureRef", "name")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(infraRefName).To(Equal("test-cluster"))

			infraRefAPIGroup, found, err := unstructured.NestedString(cluster.Object, "spec", "infrastructureRef", "apiGroup")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(infraRefAPIGroup).To(Equal("infrastructure.cluster.x-k8s.io"))

			_, found, err = unstructured.NestedString(cluster.Object, "spec", "infrastructureRef", "namespace")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse())
		})

		It("should reject invalid infrastructure apiVersions early", func() {
			_, err := infrastructureRef("v1beta2", ociClusterKind, "test-namespace", "test-cluster")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must include an API group"))
		})

		It("should reject invalid pod CIDRs early", func() {
			cfg := config
			cfg.NetworkConfig.ClusterNetworkCIDRBlock = "not-a-cidr"

			_, mutateFn := CAPICluster("oci-openshift-autoscaling-operator", "test-cluster", instance, cfg)
			err := mutateFn()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("clusterNetwork.pods.cidrBlocks[0] must be a valid CIDR"))
		})

		It("should reject empty service CIDRs early", func() {
			cfg := config
			cfg.NetworkConfig.ServiceNetworkCIDRBlock = "   "

			_, mutateFn := CAPICluster("oci-openshift-autoscaling-operator", "test-cluster", instance, cfg)
			err := mutateFn()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("clusterNetwork.services.cidrBlocks[0] must not be empty"))
		})
	})

	Context("OCIMachineTemplate", func() {
		It("should append pool identifier to OCIMachineTemplate name and OCI tags", func() {
			instance.Spec.Autoscaling.PoolIdentifier = "bm01"
			obj, mutateFn := OCIMachineTemplate("oci-openshift-autoscaling-operator", "test-cluster", instance, config)
			template := obj.(*infrastructurev1beta2.OCIMachineTemplate)

			Expect(template.Name).To(Equal("test-cluster-bm01-autoscaling"))

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())
			Expect(template.Spec.Template.Spec.FreeformTags).To(HaveKeyWithValue(utils.OCIInstanceAutoscalerClusterNameTag, "test-cluster"))
			Expect(template.Spec.Template.Spec.FreeformTags).To(HaveKeyWithValue(utils.OCIInstanceAutoscalerPoolTag, "bm01"))
		})

		It("should reject node pool names longer than the generated name budget", func() {
			_, mutateFn := OCIMachineTemplate("oci-openshift-autoscaling-operator", strings.Repeat("a", 52), instance, config)

			err := mutateFn()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("at most 51 characters"))
		})

		It("should fail when bare metal subnet ID is empty for BM shapes", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			cfg.NetworkConfig.BareMetalSubnetID = ""
			cfg.NetworkConfig.BareMetalSubnetName = "private-secondary"
			obj, mutateFn := OCIMachineTemplate("oci-openshift-autoscaling-operator", "test-cluster", instance, cfg)
			template := obj.(*infrastructurev1beta2.OCIMachineTemplate)

			err := mutateFn()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bare metal subnet ID must not be empty for BM shapes"))
			Expect(template.Spec.Template.Spec.VnicAttachments).To(BeEmpty())
		})

		It("should fail when bare metal subnet name is empty for BM shapes", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			cfg.NetworkConfig.BareMetalSubnetID = "test-bm-subnet"
			cfg.NetworkConfig.BareMetalSubnetName = ""
			obj, mutateFn := OCIMachineTemplate("oci-openshift-autoscaling-operator", "test-cluster", instance, cfg)
			template := obj.(*infrastructurev1beta2.OCIMachineTemplate)

			err := mutateFn()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bare metal subnet name must not be empty for BM shapes"))
			Expect(template.Spec.Template.Spec.VnicAttachments).To(BeEmpty())
		})

		It("should resolve subnet names for BM shapes", func() {
			primary, secondary := ResolveSubnetNamesForShape("BM.Standard3.64", "primary-subnet", "bm-subnet")
			Expect(primary).To(Equal("bm-subnet"))
			Expect(secondary).To(Equal("primary-subnet"))
		})

		It("should resolve subnet names for non-BM shapes", func() {
			primary, secondary := ResolveSubnetNamesForShape("VM.Standard.E4.Flex", "primary-subnet", "bm-subnet")
			Expect(primary).To(Equal("primary-subnet"))
			Expect(secondary).To(Equal(""))
		})

		It("should fail when bare metal subnet matches primary subnet", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			cfg.NetworkConfig.BareMetalSubnetID = cfg.NetworkConfig.OCPSubnetID
			cfg.NetworkConfig.BareMetalSubnetName = cfg.NetworkConfig.OCPSubnetName
			obj, mutateFn := OCIMachineTemplate("oci-openshift-autoscaling-operator", "test-cluster", instance, cfg)
			template := obj.(*infrastructurev1beta2.OCIMachineTemplate)

			err := mutateFn()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bare metal subnet must differ from primary subnet"))
			Expect(template.Spec.Template.Spec.VnicAttachments).To(BeEmpty())
		})
		It("should create OCIMachineTemplate with correct configuration", func() {
			obj, mutateFn := OCIMachineTemplate("oci-openshift-autoscaling-operator", "test-cluster", instance, config)
			template, ok := obj.(*infrastructurev1beta2.OCIMachineTemplate)
			Expect(ok).To(BeTrue(), "Object should be an OCIMachineTemplate")

			// Verify initial state
			Expect(template.Name).To(Equal("test-cluster-autoscaling"))
			Expect(template.Namespace).To(Equal("oci-openshift-autoscaling-operator"))

			// Apply mutation
			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			// Verify spec
			Expect(template.Spec.Template.Spec.ImageId).To(Equal(config.AutoScalingConfig.ImageID))
			Expect(template.Spec.Template.Spec.Shape).To(Equal(config.AutoScalingConfig.Shape))
			Expect(template.Spec.Template.Spec.ShapeConfig.Ocpus).To(Equal("2"))
			Expect(template.Spec.Template.Spec.ShapeConfig.MemoryInGBs).To(Equal("4"))
			Expect(template.Spec.Template.Spec.IsPvEncryptionInTransitEnabled).To(BeFalse())
			Expect(template.Spec.Template.Spec.SubnetName).To(Equal("private"))
			Expect(template.Spec.Template.Spec.DefinedTags).To(BeEmpty())
			Expect(template.Spec.Template.Spec.FreeformTags).To(HaveKeyWithValue(utils.OCIInstanceManagedByTag, instance.Name))
			Expect(template.Spec.Template.Spec.FreeformTags).To(HaveKeyWithValue(utils.OCIInstanceAutoscalerNamespaceTag, instance.Namespace))
			Expect(template.Spec.Template.Spec.FreeformTags).To(HaveKeyWithValue(utils.OCIInstanceAutoscalerClusterNameTag, "test-cluster"))
			Expect(template.Spec.Template.Spec.VnicAttachments).To(BeEmpty())
		})

		It("should add a secondary VNIC attachment when SecondarySubnetID is set", func() {
			cfg := config
			cfg.NetworkConfig.BareMetalSubnetID = "test-bm-subnet"
			cfg.NetworkConfig.BareMetalSubnetName = "private-secondary"
			// For VNIC attachment, ensure BM shape so we only attach on Bare Metal
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			obj, mutateFn := OCIMachineTemplate("oci-openshift-autoscaling-operator", "test-cluster", instance, cfg)
			template := obj.(*infrastructurev1beta2.OCIMachineTemplate)

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			attachments := template.Spec.Template.Spec.VnicAttachments
			Expect(attachments).To(HaveLen(1))
			Expect(attachments[0].SubnetName).To(Equal("private"))
			Expect(attachments[0].AssignPublicIp).To(BeFalse())
			Expect(attachments[0].DisplayName).NotTo(BeNil())
			Expect(*attachments[0].DisplayName).To(Equal("vnic_ocp"))
			Expect(attachments[0].NicIndex).NotTo(BeNil())
			Expect(*attachments[0].NicIndex).To(Equal(1))
			Expect(template.Spec.Template.Spec.DefinedTags).To(HaveKeyWithValue("openshift-tags", map[string]string{
				"openshift-resource": "openshift-resource-infra",
			}))
		})

		It("should merge bare metal boot-volume tags into existing namespace tags", func() {
			cfg := config
			cfg.NetworkConfig.BareMetalSubnetID = "test-bm-subnet"
			cfg.NetworkConfig.BareMetalSubnetName = "private-secondary"
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			obj, mutateFn := OCIMachineTemplate("oci-openshift-autoscaling-operator", "test-cluster", instance, cfg)
			template := obj.(*infrastructurev1beta2.OCIMachineTemplate)
			template.Spec.Template.Spec.DefinedTags = map[string]map[string]string{
				"openshift-tags": {
					"existing-key": "existing-value",
				},
				"other-namespace": {
					"other-key": "other-value",
				},
			}

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())
			Expect(template.Spec.Template.Spec.DefinedTags).To(HaveKeyWithValue("openshift-tags", map[string]string{
				"existing-key":       "existing-value",
				"openshift-resource": "openshift-resource-infra",
			}))
			Expect(template.Spec.Template.Spec.DefinedTags).To(HaveKeyWithValue("other-namespace", map[string]string{
				"other-key": "other-value",
			}))
		})

		It("should reject conflicting bare metal boot-volume tags", func() {
			cfg := config
			cfg.NetworkConfig.BareMetalSubnetID = "test-bm-subnet"
			cfg.NetworkConfig.BareMetalSubnetName = "private-secondary"
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			obj, mutateFn := OCIMachineTemplate("oci-openshift-autoscaling-operator", "test-cluster", instance, cfg)
			template := obj.(*infrastructurev1beta2.OCIMachineTemplate)
			template.Spec.Template.Spec.DefinedTags = map[string]map[string]string{
				"openshift-tags": {
					"openshift-resource": "unexpected-value",
				},
			}

			err := mutateFn()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(`defined tag openshift-tags/openshift-resource must be "openshift-resource-infra"`))
		})

		It("should NOT add a secondary VNIC for non-BM shapes even if SecondarySubnetID is set", func() {
			cfg := config
			cfg.NetworkConfig.BareMetalSubnetID = "test-bm-subnet"
			cfg.AutoScalingConfig.Shape = "VM.Standard.E4.Flex"
			obj, mutateFn := OCIMachineTemplate("oci-openshift-autoscaling-operator", "test-cluster", instance, cfg)
			template := obj.(*infrastructurev1beta2.OCIMachineTemplate)

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			Expect(template.Spec.Template.Spec.VnicAttachments).To(BeEmpty())
			Expect(template.Spec.Template.Spec.DefinedTags).To(BeEmpty())
		})

		It("should omit ShapeConfig when using Bare Metal shape", func() {
			cfg := config
			cfg.AutoScalingConfig.Shape = "BM.Standard3.64"
			cfg.NetworkConfig.BareMetalSubnetID = "test-bm-subnet"
			cfg.NetworkConfig.BareMetalSubnetName = "private-secondary"

			obj, mutateFn := OCIMachineTemplate("oci-openshift-autoscaling-operator", "test-cluster", instance, cfg)
			template := obj.(*infrastructurev1beta2.OCIMachineTemplate)

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			Expect(template.Spec.Template.Spec.Shape).To(Equal("BM.Standard3.64"))
			Expect(template.Spec.Template.Spec.ShapeConfig.Ocpus).To(Equal(""))
			Expect(template.Spec.Template.Spec.ShapeConfig.MemoryInGBs).To(Equal(""))
		})
	})

	Context("MachineDeployment", func() {
		It("should append pool identifier to MachineDeployment resource names without changing CAPI cluster name", func() {
			instance.Spec.Autoscaling.PoolIdentifier = "bm01"
			obj, mutateFn := MachineDeployment("oci-openshift-autoscaling-operator", "test-cluster", instance, config)
			deployment := obj.(*unstructured.Unstructured)

			Expect(deployment.GetName()).To(Equal("test-cluster-bm01"))

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			clusterName, found, err := unstructured.NestedString(deployment.Object, "spec", "clusterName")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(clusterName).To(Equal("test-cluster"))

			selectorLabels, found, err := unstructured.NestedStringMap(deployment.Object, "spec", "selector", "matchLabels")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(selectorLabels).To(HaveKeyWithValue("cluster.x-k8s.io/cluster-name", "test-cluster"))
			Expect(selectorLabels).To(HaveKeyWithValue("cluster.x-k8s.io/deployment-name", "test-cluster-bm01"))

			templateClusterName, found, err := unstructured.NestedString(deployment.Object, "spec", "template", "spec", "clusterName")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(templateClusterName).To(Equal("test-cluster"))

			dataSecretName, found, err := unstructured.NestedString(deployment.Object, "spec", "template", "spec", "bootstrap", "dataSecretName")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(dataSecretName).To(Equal("test-cluster-bootstrap"))

			infraRefName, found, err := unstructured.NestedString(deployment.Object, "spec", "template", "spec", "infrastructureRef", "name")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(infraRefName).To(Equal("test-cluster-bm01-autoscaling"))
		})

		It("should reject generated node pool names longer than the generated name budget", func() {
			instance.Spec.Autoscaling.PoolIdentifier = "pool1"
			_, mutateFn := MachineDeployment("oci-openshift-autoscaling-operator", strings.Repeat("a", 47), instance, config)

			err := mutateFn()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("at most 51 characters"))
		})

		It("should create MachineDeployment with correct configuration", func() {
			obj, mutateFn := MachineDeployment("oci-openshift-autoscaling-operator", "test-cluster", instance, config)
			deployment, ok := obj.(*unstructured.Unstructured)
			Expect(ok).To(BeTrue(), "Object should be a MachineDeployment")

			// Verify initial state
			Expect(deployment.GetName()).To(Equal("test-cluster"))
			Expect(deployment.GetNamespace()).To(Equal("oci-openshift-autoscaling-operator"))
			Expect(deployment.GetAPIVersion()).To(Equal("cluster.x-k8s.io/v1beta2"))

			// Apply mutation
			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			// Verify labels
			defaultLabels := utils.GetDefaultLabels(instance.Name)
			Expect(deployment.GetLabels()).To(Equal(defaultLabels))

			// Verify annotations
			Expect(deployment.GetAnnotations()).To(HaveKeyWithValue("capacity.cluster-autoscaler.kubernetes.io/cpu", "2"))
			Expect(deployment.GetAnnotations()).To(HaveKeyWithValue("capacity.cluster-autoscaler.kubernetes.io/labels", "node-role.kubernetes.io/worker="))
			Expect(deployment.GetAnnotations()).To(HaveKeyWithValue("capacity.cluster-autoscaler.kubernetes.io/memory", "4G"))
			Expect(deployment.GetAnnotations()).To(HaveKeyWithValue("cluster.x-k8s.io/cluster-api-autoscaler-node-group-min-size", "1"))
			Expect(deployment.GetAnnotations()).To(HaveKeyWithValue("cluster.x-k8s.io/cluster-api-autoscaler-node-group-max-size", "3"))

			// Verify spec
			clusterName, found, err := unstructured.NestedString(deployment.Object, "spec", "clusterName")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(clusterName).To(Equal("test-cluster"))

			replicas, found, err := unstructured.NestedInt64(deployment.Object, "spec", "replicas")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(replicas).To(Equal(int64(1)))

			selectorLabels, found, err := unstructured.NestedStringMap(deployment.Object, "spec", "selector", "matchLabels")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(selectorLabels).To(HaveKeyWithValue("cluster.x-k8s.io/cluster-name", "test-cluster"))
			Expect(selectorLabels).To(HaveKeyWithValue("cluster.x-k8s.io/deployment-name", "test-cluster"))

			templateLabels, found, err := unstructured.NestedStringMap(deployment.Object, "spec", "template", "metadata", "labels")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(templateLabels).To(HaveKeyWithValue("cluster.x-k8s.io/cluster-name", "test-cluster"))
			Expect(templateLabels).To(HaveKeyWithValue("cluster.x-k8s.io/deployment-name", "test-cluster"))
			Expect(templateLabels).To(HaveKeyWithValue(utils.ManagedByLabel, instance.Name))

			templateClusterName, found, err := unstructured.NestedString(deployment.Object, "spec", "template", "spec", "clusterName")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(templateClusterName).To(Equal("test-cluster"))

			dataSecretName, found, err := unstructured.NestedString(deployment.Object, "spec", "template", "spec", "bootstrap", "dataSecretName")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(dataSecretName).To(Equal("test-cluster-bootstrap"))

			// Verify infrastructure ref
			infraRefKind, found, err := unstructured.NestedString(deployment.Object, "spec", "template", "spec", "infrastructureRef", "kind")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(infraRefKind).To(Equal("OCIMachineTemplate"))

			infraRefName, found, err := unstructured.NestedString(deployment.Object, "spec", "template", "spec", "infrastructureRef", "name")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(infraRefName).To(Equal("test-cluster-autoscaling"))

			infraRefAPIGroup, found, err := unstructured.NestedString(deployment.Object, "spec", "template", "spec", "infrastructureRef", "apiGroup")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(infraRefAPIGroup).To(Equal("infrastructure.cluster.x-k8s.io"))

			_, found, err = unstructured.NestedString(deployment.Object, "spec", "template", "spec", "infrastructureRef", "namespace")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse())

			_, found, err = unstructured.NestedMap(deployment.Object, "spec", "rollout")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse())
		})

		It("should disable surge rollout for bare metal MachineDeployment", func() {
			config.AutoScalingConfig.Shape = "BM.Standard3.64"
			obj, mutateFn := MachineDeployment("oci-openshift-autoscaling-operator", "test-cluster", instance, config)
			deployment := obj.(*unstructured.Unstructured)

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			strategyType, found, err := unstructured.NestedString(deployment.Object, "spec", "rollout", "strategy", "type")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(strategyType).To(Equal("RollingUpdate"))

			maxSurge, found, err := unstructured.NestedInt64(deployment.Object, "spec", "rollout", "strategy", "rollingUpdate", "maxSurge")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(maxSurge).To(Equal(int64(0)))

			maxUnavailable, found, err := unstructured.NestedInt64(deployment.Object, "spec", "rollout", "strategy", "rollingUpdate", "maxUnavailable")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(maxUnavailable).To(Equal(int64(1)))
		})

		It("should preserve existing annotations", func() {
			obj, mutateFn := MachineDeployment("oci-openshift-autoscaling-operator", "test-cluster", instance, config)
			deployment := obj.(*unstructured.Unstructured)

			// Add existing annotation
			deployment.SetAnnotations(map[string]string{
				"existing-key": "existing-value",
				"cluster.x-k8s.io/cluster-api-autoscaler-node-group-min-size": "0",
				"cluster.x-k8s.io/cluster-api-autoscaler-node-group-max-size": "1",
			})
			deployment.Object["spec"] = map[string]interface{}{
				"replicas": int64(2),
			}

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			Expect(deployment.GetAnnotations()).To(HaveKeyWithValue("existing-key", "existing-value"))
			Expect(deployment.GetAnnotations()).To(HaveKeyWithValue("capacity.cluster-autoscaler.kubernetes.io/cpu", "2"))
			Expect(deployment.GetAnnotations()).To(HaveKeyWithValue("cluster.x-k8s.io/cluster-api-autoscaler-node-group-min-size", "1"))
			Expect(deployment.GetAnnotations()).To(HaveKeyWithValue("cluster.x-k8s.io/cluster-api-autoscaler-node-group-max-size", "3"))

			replicas, found, err := unstructured.NestedInt64(deployment.Object, "spec", "replicas")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(replicas).To(Equal(int64(2)))
		})

		It("should leave existing replicas for cluster-autoscaler to manage", func() {
			obj, mutateFn := MachineDeployment("oci-openshift-autoscaling-operator", "test-cluster", instance, config)
			deployment := obj.(*unstructured.Unstructured)

			deployment.Object["spec"] = map[string]interface{}{
				"replicas": int64(5),
			}

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			replicas, found, err := unstructured.NestedInt64(deployment.Object, "spec", "replicas")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(replicas).To(Equal(int64(5)))

			deployment.Object["spec"] = map[string]interface{}{
				"replicas": int64(0),
			}

			err = mutateFn()
			Expect(err).NotTo(HaveOccurred())

			replicas, found, err = unstructured.NestedInt64(deployment.Object, "spec", "replicas")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(replicas).To(Equal(int64(0)))
		})

	})

	Context("MachineHealthCheck", func() {
		It("should append pool identifier to MachineHealthCheck name and selector", func() {
			instance.Spec.Autoscaling.PoolIdentifier = "bm01"
			obj, mutateFn := MachineHealthCheck("oci-openshift-autoscaling-operator", "test-cluster", instance, config)
			mhc := obj.(*unstructured.Unstructured)

			Expect(mhc.GetName()).To(Equal("test-cluster-bm01-autoscaling"))

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			clusterName, found, err := unstructured.NestedString(mhc.Object, "spec", "clusterName")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(clusterName).To(Equal("test-cluster"))

			matchLabels, found, err := unstructured.NestedStringMap(mhc.Object, "spec", "selector", "matchLabels")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(matchLabels).To(HaveKeyWithValue("cluster.x-k8s.io/cluster-name", "test-cluster"))
			Expect(matchLabels).To(HaveKeyWithValue("cluster.x-k8s.io/deployment-name", "test-cluster-bm01"))
		})

		It("should create MachineHealthCheck with correct configuration", func() {
			obj, mutateFn := MachineHealthCheck("oci-openshift-autoscaling-operator", "test-cluster", instance, config)
			mhc, ok := obj.(*unstructured.Unstructured)
			Expect(ok).To(BeTrue(), "Object should be a MachineHealthCheck")

			Expect(mhc.GetName()).To(Equal("test-cluster-autoscaling"))
			Expect(mhc.GetNamespace()).To(Equal("oci-openshift-autoscaling-operator"))
			Expect(mhc.GetAPIVersion()).To(Equal("cluster.x-k8s.io/v1beta2"))
			Expect(mhc.GetKind()).To(Equal("MachineHealthCheck"))

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			defaultLabels := utils.GetDefaultLabels(instance.Name)
			Expect(mhc.GetLabels()).To(Equal(defaultLabels))

			clusterName, found, err := unstructured.NestedString(mhc.Object, "spec", "clusterName")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(clusterName).To(Equal("test-cluster"))

			matchLabels, found, err := unstructured.NestedStringMap(mhc.Object, "spec", "selector", "matchLabels")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(matchLabels).To(HaveKeyWithValue("cluster.x-k8s.io/cluster-name", "test-cluster"))
			Expect(matchLabels).To(HaveKeyWithValue("cluster.x-k8s.io/deployment-name", "test-cluster"))

			nodeStartupTimeoutSeconds, found, err := unstructured.NestedInt64(mhc.Object, "spec", "checks", "nodeStartupTimeoutSeconds")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(nodeStartupTimeoutSeconds).To(Equal(int64(600)))

			unhealthyNodeConditions, found, err := unstructured.NestedSlice(mhc.Object, "spec", "checks", "unhealthyNodeConditions")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(unhealthyNodeConditions).To(HaveLen(2))

			remediationThreshold, found, err := unstructured.NestedString(mhc.Object, "spec", "remediation", "triggerIf", "unhealthyLessThanOrEqualTo")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(remediationThreshold).To(Equal("100%"))
		})

		It("should use the bare metal startup timeout for MachineHealthCheck", func() {
			config.AutoScalingConfig.Shape = "BM.Standard3.64"
			obj, mutateFn := MachineHealthCheck("oci-openshift-autoscaling-operator", "test-cluster", instance, config)
			mhc := obj.(*unstructured.Unstructured)

			err := mutateFn()
			Expect(err).NotTo(HaveOccurred())

			nodeStartupTimeoutSeconds, found, err := unstructured.NestedInt64(mhc.Object, "spec", "checks", "nodeStartupTimeoutSeconds")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(nodeStartupTimeoutSeconds).To(Equal(int64(1200)))
		})
	})

	Context("ValidateMinMaxNodes", func() {
		It("should pass with valid min/max values", func() {
			err := ValidateMinMaxNodes(instance, config)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should fail when min > max", func() {
			config.AutoScalingConfig.MinNodes = 5
			config.AutoScalingConfig.MaxNodes = 3

			err := ValidateMinMaxNodes(instance, config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("min nodes must be less than max nodes"))
		})

		It("should fail with negative min nodes", func() {
			config.AutoScalingConfig.MinNodes = -1

			err := ValidateMinMaxNodes(instance, config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("min nodes must be equal to or greater than 0"))
		})

		It("should fail with negative max nodes", func() {
			config.AutoScalingConfig.MaxNodes = -1

			err := ValidateMinMaxNodes(instance, config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("max nodes must be equal to or greater than 0"))
		})

		It("should use instance values over config values", func() {
			instance.Spec.Autoscaling.MinNodes = ptr.To[int32](2)
			instance.Spec.Autoscaling.MaxNodes = ptr.To[int32](4)

			err := ValidateMinMaxNodes(instance, config)
			Expect(err).NotTo(HaveOccurred())

			instance.Spec.Autoscaling.MinNodes = ptr.To[int32](5)
			instance.Spec.Autoscaling.MaxNodes = ptr.To[int32](3)

			err = ValidateMinMaxNodes(instance, config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("min nodes must be less than max nodes"))
		})
	})

	Context("summarizeVnicAttachments", func() {
		It("should summarize attachments with subnet name and NIC index", func() {
			attachments := []infrastructurev1beta2.VnicAttachment{
				{
					SubnetName: "subnet-a",
					NicIndex:   swag.Int(1),
				},
				{
					SubnetName: "subnet-b",
				},
			}

			summary := summarizeVnicAttachments(attachments)
			Expect(summary).To(HaveLen(2))
			Expect(summary[0].Index).To(Equal(0))
			Expect(summary[0].SubnetName).To(Equal("subnet-a"))
			Expect(summary[0].NicIndex).NotTo(BeNil())
			Expect(*summary[0].NicIndex).To(Equal(1))
			Expect(summary[1].Index).To(Equal(1))
			Expect(summary[1].SubnetName).To(Equal("subnet-b"))
			Expect(summary[1].NicIndex).To(BeNil())
		})
	})
})
