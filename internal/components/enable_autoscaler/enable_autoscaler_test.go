/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package enableautoscaler

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ocicapioperatorv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	infrastructurev1beta2 "github.com/oracle/cluster-api-provider-oci/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var _ = Describe("Enable Autoscaler", func() {
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
		}

		config = Config{
			ClusterConfig: ClusterConfig{
				CompartmentID: "test-compartment",
			},
			NetworkConfig: NetworkConfig{
				VCNID:                   "test-vcn",
				OCPSubnetID:             "test-ocp-subnet",
				OCPSubnetName:           "private",
				BareMetalSubnetID:       "test-bm-subnet",
				BareMetalSubnetName:     "private-secondary",
				NetworkSecurityGroupID:  "test-nsg",
				ControlPlaneEndpoint:    "test-endpoint",
				APIServerLoadBalancerID: "test-lb",
				ClusterNetworkCIDRBlock: "10.0.0.0/16",
				ServiceNetworkCIDRBlock: "10.1.0.0/16",
			},
			AutoScalingConfig: AutoScalingConfig{
				CPUs:     2,
				Memory:   4,
				MinNodes: 1,
				MaxNodes: 3,
				Shape:    "oc3",
				ImageID:  "test-image",
			},
		}
	})

	Context("GetComponents", func() {
		It("should return component with all subcomponents", func() {
			mockClient := &MockClient{}
			component := GetComponents(ctx, mockClient, "oci-openshift-autoscaling-operator", "capi-provider", "test-cluster", "capi-sa", instance, config, false)

			Expect(component.Name).To(Equal("EnableAutoscaler"))
			Expect(component.Subcomponents).To(HaveLen(8))

			// Verify ManagedResourceNamespace subcomponent
			namespace := component.Subcomponents[0]
			Expect(namespace.Name).To(Equal("managedResourceNamespace"))
			_, ok := namespace.Object.(*corev1.Namespace)
			Expect(ok).To(BeTrue())
			Expect(namespace.MutateFn).NotTo(BeNil())

			// Verify BootstrapConfigSecret subcomponent
			bootstrapSecret := component.Subcomponents[1]
			Expect(bootstrapSecret.Name).To(Equal("bootstrapConfigSecret"))
			_, ok = bootstrapSecret.Object.(*corev1.Secret)
			Expect(ok).To(BeTrue())
			Expect(bootstrapSecret.MutateFn).NotTo(BeNil())

			// Verify KubeConfigSecret subcomponent
			kubeConfigSecret := component.Subcomponents[2]
			Expect(kubeConfigSecret.Name).To(Equal("kubeConfigSecret"))
			_, ok = kubeConfigSecret.Object.(*corev1.Secret)
			Expect(ok).To(BeTrue())
			Expect(kubeConfigSecret.MutateFn).NotTo(BeNil())

			// Verify MachineTemplate subcomponent
			machineTemplate := component.Subcomponents[3]
			Expect(machineTemplate.Name).To(Equal("machineTemplate"))
			_, ok = machineTemplate.Object.(*infrastructurev1beta2.OCIMachineTemplate)
			Expect(ok).To(BeTrue())
			Expect(machineTemplate.MutateFn).NotTo(BeNil())

			// Verify MachineDeployment subcomponent
			machineDeployment := component.Subcomponents[4]
			Expect(machineDeployment.Name).To(Equal("machineDeployment"))
			_, ok = machineDeployment.Object.(*unstructured.Unstructured)
			Expect(ok).To(BeTrue())
			Expect(machineDeployment.MutateFn).NotTo(BeNil())

			// Verify MachineHealthCheck subcomponent
			machineHealthCheck := component.Subcomponents[5]
			Expect(machineHealthCheck.Name).To(Equal("machineHealthCheck"))
			_, ok = machineHealthCheck.Object.(*unstructured.Unstructured)
			Expect(ok).To(BeTrue())
			Expect(machineHealthCheck.MutateFn).NotTo(BeNil())

			// Verify OCICluster subcomponent
			ociCluster := component.Subcomponents[6]
			Expect(ociCluster.Name).To(Equal("ociCluster"))
			_, ok = ociCluster.Object.(*infrastructurev1beta2.OCICluster)
			Expect(ok).To(BeTrue())
			Expect(ociCluster.MutateFn).NotTo(BeNil())

			// Verify CAPICluster subcomponent
			capiCluster := component.Subcomponents[7]
			Expect(capiCluster.Name).To(Equal("cluster"))
			_, ok = capiCluster.Object.(*unstructured.Unstructured)
			Expect(ok).To(BeTrue())
			Expect(capiCluster.MutateFn).NotTo(BeNil())
		})

		It("should create components with correct names and namespaces", func() {
			mockClient := &MockClient{}
			component := GetComponents(ctx, mockClient, "custom-ns", "capi-provider-ns", "custom-cluster", "custom-sa", instance, config, false)

			managedNamespace := component.Subcomponents[0].Object.(*corev1.Namespace)
			Expect(managedNamespace.Name).To(Equal("custom-ns"))

			// Check OCICluster
			ociCluster := component.Subcomponents[6].Object.(*infrastructurev1beta2.OCICluster)
			Expect(ociCluster.Name).To(Equal("custom-cluster"))
			Expect(ociCluster.Namespace).To(Equal("custom-ns"))

			// Check CAPICluster
			capiCluster := component.Subcomponents[7].Object.(*unstructured.Unstructured)
			Expect(capiCluster.GetName()).To(Equal("custom-cluster"))
			Expect(capiCluster.GetNamespace()).To(Equal("custom-ns"))

			// Check MachineTemplate
			machineTemplate := component.Subcomponents[3].Object.(*infrastructurev1beta2.OCIMachineTemplate)
			Expect(machineTemplate.Name).To(Equal("custom-cluster-autoscaling"))
			Expect(machineTemplate.Namespace).To(Equal("custom-ns"))

			// Check MachineDeployment
			machineDeployment := component.Subcomponents[4].Object.(*unstructured.Unstructured)
			Expect(machineDeployment.GetName()).To(Equal("custom-cluster"))
			Expect(machineDeployment.GetNamespace()).To(Equal("custom-ns"))

			// Check MachineHealthCheck
			machineHealthCheck := component.Subcomponents[5].Object.(*unstructured.Unstructured)
			Expect(machineHealthCheck.GetName()).To(Equal("custom-cluster-autoscaling"))
			Expect(machineHealthCheck.GetNamespace()).To(Equal("custom-ns"))
		})

		It("should append pool identifier to node pool component names only", func() {
			instance.Spec.Autoscaling.PoolIdentifier = "vm01"
			mockClient := &MockClient{}
			component := GetComponents(ctx, mockClient, "custom-ns", "capi-provider-ns", "custom-cluster", "custom-sa", instance, config, false)

			ociCluster := component.Subcomponents[6].Object.(*infrastructurev1beta2.OCICluster)
			Expect(ociCluster.Name).To(Equal("custom-cluster"))

			capiCluster := component.Subcomponents[7].Object.(*unstructured.Unstructured)
			Expect(capiCluster.GetName()).To(Equal("custom-cluster"))

			machineTemplate := component.Subcomponents[3].Object.(*infrastructurev1beta2.OCIMachineTemplate)
			Expect(machineTemplate.Name).To(Equal("custom-cluster-vm01-autoscaling"))

			machineDeployment := component.Subcomponents[4].Object.(*unstructured.Unstructured)
			Expect(machineDeployment.GetName()).To(Equal("custom-cluster-vm01"))

			machineHealthCheck := component.Subcomponents[5].Object.(*unstructured.Unstructured)
			Expect(machineHealthCheck.GetName()).To(Equal("custom-cluster-vm01-autoscaling"))
		})

		It("should add OCIClusterIdentity when instance principal is enabled", func() {
			mockClient := &MockClient{}
			component := GetComponents(ctx, mockClient, "oci-openshift-autoscaling-operator", "capi-provider", "test-cluster", "capi-sa", instance, config, true)

			Expect(component.Subcomponents).To(HaveLen(9))

			identity := component.Subcomponents[8]
			Expect(identity.Name).To(Equal("ociClusterIdentity"))
			obj, ok := identity.Object.(*infrastructurev1beta2.OCIClusterIdentity)
			Expect(ok).To(BeTrue())
			Expect(obj.Name).To(Equal("test-cluster-identity"))
			Expect(obj.Namespace).To(Equal("oci-openshift-autoscaling-operator"))
		})
	})
})
