/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package enableautoscaler

import (
	"fmt"
	"net"
	"strings"

	"github.com/go-openapi/swag"
	ocicapioperatorv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	applog "github.com/openshift/oci-capi-operator/internal/logging"
	"github.com/openshift/oci-capi-operator/internal/utils"
	infrastructurev1beta2 "github.com/oracle/cluster-api-provider-oci/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	capiv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	secondaryVnicIndex  = 1
	bootVolumeTypeTag   = "boot-volume-type"
	bootVolumeTypeIscsi = "ISCSI"
	vnicOcpDisplayName  = "vnic_ocp"
	openshiftTagKey     = "openshift-resource"
	openshiftTagValue   = "openshift-resource-infra"
	openshiftTagNS      = "openshift-tags"
	workerRoleName      = "worker"
	assignPrivateDNS    = true
)

const (
	skipAPIServerLBManagementAnnotation = "cluster.x-k8s.io/skip-apiserver-lb-management"
	capiClusterAPIVersion               = "cluster.x-k8s.io/v1beta2"
	ociInfrastructureAPIVersion         = "infrastructure.cluster.x-k8s.io/v1beta2"
	ociClusterIdentityKind              = "OCIClusterIdentity"
	ociClusterKind                      = "OCICluster"
	ociMachineTemplateKind              = "OCIMachineTemplate"
	clusterServiceDomain                = "cluster.local"
	autoscalingNameFormat               = "%s-autoscaling"
	bootstrapSecretNameFormat           = "%s-bootstrap"
	capiClusterNameLabel                = "cluster.x-k8s.io/cluster-name"
	capiDeploymentNameLabel             = "cluster.x-k8s.io/deployment-name"
	cpuCapacityAnnotation               = "capacity.cluster-autoscaler.kubernetes.io/cpu"
	nodeLabelsAnnotation                = "capacity.cluster-autoscaler.kubernetes.io/labels"
	memoryCapacityAnnotation            = "capacity.cluster-autoscaler.kubernetes.io/memory"
	nodeGroupMinSizeAnnotation          = "cluster.x-k8s.io/cluster-api-autoscaler-node-group-min-size"
	nodeGroupMaxSizeAnnotation          = "cluster.x-k8s.io/cluster-api-autoscaler-node-group-max-size"
	memoryCapacityFormat                = "%dG"
	bareMetalNodeStartupTimeoutSeconds  = int64(1200)
	defaultNodeStartupTimeoutSeconds    = int64(600)
	unhealthyNodeTimeoutSeconds         = int64(300)
	workerRoleLabelValue                = "node-role.kubernetes.io/worker="
	maxDNS1123LabelLength               = 63
	maxNodePoolNameLength               = maxDNS1123LabelLength - len("-autoscaling")
)

func identityName(clusterName string) string {
	return fmt.Sprintf("%s-identity", clusterName)
}

func autoscalingName(clusterName string) string {
	return fmt.Sprintf(autoscalingNameFormat, clusterName)
}

func NodePoolName(clusterName string, instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler) string {
	poolIdentifier := ""
	if instance != nil {
		poolIdentifier = strings.TrimSpace(instance.Spec.Autoscaling.PoolIdentifier)
	}
	if poolIdentifier == "" {
		return clusterName
	}
	return fmt.Sprintf("%s-%s", clusterName, poolIdentifier)
}

func AutoscalingResourceName(clusterName string, instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler) string {
	return autoscalingName(NodePoolName(clusterName, instance))
}

func ValidateNodePoolName(nodePoolName string) error {
	if len(nodePoolName) > maxNodePoolNameLength {
		return fmt.Errorf("node pool name %q must be at most %d characters so generated CAPI resource names stay within %d characters", nodePoolName, maxNodePoolNameLength, maxDNS1123LabelLength)
	}
	if err := validateDNS1123Label("node pool name", nodePoolName); err != nil {
		return err
	}
	return nil
}

func validateGeneratedNodePoolNames(clusterName string, instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler) error {
	nodePoolName := NodePoolName(clusterName, instance)
	if err := ValidateNodePoolName(nodePoolName); err != nil {
		return err
	}
	if err := validateDNS1123Label("autoscaling resource name", autoscalingName(nodePoolName)); err != nil {
		return err
	}
	return nil
}

func validateDNS1123Label(field, value string) error {
	if errs := validation.IsDNS1123Label(value); len(errs) > 0 {
		return fmt.Errorf("%s %q must be a valid DNS-1123 label: %s", field, value, strings.Join(errs, "; "))
	}
	return nil
}

