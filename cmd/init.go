/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/openshift/oci-capi-operator/internal/components/crds"
	"github.com/openshift/oci-capi-operator/internal/controllers"
	"github.com/openshift/oci-capi-operator/internal/utils"

	"github.com/go-logr/logr"
	"github.com/kelseyhightower/envconfig"
	"github.com/spf13/cobra"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	coreProviderName              = "cluster-api"
	infrastructureOCIProviderName = "oci"
)

func NewInitCommand() *cobra.Command {
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initializes prerequesites for the OCI CAPI operator.",
		Long:  "Applies the required CRDs for the cluster-api and capi-provider-oci providers to the cluster.",
	}
	initCmd.Run = func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		setupLog := ctrl.Log.WithName("setup")
		if err := runInit(ctx, &setupLog); err != nil {
			setupLog.Error(err, "Failed to initialize operator")
			os.Exit(1)
		}
		os.Exit(0)
	}
	return initCmd
}

// Apply the CRDs to the cluster for this operator
func runInit(ctx context.Context, setupLog *logr.Logger) error {
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))

	cfg, err := ctrl.GetConfig()
	if err != nil {
		setupLog.Error(err, "Failed to get config")
		return err
	}
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "Failed to create client")
		return err
	}

	providerConfig := controllers.ProviderConfig{}
	if err := envconfig.Process("", &providerConfig); err != nil {
		setupLog.Error(err, "Failed to process provider environment variables")
		return err
	}
	namespaceConfig := controllers.NamespaceConfig{}
	if err := envconfig.Process("", &namespaceConfig); err != nil {
		setupLog.Error(err, "Failed to process namespace environment variables")
		return err
	}
	namespaceConfig = namespaceConfig.WithDefaults()
	if err := namespaceConfig.Validate(); err != nil {
		setupLog.Error(err, "Invalid namespace configuration")
		return err
	}
	if err := utils.ValidateProviderVersion(providerConfig.CAPIVersion); err != nil {
		setupLog.Error(err, "Invalid CAPI_VERSION")
		return err
	}

	// get the CRDs for the cluster-api provider
	setupLog.Info("Fetching provider CRDs", "provider", coreProviderName, "version", providerConfig.CAPIVersion)
	capiCRDs, err := crds.GetClusterctlComponents(ctx, coreProviderName, v1alpha3.CoreProviderType, providerConfig.CAPIVersion)
	if err != nil {
		setupLog.Error(err, "Failed to get CRDs for CAPI")
		return err
	}
	if err := setProviderCRDWebhookNamespace(capiCRDs, namespaceConfig.CAPIProviderNamespace); err != nil {
		setupLog.Error(err, "Failed to configure CAPI CRD webhook namespace")
		return err
	}
	capociProviderVersion := providerConfig.CAPOCIVersion
	if err := utils.ValidateProviderVersion(capociProviderVersion); err != nil {
		setupLog.Error(err, "Invalid CAPOCI_VERSION")
		return err
	}
	setupLog.Info("Fetching provider CRDs", "provider", infrastructureOCIProviderName, "version", capociProviderVersion)
	// get the CRDs for the capi-provider-oci provider
	capociCRDs, err := crds.GetClusterctlComponents(ctx, infrastructureOCIProviderName, v1alpha3.InfrastructureProviderType, providerConfig.CAPOCIVersion)

	if err != nil {
		setupLog.Error(err, "Failed to get CRDs for CAPOCI")
		return err
	}
	if err := setProviderCRDWebhookNamespace(capociCRDs, namespaceConfig.CAPOCIProviderNamespace); err != nil {
		setupLog.Error(err, "Failed to configure CAPOCI CRD webhook namespace")
		return err
	}
	setupLog.Info("Fetched provider CRDs",
		"capiCount", len(capiCRDs),
		"capociCount", len(capociCRDs),
	)

	// apply all the CRDs
	createdCount := 0
	updatedCount := 0
	for _, crd := range append(capiCRDs, capociCRDs...) {
		setupLog.Info("Applying CRD", "name", crd.GetName())
		applied, err := applyProviderCRD(ctx, k8sClient, &crd)
		if err != nil {
			setupLog.Error(err, "Failed to apply CRD", "name", crd.GetName())
			return err
		}
		switch applied {
		case "created":
			createdCount++
			setupLog.Info("Created CRD", "name", crd.GetName())
		case "updated":
			updatedCount++
			setupLog.Info("Updated CRD", "name", crd.GetName())
		}
	}
	setupLog.Info("All CRDs applied successfully", "created", createdCount, "updated", updatedCount)
	return nil
}

