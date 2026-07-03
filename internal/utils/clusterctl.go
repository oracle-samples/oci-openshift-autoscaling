/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package utils

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	v1alpha3 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	coreProviderName              = "cluster-api"
	infrastructureOCIProviderName = "oci"
	providerRefFormat             = "%s:%s"
	capiClusterCRDName            = "clusters.cluster.x-k8s.io"
	capiMachineDeploymentCRDName  = "machinedeployments.cluster.x-k8s.io"
	capociClusterCRDName          = "ociclusters.infrastructure.cluster.x-k8s.io"
	capociMachineTemplateCRDName  = "ocimachinetemplates.infrastructure.cluster.x-k8s.io"
)

// GenerateCAPIComponents uses the clusterctl library to generate all CAPI resources
// for a given provider and returns the resources as a list of unstructured objects
func GenerateCAPIComponents(ctx context.Context, provider string, providerType v1alpha3.ProviderType, namespace string, versionOverride string) ([]unstructured.Unstructured, error) {
	ref, err := providerRef(provider, providerType, versionOverride)
	if err != nil {
		return nil, err
	}
	logger := ctrllog.Log.WithName("clusterctl").WithValues(
		"provider", provider,
		"providerType", string(providerType),
		"providerRef", ref,
		"targetNamespace", namespace,
	)
	logger.Info("Fetching provider components")
	clusterctlClient, err := client.New(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("error creating clusterctl client: %w", err)
	}
	components, err := clusterctlClient.GetProviderComponents(ctx, ref, providerType, client.ComponentsOptions{
		TargetNamespace: namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("error getting provider components: %w", err)
	}
	objs := components.Objs()
	if err := ValidateGeneratedProviderComponents(provider, providerType, namespace, objs); err != nil {
		return nil, err
	}
	logger.Info("Fetched provider components", "count", len(objs))
	return objs, nil
}

func ValidateGeneratedProviderComponents(provider string, providerType v1alpha3.ProviderType, namespace string, objs []unstructured.Unstructured) error {
	requiredCRDs := map[string]string{}
	requiredKinds := map[string]string{}
	validateNamespacedResources := strings.TrimSpace(namespace) != ""

	switch {
	case provider == coreProviderName && providerType == v1alpha3.CoreProviderType:
		if validateNamespacedResources {
			requiredKinds[namespacedKindKey("Deployment", namespace)] = "core provider deployment"
			requiredKinds[namespacedKindKey("Service", namespace)] = "core provider service"
		}
		requiredKinds[kindKey("ValidatingWebhookConfiguration")] = "core provider validating webhook"
		requiredKinds[kindKey("MutatingWebhookConfiguration")] = "core provider mutating webhook"
		requiredCRDs[capiClusterCRDName] = "Cluster API Cluster CRD"
		requiredCRDs[capiMachineDeploymentCRDName] = "Cluster API MachineDeployment CRD"
	case provider == infrastructureOCIProviderName && providerType == v1alpha3.InfrastructureProviderType:
		if validateNamespacedResources {
			requiredKinds[namespacedKindKey("Deployment", namespace)] = "CAPOCI deployment"
			requiredKinds[namespacedKindKey("Service", namespace)] = "CAPOCI service"
		}
		requiredKinds[kindKey("ValidatingWebhookConfiguration")] = "CAPOCI validating webhook"
		requiredKinds[kindKey("MutatingWebhookConfiguration")] = "CAPOCI mutating webhook"
		requiredCRDs[capociClusterCRDName] = "CAPOCI OCICluster CRD"
		requiredCRDs[capociMachineTemplateCRDName] = "CAPOCI OCIMachineTemplate CRD"
	default:
		return nil
	}

	seenKinds := map[string]bool{}
	seenCRDs := map[string]bool{}
	for i := range objs {
		obj := objs[i]
		seenKinds[kindKey(obj.GetKind())] = true
		if obj.GetNamespace() == namespace {
			seenKinds[namespacedKindKey(obj.GetKind(), namespace)] = true
		}
		if obj.GetKind() == "CustomResourceDefinition" {
			seenCRDs[obj.GetName()] = true
		}
	}
	for requiredKey, description := range requiredKinds {
		if !seenKinds[requiredKey] {
			return fmt.Errorf("generated %s %s components are missing %s (%s); review provider version compatibility before deploying", provider, providerType, description, requiredKey)
		}
	}
	for crdName, description := range requiredCRDs {
		if !seenCRDs[crdName] {
			return fmt.Errorf("generated %s %s components are missing %s (%s); review provider version compatibility before deploying", provider, providerType, description, crdName)
		}
	}
	return nil
}

func kindKey(kind string) string {
	return kind
}

func namespacedKindKey(kind, namespace string) string {
	return fmt.Sprintf("%s/%s", kind, namespace)
}

func providerRef(provider string, providerType v1alpha3.ProviderType, versionOverride string) (string, error) {
	version := strings.TrimSpace(versionOverride)
	if requiresExplicitProviderVersion(provider, providerType) {
		if version == "" {
			return "", fmt.Errorf("provider version is required for %s %s; set CAPI_VERSION or CAPOCI_VERSION", provider, providerType)
		}
		if err := ValidateProviderVersion(version); err != nil {
			return "", err
		}
		return fmt.Sprintf(providerRefFormat, provider, version), nil
	}

	if version == "" {
		return provider, nil
	}
	if err := ValidateProviderVersion(version); err != nil {
		return "", err
	}
	return fmt.Sprintf(providerRefFormat, provider, version), nil
}

func requiresExplicitProviderVersion(provider string, providerType v1alpha3.ProviderType) bool {
	switch {
	case provider == coreProviderName && providerType == v1alpha3.CoreProviderType:
		return true
	case provider == infrastructureOCIProviderName && providerType == v1alpha3.InfrastructureProviderType:
		return true
	default:
		return false
	}
}
func ProviderWithVersion(provider string, providerVersion string) string {
	if strings.TrimSpace(providerVersion) == "" {
		return provider
	}
	return fmt.Sprintf("%s:%s", provider, providerVersion)
}

var providerVersionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

func ValidateProviderVersion(version string) error {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return fmt.Errorf("provider version must not be empty")
	}
	if !providerVersionPattern.MatchString(trimmed) {
		return fmt.Errorf("provider version %q must match vMAJOR.MINOR.PATCH (optionally with prerelease)", trimmed)
	}
	return nil
}