func machineTemplateLabels(instanceName, clusterName, nodePoolName string) map[string]interface{} {
	labels := map[string]interface{}{}
	for key, value := range utils.GetComponentLabels(instanceName, "EnableAutoscaler", "machine") {
		labels[key] = value
	}
	labels[capiClusterNameLabel] = clusterName
	labels[capiDeploymentNameLabel] = nodePoolName
	return labels
}

func bootstrapSecretName(clusterName string) string {
	return fmt.Sprintf(bootstrapSecretNameFormat, clusterName)
}

func infrastructureRef(apiVersion, kind, _ string, name string) (map[string]interface{}, error) {
	groupVersion, err := schema.ParseGroupVersion(strings.TrimSpace(apiVersion))
	if err != nil {
		return nil, fmt.Errorf("invalid infrastructure apiVersion %q: %w", apiVersion, err)
	}
	if groupVersion.Group == "" {
		return nil, fmt.Errorf("infrastructure apiVersion %q must include an API group", apiVersion)
	}
	if strings.TrimSpace(kind) == "" {
		return nil, fmt.Errorf("infrastructureRef kind must not be empty")
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("infrastructureRef name must not be empty")
	}
	ref := map[string]interface{}{
		"apiGroup": groupVersion.Group,
		"kind":     kind,
		"name":     name,
	}
	return ref, nil
}

func validateCAPIInfrastructureRef(obj *unstructured.Unstructured, path ...string) error {
	ref, found, err := unstructured.NestedMap(obj.Object, path...)
	if err != nil {
		return fmt.Errorf("failed to read infrastructureRef: %w", err)
	}
	if !found {
		return fmt.Errorf("infrastructureRef is required")
	}
	for _, field := range []string{"apiGroup", "kind", "name"} {
		value, ok := ref[field].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("infrastructureRef.%s must not be empty", field)
		}
	}
	if value, found := ref["namespace"]; found {
		namespace, ok := value.(string)
		if !ok || strings.TrimSpace(namespace) == "" {
			return fmt.Errorf("infrastructureRef.namespace must not be empty when set")
		}
	}
	return nil
}

func validateCAPIClusterObject(obj *unstructured.Unstructured) error {
	if err := validateCAPIInfrastructureRef(obj, "spec", "infrastructureRef"); err != nil {
		return err
	}
	podCIDRs, found, err := unstructured.NestedSlice(obj.Object, "spec", "clusterNetwork", "pods", "cidrBlocks")
	if err != nil {
		return fmt.Errorf("failed to read cluster pod CIDRs: %w", err)
	}
	if !found || len(podCIDRs) == 0 {
		return fmt.Errorf("clusterNetwork.pods.cidrBlocks must contain at least one entry")
	}
	if err := validateCIDRBlocks("clusterNetwork.pods.cidrBlocks", podCIDRs); err != nil {
		return err
	}
	serviceCIDRs, found, err := unstructured.NestedSlice(obj.Object, "spec", "clusterNetwork", "services", "cidrBlocks")
	if err != nil {
		return fmt.Errorf("failed to read cluster service CIDRs: %w", err)
	}
	if !found || len(serviceCIDRs) == 0 {
		return fmt.Errorf("clusterNetwork.services.cidrBlocks must contain at least one entry")
	}
	if err := validateCIDRBlocks("clusterNetwork.services.cidrBlocks", serviceCIDRs); err != nil {
		return err
	}
	return nil
}

func validateCIDRBlocks(path string, cidrBlocks []interface{}) error {
	for i, cidrBlock := range cidrBlocks {
		value, ok := cidrBlock.(string)
		if !ok {
			return fmt.Errorf("%s[%d] must be a string", path, i)
		}
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return fmt.Errorf("%s[%d] must not be empty", path, i)
		}
		if _, _, err := net.ParseCIDR(trimmed); err != nil {
			return fmt.Errorf("%s[%d] must be a valid CIDR: %w", path, i, err)
		}
	}
	return nil
}

func validateMachineDeploymentObject(obj *unstructured.Unstructured) error {
	if err := validateCAPIInfrastructureRef(obj, "spec", "template", "spec", "infrastructureRef"); err != nil {
		return err
	}
	clusterName, found, err := unstructured.NestedString(obj.Object, "spec", "clusterName")
	if err != nil {
		return fmt.Errorf("failed to read MachineDeployment spec.clusterName: %w", err)
	}
	if !found || strings.TrimSpace(clusterName) == "" {
		return fmt.Errorf("spec.clusterName must not be empty")
	}
	bootstrapSecret, found, err := unstructured.NestedString(obj.Object, "spec", "template", "spec", "bootstrap", "dataSecretName")
	if err != nil {
		return fmt.Errorf("failed to read MachineDeployment bootstrap secret: %w", err)
	}
	if !found || strings.TrimSpace(bootstrapSecret) == "" {
		return fmt.Errorf("spec.template.spec.bootstrap.dataSecretName must not be empty")
	}
	return nil
}