func setProviderCRDWebhookNamespace(crds []unstructured.Unstructured, namespace string) error {
	for i := range crds {
		serviceName, found, err := unstructured.NestedString(crds[i].Object, "spec", "conversion", "webhook", "clientConfig", "service", "name")
		if err != nil {
			return fmt.Errorf("failed to read CRD conversion webhook service name for %s: %w", crds[i].GetName(), err)
		}
		if !found || serviceName == "" {
			continue
		}
		if err := unstructured.SetNestedField(crds[i].Object, namespace, "spec", "conversion", "webhook", "clientConfig", "service", "namespace"); err != nil {
			return fmt.Errorf("failed to set CRD conversion webhook namespace for %s: %w", crds[i].GetName(), err)
		}
	}
	return nil
}

func applyProviderCRD(ctx context.Context, k8sClient client.Client, desired client.Object) (string, error) {
	if desired == nil {
		return "", fmt.Errorf("desired CRD is nil")
	}
	created := desired.DeepCopyObject()
	createdObject, ok := created.(client.Object)
	if !ok {
		return "", fmt.Errorf("desired CRD %T is not a client object", desired)
	}
	if err := k8sClient.Create(ctx, createdObject); err == nil {
		return "created", nil
	} else if !errors.IsAlreadyExists(err) {
		return "", err
	}

	existingObject := &unstructured.Unstructured{}
	existingObject.SetAPIVersion(desired.GetObjectKind().GroupVersionKind().GroupVersion().String())
	existingObject.SetKind(desired.GetObjectKind().GroupVersionKind().Kind)
	if existingObject.GetAPIVersion() == "" || existingObject.GetKind() == "" {
		existingObject.SetAPIVersion("apiextensions.k8s.io/v1")
		existingObject.SetKind("CustomResourceDefinition")
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: desired.GetName()}, existingObject); err != nil {
		return "", err
	}

	updatedObject := existingObject.DeepCopy()
	updatedObject.SetLabels(mergeStringMaps(existingObject.GetLabels(), desired.GetLabels()))
	updatedObject.SetAnnotations(mergeStringMaps(existingObject.GetAnnotations(), desired.GetAnnotations()))
	if err := copyMutableCRDSpecFields(updatedObject, desired); err != nil {
		return "", err
	}
	if err := k8sClient.Patch(ctx, updatedObject, client.MergeFrom(existingObject)); err != nil {
		return "", err
	}
	return "updated", nil
}

func copyMutableCRDSpecFields(target *unstructured.Unstructured, source client.Object) error {
	sourceObject, ok := source.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("desired CRD %T is not an unstructured object", source)
	}
	for _, path := range [][]string{
		{"spec", "versions"},
		{"spec", "conversion"},
		{"spec", "preserveUnknownFields"},
	} {
		if err := copyNestedField(target, sourceObject, path...); err != nil {
			return err
		}
	}
	return nil
}

func copyNestedField(target, source *unstructured.Unstructured, path ...string) error {
	value, found, err := unstructured.NestedFieldNoCopy(source.Object, path...)
	if err != nil {
		return fmt.Errorf("failed to read desired CRD field %v: %w", path, err)
	}
	if !found {
		unstructured.RemoveNestedField(target.Object, path...)
		return nil
	}
	if err := unstructured.SetNestedField(target.Object, apiruntime.DeepCopyJSONValue(value), path...); err != nil {
		return fmt.Errorf("failed to set CRD field %v: %w", path, err)
	}
	return nil
}

func mergeStringMaps(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(override))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range override {
		merged[key] = value
	}
	return merged
}
