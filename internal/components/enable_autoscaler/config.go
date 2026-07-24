/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package enableautoscaler

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	ocicapioperatorv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	applog "github.com/openshift/oci-capi-operator/internal/logging"
	"github.com/openshift/oci-capi-operator/internal/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

var definedTagsNamespaceNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,99}$`)

type Config struct {
	ClusterConfig     ClusterConfig
	NetworkConfig     NetworkConfig
	AutoScalingConfig AutoScalingConfig
}

type AutoScalingConfig struct {
	CPUs                 int32  `envconfig:"AUTOSCALER_CPUS" default:"2"`
	Memory               int32  `envconfig:"AUTOSCALER_MEMORY" default:"4"`
	MinNodes             int32  `envconfig:"AUTOSCALER_MIN_NODES" default:"1"`
	MaxNodes             int32  `envconfig:"AUTOSCALER_MAX_NODES" default:"3"`
	Shape                string `envconfig:"AUTOSCALER_SHAPE" default:"oc3"`
	ImageID              string `envconfig:"IMAGE_ID" default:""`
	DefinedTagsNamespace string `envconfig:"AUTOSCALER_DEFINED_TAGS_NAMESPACE" default:""`
}

type ClusterConfig struct {
	CompartmentID string `envconfig:"COMPARTMENT_ID" default:""`
}

type NetworkConfig struct {
	VCNID                   string `envconfig:"VCN_ID" default:""`
	OCPSubnetID             string `envconfig:"OCP_SUBNET_ID" default:""`
	OCPSubnetName           string `envconfig:"OCP_SUBNET_NAME" default:"private"`
	ComputeNsgName          string `envconfig:"COMPUTE_NSG_NAME" default:"ComputeNSG"`
	NetworkSecurityGroupID  string `envconfig:"NETWORK_SECURITY_GROUP_ID" default:""`
	ControlPlaneEndpoint    string `envconfig:"CONTROL_PLANE_ENDPOINT" default:""`
	APIServerLoadBalancerID string `envconfig:"API_SERVER_LOAD_BALANCER_ID" default:""`
	ClusterNetworkCIDRBlock string `envconfig:"CLUSTER_NETWORK_CIDR_BLOCK" default:""`
	ServiceNetworkCIDRBlock string `envconfig:"SERVICE_NETWORK_CIDR_BLOCK" default:""`

	// Secondary network attachments (optional)
	BareMetalSubnetID string `envconfig:"BARE_METAL_SUBNET_ID" default:""`
	// BareMetalSubnetName is the name to assign/use for the bare metal subnet in OCICluster
	// and to reference from VNIC attachments. It must match the VCN subnets list entry.
	BareMetalSubnetName string `envconfig:"BARE_METAL_SUBNET_NAME" default:""`
}

func SetAutoScalingConfig(ctx context.Context, client client.Client, instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler, config Config) (Config, error) {
	logger := ctrllog.Log.WithName("enableautoscaler").WithValues("component", "config", "resource", instance.Name)
	clusterCIDRProvided := config.NetworkConfig.ClusterNetworkCIDRBlock != ""
	serviceCIDRProvided := config.NetworkConfig.ServiceNetworkCIDRBlock != ""
	if err := ValidateShapeConfig(instance, config); err != nil {
		return config, err
	}
	if instance.Spec.Autoscaling.ShapeConfig != nil {
		if instance.Spec.Autoscaling.ShapeConfig.CPUs != nil {
			config.AutoScalingConfig.CPUs = *instance.Spec.Autoscaling.ShapeConfig.CPUs
		}
		if instance.Spec.Autoscaling.ShapeConfig.Memory != nil {
			config.AutoScalingConfig.Memory = *instance.Spec.Autoscaling.ShapeConfig.Memory
		}
	}
	if instance.Spec.Autoscaling.MinNodes != nil {
		config.AutoScalingConfig.MinNodes = *instance.Spec.Autoscaling.MinNodes
	}
	if instance.Spec.Autoscaling.MaxNodes != nil {
		config.AutoScalingConfig.MaxNodes = *instance.Spec.Autoscaling.MaxNodes
	}

	if instance.Spec.Autoscaling.Shape != "" {
		config.AutoScalingConfig.Shape = instance.Spec.Autoscaling.Shape
	}
	if instance.Spec.Autoscaling.ImageID != "" {
		config.AutoScalingConfig.ImageID = instance.Spec.Autoscaling.ImageID
	}
	if err := ValidateRequiredConfigFields(config); err != nil {
		return config, err
	}
	if err := ValidateBareMetalSubnetConfig(config); err != nil {
		return config, err
	}
	if err := ValidateDefinedTagsNamespace(config); err != nil {
		return config, err
	}
	if config, err := SetNetworkConfig(ctx, client, config); err != nil {
		return config, fmt.Errorf("failed to set network config: %w", err)
	}
	logger.Info("Resolved autoscaling configuration",
		"shape", config.AutoScalingConfig.Shape,
		"shapeSource", stringSource(instance.Spec.Autoscaling.Shape),
		"imageID", applog.SafeResourceIdentifier(config.AutoScalingConfig.ImageID),
		"imageIDSource", stringSource(instance.Spec.Autoscaling.ImageID),
		"cpus", config.AutoScalingConfig.CPUs,
		"cpusSource", int32Source(instance.Spec.Autoscaling.ShapeConfig != nil && instance.Spec.Autoscaling.ShapeConfig.CPUs != nil),
		"memoryGB", config.AutoScalingConfig.Memory,
		"memorySource", int32Source(instance.Spec.Autoscaling.ShapeConfig != nil && instance.Spec.Autoscaling.ShapeConfig.Memory != nil),
		"minNodes", config.AutoScalingConfig.MinNodes,
		"minNodesSource", int32Source(instance.Spec.Autoscaling.MinNodes != nil),
		"maxNodes", config.AutoScalingConfig.MaxNodes,
		"maxNodesSource", int32Source(instance.Spec.Autoscaling.MaxNodes != nil),
		"definedTagsNamespace", config.AutoScalingConfig.DefinedTagsNamespace,
		"definedTagsNamespaceSource", "operatorConfig",
		"compartmentID", applog.SafeResourceIdentifier(config.ClusterConfig.CompartmentID),
		"vcnID", applog.SafeResourceIdentifier(config.NetworkConfig.VCNID),
		"ocpSubnetID", applog.SafeResourceIdentifier(config.NetworkConfig.OCPSubnetID),
		"clusterNetworkCIDRBlock", config.NetworkConfig.ClusterNetworkCIDRBlock,
		"clusterNetworkCIDRSource", discoveredSource(clusterCIDRProvided),
		"serviceNetworkCIDRBlock", config.NetworkConfig.ServiceNetworkCIDRBlock,
		"serviceNetworkCIDRSource", discoveredSource(serviceCIDRProvided),
	)

	return config, nil
}

func ValidateShapeConfig(instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler, config Config) error {
	cpus := config.AutoScalingConfig.CPUs
	memory := config.AutoScalingConfig.Memory
	if instance != nil && instance.Spec.Autoscaling.ShapeConfig != nil {
		if instance.Spec.Autoscaling.ShapeConfig.CPUs != nil {
			cpus = *instance.Spec.Autoscaling.ShapeConfig.CPUs
		}
		if instance.Spec.Autoscaling.ShapeConfig.Memory != nil {
			memory = *instance.Spec.Autoscaling.ShapeConfig.Memory
		}
	}
	if cpus <= 0 {
		return fmt.Errorf("shapeConfig.cpus must be greater than 0")
	}
	if memory <= 0 {
		return fmt.Errorf("shapeConfig.memory must be greater than 0")
	}
	return nil
}

// SetNetworkConfig finds the network CIDRs in the cluster if it is not set in the config
// then sets them in the config
func SetNetworkConfig(ctx context.Context, client client.Client, config Config) (Config, error) {
	clusterCIDRProvided := config.NetworkConfig.ClusterNetworkCIDRBlock != ""
	serviceCIDRProvided := config.NetworkConfig.ServiceNetworkCIDRBlock != ""
	// Validate secondary subnet configuration when provided
	if config.NetworkConfig.BareMetalSubnetID != "" && config.NetworkConfig.BareMetalSubnetID == config.NetworkConfig.OCPSubnetID {
		return config, fmt.Errorf("secondary subnet must differ from primary subnet (bareMetalSubnetID=%s, ocpSubnetID=%s)", config.NetworkConfig.BareMetalSubnetID, config.NetworkConfig.OCPSubnetID)
	}
	if config.NetworkConfig.ClusterNetworkCIDRBlock == "" {
		clusterNetworkCIDRBlock, err := utils.GetClusterNetworkCIDRBlock(ctx, client)
		if err != nil {
			return config, fmt.Errorf("failed to get cluster network CIDR block: %w", err)
		}
		config.NetworkConfig.ClusterNetworkCIDRBlock = clusterNetworkCIDRBlock
	}
	if config.NetworkConfig.ServiceNetworkCIDRBlock == "" {
		serviceNetworkCIDRBlock, err := utils.GetServiceNetworkCIDRBlock(ctx, client)
		if err != nil {
			return config, fmt.Errorf("failed to get service network CIDR block: %w", err)
		}
		config.NetworkConfig.ServiceNetworkCIDRBlock = serviceNetworkCIDRBlock
	}
	logger := ctrllog.Log.WithName("enableautoscaler").WithValues("component", "networkConfig")
	logger.Info("Resolved network CIDR configuration",
		"clusterNetworkCIDRBlock", config.NetworkConfig.ClusterNetworkCIDRBlock,
		"clusterNetworkCIDRSource", discoveredSource(clusterCIDRProvided),
		"serviceNetworkCIDRBlock", config.NetworkConfig.ServiceNetworkCIDRBlock,
		"serviceNetworkCIDRSource", discoveredSource(serviceCIDRProvided),
	)
	return config, nil
}

func ValidateDefinedTagsNamespace(config Config) error {
	namespace := strings.TrimSpace(config.AutoScalingConfig.DefinedTagsNamespace)
	if namespace == "" {
		return nil
	}
	if !definedTagsNamespaceNamePattern.MatchString(namespace) {
		return fmt.Errorf("defined tags namespace must start with a letter, contain only letters, numbers, '.', '_' or '-', and be at most 100 characters")
	}
	return nil
}

func ValidateRequiredConfigFields(config Config) error {
	if strings.TrimSpace(config.ClusterConfig.CompartmentID) == "" {
		return fmt.Errorf("compartment ID must not be empty")
	}
	if strings.TrimSpace(config.NetworkConfig.VCNID) == "" {
		return fmt.Errorf("VCN ID must not be empty")
	}
	if strings.TrimSpace(config.NetworkConfig.OCPSubnetID) == "" {
		return fmt.Errorf("OCP subnet ID must not be empty")
	}
	if strings.TrimSpace(config.NetworkConfig.OCPSubnetName) == "" {
		return fmt.Errorf("OCP subnet name must not be empty")
	}
	if strings.TrimSpace(config.NetworkConfig.ComputeNsgName) == "" {
		return fmt.Errorf("compute NSG name must not be empty")
	}
	if strings.TrimSpace(config.NetworkConfig.NetworkSecurityGroupID) == "" {
		return fmt.Errorf("network security group ID must not be empty")
	}
	if strings.TrimSpace(config.NetworkConfig.ControlPlaneEndpoint) == "" {
		return fmt.Errorf("control plane endpoint must not be empty")
	}
	if strings.TrimSpace(config.NetworkConfig.APIServerLoadBalancerID) == "" {
		return fmt.Errorf("API server load balancer ID must not be empty")
	}
	return nil
}

func ValidateBareMetalSubnetConfig(config Config) error {
	if !IsBareMetalShape(config.AutoScalingConfig.Shape) {
		return nil
	}
	if strings.TrimSpace(config.NetworkConfig.BareMetalSubnetID) == "" {
		return fmt.Errorf("bare metal subnet ID must not be empty for BM shapes")
	}
	if strings.TrimSpace(config.NetworkConfig.BareMetalSubnetName) == "" {
		return fmt.Errorf("bare metal subnet name must not be empty for BM shapes")
	}
	return nil
}

func stringSource(value string) string {
	if strings.TrimSpace(value) != "" {
		return "customResource"
	}
	return "operatorConfig"
}

func int32Source(fromCustomResource bool) string {
	if fromCustomResource {
		return "customResource"
	}
	return "operatorConfig"
}

func discoveredSource(provided bool) string {
	if provided {
		return "operatorConfig"
	}
	return "discovered"
}