func mergeDefinedTags(existing map[string]map[string]string, namespace string, tags map[string]string) map[string]map[string]string {
	merged := make(map[string]map[string]string, len(existing)+1)
	for ns, values := range existing {
		nsCopy := make(map[string]string, len(values))
		for key, value := range values {
			nsCopy[key] = value
		}
		merged[ns] = nsCopy
	}
	namespaceTags := make(map[string]string, len(tags))
	if current, ok := merged[namespace]; ok {
		for key, value := range current {
			namespaceTags[key] = value
		}
	}
	for key, value := range tags {
		namespaceTags[key] = value
	}
	merged[namespace] = namespaceTags
	return merged
}

func validateDefinedTagOverrides(existing map[string]map[string]string, namespace string, required map[string]string) error {
	namespaceTags, ok := existing[namespace]
	if !ok {
		return nil
	}
	for key, expectedValue := range required {
		if currentValue, found := namespaceTags[key]; found && currentValue != expectedValue {
			return fmt.Errorf("defined tag %s/%s must be %q, found %q", namespace, key, expectedValue, currentValue)
		}
	}
	return nil
}

func bootVolumeDefinedTagNamespace(config Config) string {
	namespace := strings.TrimSpace(config.AutoScalingConfig.DefinedTagsNamespace)
	if namespace == "" {
		return openshiftTagNS
	}
	return namespace
}

func OCICluster(capiSystemNamespace, clusterName string, instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler, config Config, useInstancePrincipal bool) (client.Object, func() error) { // Create OCICluster
	ociCluster := &infrastructurev1beta2.OCICluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: capiSystemNamespace,
		},
	}

	mutateFn := func() error {
		logger := ctrllog.Log.WithName("enableautoscaler").WithValues("component", "OCICluster", "cluster", clusterName)
		utils.SetDefaultLabels(ociCluster, instance.Name)
		annotations := map[string]string{
			skipAPIServerLBManagementAnnotation: "true",
		}
		if ociCluster.Annotations != nil {
			for key, value := range ociCluster.Annotations {
				annotations[key] = value
			}
		}
		ociCluster.Annotations = annotations
		// Preserve the immutable OCIResourceIdentifier if it exists
		existingIdentifier := ociCluster.Spec.OCIResourceIdentifier

		// Build primary and optional secondary subnets
		logger.Info("Assembling OCICluster network",
			"vcnID", applog.SafeResourceIdentifier(config.NetworkConfig.VCNID),
			"ocpSubnetID", applog.SafeResourceIdentifier(config.NetworkConfig.OCPSubnetID),
			"ocpSubnetName", config.NetworkConfig.OCPSubnetName,
			"bareMetalSubnetID", applog.SafeResourceIdentifier(config.NetworkConfig.BareMetalSubnetID),
			"bareMetalSubnetName", config.NetworkConfig.BareMetalSubnetName,
			"computeNsgID", applog.SafeResourceIdentifier(config.NetworkConfig.NetworkSecurityGroupID),
			"computeNsgName", config.NetworkConfig.ComputeNsgName,
			"apiServerLBID", applog.SafeResourceIdentifier(config.NetworkConfig.APIServerLoadBalancerID),
			"hasBareMetalSubnet", config.NetworkConfig.BareMetalSubnetID != "",
		)
		subnets := []*infrastructurev1beta2.Subnet{
			{
				ID:   swag.String(config.NetworkConfig.OCPSubnetID),
				Name: config.NetworkConfig.OCPSubnetName,
				Role: workerRoleName,
			},
		}
		if config.NetworkConfig.BareMetalSubnetID != "" {
			subnets = append(subnets, &infrastructurev1beta2.Subnet{
				ID:   swag.String(config.NetworkConfig.BareMetalSubnetID),
				Name: config.NetworkConfig.BareMetalSubnetName,
				// Use a valid role; provider validates against a fixed set
				Role: workerRoleName,
			})
		}

		// Build NSG list including optional secondary NSG
		nsgs := []*infrastructurev1beta2.NSG{
			{
				ID:   swag.String(config.NetworkConfig.NetworkSecurityGroupID),
				Name: config.NetworkConfig.ComputeNsgName,
				Role: workerRoleName,
			},
		}

		// Update the spec
		ociCluster.Spec = infrastructurev1beta2.OCIClusterSpec{
			CompartmentId: config.ClusterConfig.CompartmentID,
			ControlPlaneEndpoint: capiv1beta1.APIEndpoint{
				Host: config.NetworkConfig.ControlPlaneEndpoint,
				Port: 6443,
			},
			NetworkSpec: infrastructurev1beta2.NetworkSpec{
				APIServerLB: infrastructurev1beta2.LoadBalancer{
					LoadBalancerId: swag.String(config.NetworkConfig.APIServerLoadBalancerID),
				},
				SkipNetworkManagement: true,
				Vcn: infrastructurev1beta2.VCN{
					ID:      swag.String(config.NetworkConfig.VCNID),
					Subnets: subnets,
					NetworkSecurityGroup: infrastructurev1beta2.NetworkSecurityGroup{
						List: nsgs,
					},
				},
			},
		}
		if useInstancePrincipal {
			ociCluster.Spec.IdentityRef = &corev1.ObjectReference{
				APIVersion: ociInfrastructureAPIVersion,
				Kind:       ociClusterIdentityKind,
				Name:       identityName(clusterName),
				Namespace:  capiSystemNamespace,
			}
		}
		logger.Info("OCICluster spec ready", "subnets", len(subnets), "nsgs", len(nsgs))

		// Restore the immutable OCIResourceIdentifier if it was previously set
		if existingIdentifier != "" {
			ociCluster.Spec.OCIResourceIdentifier = existingIdentifier
		}

		return nil
	}

	return ociCluster, mutateFn
}

func OCIClusterIdentity(capiSystemNamespace, clusterName string, instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler) (client.Object, func() error) {
	identity := &infrastructurev1beta2.OCIClusterIdentity{
		ObjectMeta: metav1.ObjectMeta{
			Name:      identityName(clusterName),
			Namespace: capiSystemNamespace,
		},
	}

	mutateFn := func() error {
		logger := ctrllog.Log.WithName("enableautoscaler").WithValues("component", "OCIClusterIdentity", "cluster", clusterName)
		utils.SetDefaultLabels(identity, instance.Name)
		identity.Spec = infrastructurev1beta2.OCIClusterIdentitySpec{
			Type: infrastructurev1beta2.InstancePrincipal,
			AllowedNamespaces: &infrastructurev1beta2.AllowedNamespaces{
				NamespaceList: []string{capiSystemNamespace},
			},
		}
		logger.Info("OCIClusterIdentity spec ready",
			"name", identity.Name,
			"namespace", identity.Namespace,
			"type", identity.Spec.Type,
			"allowedNamespaces", identity.Spec.AllowedNamespaces.NamespaceList,
		)
		return nil
	}

	return identity, mutateFn
}

func CAPICluster(capiSystemNamespace, clusterName string, instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler, config Config) (client.Object, func() error) {
	cluster := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": capiClusterAPIVersion,
			"kind":       "Cluster",
			"metadata": map[string]interface{}{
				"name":      clusterName,
				"namespace": capiSystemNamespace,
			},
		},
	}

	mutateFn := func() error {
		logger := ctrllog.Log.WithName("enableautoscaler").WithValues("component", "CAPICluster", "cluster", clusterName)
		utils.SetDefaultLabels(cluster, instance.Name)
		infraRef, err := infrastructureRef(ociInfrastructureAPIVersion, ociClusterKind, capiSystemNamespace, clusterName)
		if err != nil {
			return err
		}
		cluster.Object["spec"] = map[string]interface{}{
			"clusterNetwork": map[string]interface{}{
				"pods": map[string]interface{}{
					"cidrBlocks": []interface{}{config.NetworkConfig.ClusterNetworkCIDRBlock},
				},
				"serviceDomain": clusterServiceDomain,
				"services": map[string]interface{}{
					"cidrBlocks": []interface{}{config.NetworkConfig.ServiceNetworkCIDRBlock},
				},
			},
			"infrastructureRef": infraRef,
		}
		if err := validateCAPIClusterObject(cluster); err != nil {
			return fmt.Errorf("invalid rendered Cluster object: %w", err)
		}

		infraRefName, _, _ := unstructured.NestedString(cluster.Object, "spec", "infrastructureRef", "name")
		podCIDRs, _, _ := unstructured.NestedStringSlice(cluster.Object, "spec", "clusterNetwork", "pods", "cidrBlocks")
		serviceCIDRs, _, _ := unstructured.NestedStringSlice(cluster.Object, "spec", "clusterNetwork", "services", "cidrBlocks")
		logger.Info("CAPICluster spec ready",
			"name", cluster.GetName(),
			"namespace", cluster.GetNamespace(),
			"infrastructureRef", infraRefName,
			"podCIDRs", podCIDRs,
			"serviceCIDRs", serviceCIDRs,
		)
		return nil
	}

	return cluster, mutateFn
}

func OCIMachineTemplate(capiSystemNamespace, clusterName string, instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler, config Config) (client.Object, func() error) {
	nodePoolName := NodePoolName(clusterName, instance)
	machineTemplate := &infrastructurev1beta2.OCIMachineTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      autoscalingName(nodePoolName),
			Namespace: capiSystemNamespace,
		},
	}

	mutateFn := func() error {
		logger := ctrllog.Log.WithName("enableautoscaler").WithValues("component", "OCIMachineTemplate", "cluster", clusterName, "nodePool", nodePoolName)
		if err := validateGeneratedNodePoolNames(clusterName, instance); err != nil {
			return err
		}
		utils.SetComponentLabels(machineTemplate, instance.Name, "EnableAutoscaler", "machineTemplate")

		shape := config.AutoScalingConfig.Shape

		// Check if the shape is Bare Metal
		isBM := IsBareMetalShape(shape)

		ocpSubnetID := strings.TrimSpace(config.NetworkConfig.OCPSubnetID)
		bareMetalSubnetID := ""
		bareMetalSubnetName := ""
		if isBM {
			bareMetalSubnetID = strings.TrimSpace(config.NetworkConfig.BareMetalSubnetID)
			bareMetalSubnetName = strings.TrimSpace(config.NetworkConfig.BareMetalSubnetName)
		}

		if logger.V(2).Enabled() {
			existingSpec := machineTemplate.Spec.Template.Spec
			logger.V(2).Info("Existing OCIMachineTemplate spec before mutation",
				"shape", existingSpec.Shape,
				"subnetName", existingSpec.SubnetName,
				"vnicAttachments", summarizeVnicAttachments(existingSpec.VnicAttachments),
			)
		}
		logger.Info("Assembling OCIMachineTemplate",
			"shape", shape,
			"isBM", isBM,
			"imageID", applog.SafeResourceIdentifier(config.AutoScalingConfig.ImageID),
			"cpus", config.AutoScalingConfig.CPUs,
			"memoryGB", config.AutoScalingConfig.Memory,
			"minNodes", config.AutoScalingConfig.MinNodes,
			"maxNodes", config.AutoScalingConfig.MaxNodes,
			"ocpSubnetName", config.NetworkConfig.OCPSubnetName,
			"bareMetalSubnetName", bareMetalSubnetName,
			"hasBareMetalSubnet", bareMetalSubnetID != "",
		)
		ocpSubnetName := strings.TrimSpace(config.NetworkConfig.OCPSubnetName)
		primarySubnetName, secondaryVnicSubnetName := ResolveSubnetNamesForShape(shape, ocpSubnetName, bareMetalSubnetName)
		if isBM {
			logger.Info("Resolved subnet names for shape",
				"isBM", isBM,
				"primarySubnetName", primarySubnetName,
				"secondaryVnicSubnetName", secondaryVnicSubnetName,
			)
		}
		existingDefinedTags := machineTemplate.Spec.Template.Spec.DefinedTags

		// Common machinespec for bare metal and virtual machines
		spec := infrastructurev1beta2.OCIMachineSpec{
			ImageId: config.AutoScalingConfig.ImageID,
			Shape:   shape,
			// ShapeConfig will be populated for non-Bare Metal shapes. For Bare Metal shapes (BM.*),
			// leave OCPUs/Memory empty as those shapes are not flex-configurable.
			ShapeConfig:                    infrastructurev1beta2.ShapeConfig{},
			IsPvEncryptionInTransitEnabled: false,
			// Select the primary subnet by name defined in OCICluster VCN subnets
			SubnetName: primarySubnetName,

			NetworkDetails: infrastructurev1beta2.NetworkDetails{
				AssignPrivateDnsRecord: swag.Bool(assignPrivateDNS),
			},
			FreeformTags: map[string]string{
				utils.OCIInstanceManagedByTag:             instance.Name,
				utils.OCIInstanceAutoscalerNamespaceTag:   instance.Namespace,
				utils.OCIInstanceAutoscalerClusterNameTag: clusterName,
			},
			DefinedTags: existingDefinedTags,
		}
		if poolIdentifier := strings.TrimSpace(instance.Spec.Autoscaling.PoolIdentifier); poolIdentifier != "" {
			spec.FreeformTags[utils.OCIInstanceAutoscalerPoolTag] = poolIdentifier
		}

		// BM shapes:
		//   1. Primary subnet is bare metal; secondary VNIC uses OCP subnet.
		//   2. BootVolume Type is ISCSI
		//   3. Add defined tag for ISCSI
		if isBM {
			if ocpSubnetID != "" && bareMetalSubnetID != "" && ocpSubnetID == bareMetalSubnetID {
				return fmt.Errorf("bare metal subnet must differ from primary subnet")
			}
			if ocpSubnetName != "" && bareMetalSubnetName != "" && ocpSubnetName == bareMetalSubnetName {
				return fmt.Errorf("bare metal subnet must differ from primary subnet")
			}
			if bareMetalSubnetID == "" {
				return fmt.Errorf("bare metal subnet ID must not be empty for BM shapes")
			}
			if bareMetalSubnetName == "" {
				return fmt.Errorf("bare metal subnet name must not be empty for BM shapes")
			}
			if secondaryVnicSubnetName == "" {
				return fmt.Errorf("bare metal secondary VNIC subnet name must not be empty for BM shapes")
			}
			spec.SubnetName = primarySubnetName
			bootTagNamespace := bootVolumeDefinedTagNamespace(config)
			if err := validateDefinedTagOverrides(spec.DefinedTags, openshiftTagNS, map[string]string{
				openshiftTagKey: openshiftTagValue,
			}); err != nil {
				return err
			}
			if err := validateDefinedTagOverrides(spec.DefinedTags, bootTagNamespace, map[string]string{
				bootVolumeTypeTag: bootVolumeTypeIscsi,
			}); err != nil {
				return err
			}
			// Use the tenancy's existing OpenShift namespace for resource classification.
			spec.DefinedTags = mergeDefinedTags(spec.DefinedTags, openshiftTagNS, map[string]string{
				openshiftTagKey: openshiftTagValue,
			})
			spec.DefinedTags = mergeDefinedTags(spec.DefinedTags, bootTagNamespace, map[string]string{
				bootVolumeTypeTag: bootVolumeTypeIscsi,
			})
			// For BM, explicitly force iSCSI boot volume type to match OCI BM boot expectations.
			spec.LaunchOptions = &infrastructurev1beta2.LaunchOptions{
				BootVolumeType: infrastructurev1beta2.LaunchOptionsBootVolumeTypeIscsi,
			}
			// For Bare Metal shapes, attach an additional VNIC for OCP.
			// Align with OCI BM secondary-VNIC patterns by pinning the attachment to NIC index 1.
			spec.VnicAttachments = []infrastructurev1beta2.VnicAttachment{
				{
					SubnetName:     secondaryVnicSubnetName,
					AssignPublicIp: false,
					DisplayName:    swag.String(vnicOcpDisplayName),
					NicIndex:       swag.Int(secondaryVnicIndex),
				},
			}
			if config.AutoScalingConfig.EnableRDMA {
				spec.ComputeClusterId = swag.String(config.AutoScalingConfig.RDMAComputeClusterID)
				spec.AgentConfig = &infrastructurev1beta2.LaunchInstanceAgentConfig{
					PluginsConfig: []infrastructurev1beta2.InstanceAgentPluginConfig{
						{
							Name:         swag.String("Compute HPC RDMA Authentication"),
							DesiredState: infrastructurev1beta2.InstanceAgentPluginConfigDetailsDesiredStateEnabled,
						},
					},
				}
			}
		} else {
			// VM shapes:
			//    1. primary subnet is OCP; no secondary VNIC.
			spec.SubnetName = primarySubnetName
			spec.ShapeConfig.Ocpus = fmt.Sprintf("%d", config.AutoScalingConfig.CPUs)
			spec.ShapeConfig.MemoryInGBs = fmt.Sprintf("%d", config.AutoScalingConfig.Memory)
		}

		machineTemplate.Spec = infrastructurev1beta2.OCIMachineTemplateSpec{
			Template: infrastructurev1beta2.OCIMachineTemplateResource{
				Spec: spec,
			},
		}
		logger.Info("OCIMachineTemplate spec ready",
			"shape", machineTemplate.Spec.Template.Spec.Shape,
			"subnetName", machineTemplate.Spec.Template.Spec.SubnetName,
			"definedTagKeys", applog.SortedNestedMapKeys(spec.DefinedTags),
			"hasLaunchOptions", spec.LaunchOptions != nil,
			"vnicAttachments", summarizeVnicAttachments(spec.VnicAttachments),
		)
		return nil
	}

	return machineTemplate, mutateFn
}

func ResolveSubnetNamesForShape(shape string, ocpSubnetName string, bareMetalSubnetName string) (string, string) {
	if IsBareMetalShape(shape) {
		return bareMetalSubnetName, ocpSubnetName
	}
	return ocpSubnetName, ""
}

func MachineDeployment(capiSystemNamespace, clusterName string, instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler, config Config) (client.Object, func() error) {
	nodePoolName := NodePoolName(clusterName, instance)
	machineDeployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": capiClusterAPIVersion,
			"kind":       "MachineDeployment",
			"metadata": map[string]interface{}{
				"name":      nodePoolName,
				"namespace": capiSystemNamespace,
			},
		},
	}

	mutateFn := func() error {
		logger := ctrllog.Log.WithName("enableautoscaler").WithValues("component", "MachineDeployment", "cluster", clusterName, "nodePool", nodePoolName)
		if err := validateGeneratedNodePoolNames(clusterName, instance); err != nil {
			return err
		}
		utils.SetDefaultLabels(machineDeployment, instance.Name)
		infraRef, err := infrastructureRef(ociInfrastructureAPIVersion, ociMachineTemplateKind, capiSystemNamespace, autoscalingName(nodePoolName))
		if err != nil {
			return err
		}
		annotations := map[string]string{}
		if objAnnotations := machineDeployment.GetAnnotations(); objAnnotations != nil {
			for key, value := range objAnnotations {
				annotations[key] = value
			}
		}
		annotations[cpuCapacityAnnotation] = fmt.Sprintf("%d", config.AutoScalingConfig.CPUs)
		annotations[nodeLabelsAnnotation] = workerRoleLabelValue
		annotations[memoryCapacityAnnotation] = fmt.Sprintf(memoryCapacityFormat, config.AutoScalingConfig.Memory)
		annotations[nodeGroupMinSizeAnnotation] = fmt.Sprintf("%d", config.AutoScalingConfig.MinNodes)
		annotations[nodeGroupMaxSizeAnnotation] = fmt.Sprintf("%d", config.AutoScalingConfig.MaxNodes)
		machineDeployment.SetAnnotations(annotations)

		spec, found, err := unstructured.NestedMap(machineDeployment.Object, "spec")
		if err != nil {
			return fmt.Errorf("failed to read existing MachineDeployment spec: %w", err)
		}
		if !found {
			spec = map[string]interface{}{
				"replicas": int64(config.AutoScalingConfig.MinNodes),
			}
		} else {
			_, found, err := unstructured.NestedInt64(spec, "replicas")
			if err != nil {
				return fmt.Errorf("failed to read existing MachineDeployment spec.replicas: %w", err)
			}
			if !found {
				spec["replicas"] = int64(config.AutoScalingConfig.MinNodes)
			}
		}
		machineLabels := map[string]interface{}{
			capiClusterNameLabel:    clusterName,
			capiDeploymentNameLabel: nodePoolName,
		}
		spec["clusterName"] = clusterName
		spec["selector"] = map[string]interface{}{
			"matchLabels": machineLabels,
		}
		machineTemplateSpec := map[string]interface{}{
			"clusterName": clusterName,
			"bootstrap": map[string]interface{}{
				"dataSecretName": bootstrapSecretName(clusterName),
			},
			"infrastructureRef": infraRef,
		}
		if config.AutoScalingConfig.EnableRDMA {
			machineTemplateSpec["failureDomain"] = config.AutoScalingConfig.RDMAFailureDomain
		}
		spec["template"] = map[string]interface{}{
			"metadata": map[string]interface{}{
				"labels": machineTemplateLabels(instance.Name, clusterName, nodePoolName),
			},
			"spec": machineTemplateSpec,
		}
		if IsBareMetalShape(config.AutoScalingConfig.Shape) {
			spec["rollout"] = map[string]interface{}{
				"strategy": map[string]interface{}{
					"type": "RollingUpdate",
					"rollingUpdate": map[string]interface{}{
						"maxSurge":       int64(0),
						"maxUnavailable": int64(1),
					},
				},
			}
		} else {
			delete(spec, "rollout")
		}
		machineDeployment.Object["spec"] = spec
		if err := validateMachineDeploymentObject(machineDeployment); err != nil {
			return fmt.Errorf("invalid rendered MachineDeployment object: %w", err)
		}

		infraRefName, _, _ := unstructured.NestedString(machineDeployment.Object, "spec", "template", "spec", "infrastructureRef", "name")
		logger.Info("MachineDeployment spec ready",
			"name", machineDeployment.GetName(),
			"namespace", machineDeployment.GetNamespace(),
			"infrastructureRef", infraRefName,
			"bootstrapSecret", bootstrapSecretName(clusterName),
			"minNodes", config.AutoScalingConfig.MinNodes,
			"maxNodes", config.AutoScalingConfig.MaxNodes,
		)
		return nil
	}

	return machineDeployment, mutateFn
}

func MachineHealthCheck(capiSystemNamespace, clusterName string, instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler, config Config) (client.Object, func() error) {
	nodePoolName := NodePoolName(clusterName, instance)
	machineHealthCheck := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": capiClusterAPIVersion,
			"kind":       "MachineHealthCheck",
			"metadata": map[string]interface{}{
				"name":      autoscalingName(nodePoolName),
				"namespace": capiSystemNamespace,
			},
		},
	}

	mutateFn := func() error {
		logger := ctrllog.Log.WithName("enableautoscaler").WithValues("component", "MachineHealthCheck", "cluster", clusterName, "nodePool", nodePoolName)
		if err := validateGeneratedNodePoolNames(clusterName, instance); err != nil {
			return err
		}
		utils.SetDefaultLabels(machineHealthCheck, instance.Name)
		nodeStartupTimeoutSeconds := defaultNodeStartupTimeoutSeconds
		if IsBareMetalShape(config.AutoScalingConfig.Shape) {
			nodeStartupTimeoutSeconds = bareMetalNodeStartupTimeoutSeconds
		}
		matchLabels := map[string]interface{}{
			capiClusterNameLabel:    clusterName,
			capiDeploymentNameLabel: nodePoolName,
		}
		machineHealthCheck.Object["spec"] = map[string]interface{}{
			"clusterName": clusterName,
			"selector": map[string]interface{}{
				"matchLabels": matchLabels,
			},
			"checks": map[string]interface{}{
				"nodeStartupTimeoutSeconds": nodeStartupTimeoutSeconds,
				"unhealthyNodeConditions": []interface{}{
					map[string]interface{}{
						"type":           "Ready",
						"status":         "Unknown",
						"timeoutSeconds": unhealthyNodeTimeoutSeconds,
					},
					map[string]interface{}{
						"type":           "Ready",
						"status":         "False",
						"timeoutSeconds": unhealthyNodeTimeoutSeconds,
					},
				},
			},
			"remediation": map[string]interface{}{
				"triggerIf": map[string]interface{}{
					"unhealthyLessThanOrEqualTo": "100%",
				},
			},
		}
		logger.Info("MachineHealthCheck spec ready",
			"name", machineHealthCheck.GetName(),
			"namespace", machineHealthCheck.GetNamespace(),
			"nodeStartupTimeoutSeconds", nodeStartupTimeoutSeconds,
			"unhealthyNodeTimeoutSeconds", unhealthyNodeTimeoutSeconds,
		)
		return nil
	}

	return machineHealthCheck, mutateFn
}

// ValidateMinMaxNodes validates the min and max nodes values for the autoscaler
func ValidateMinMaxNodes(autoscaler *ocicapioperatorv1alpha1.OCIClusterAutoscaler, config Config) error {
	minNodes := config.AutoScalingConfig.MinNodes
	maxNodes := config.AutoScalingConfig.MaxNodes
	if autoscaler.Spec.Autoscaling.MinNodes != nil {
		minNodes = *autoscaler.Spec.Autoscaling.MinNodes
	}
	if autoscaler.Spec.Autoscaling.MaxNodes != nil {
		maxNodes = *autoscaler.Spec.Autoscaling.MaxNodes
	}

	// ensure that min nodes is less than max nodes
	if minNodes < 0 {
		return fmt.Errorf("min nodes must be equal to or greater than 0")
	}
	if maxNodes < 0 {
		return fmt.Errorf("max nodes must be equal to or greater than 0")
	}
	if minNodes > maxNodes {
		return fmt.Errorf("min nodes must be less than max nodes")
	}
	return nil
}

type VnicAttachmentSummary struct {
	Index      int
	SubnetName string
	NicIndex   *int
}

func summarizeVnicAttachments(attachments []infrastructurev1beta2.VnicAttachment) []VnicAttachmentSummary {
	summary := make([]VnicAttachmentSummary, 0, len(attachments))
	for i, attachment := range attachments {
		summary = append(summary, VnicAttachmentSummary{
			Index:      i,
			SubnetName: attachment.SubnetName,
			NicIndex:   attachment.NicIndex,
		})
	}
	return summary
}
