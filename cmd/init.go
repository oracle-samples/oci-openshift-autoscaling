/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package main

import (
	"context"
	"os"

	"github.com/openshift/oci-capi-operator/internal/components/crds"
	"github.com/openshift/oci-capi-operator/internal/controllers"
	"github.com/openshift/oci-capi-operator/internal/utils"

	"github.com/go-logr/logr"
	"github.com/kelseyhightower/envconfig"
	"github.com/spf13/cobra"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	client, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "Failed to create client")
		return err
	}

	providerConfig := controllers.ProviderConfig{}
	if err := envconfig.Process("", &providerConfig); err != nil {
		setupLog.Error(err, "Failed to process provider environment variables")
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
	setupLog.Info("Fetched provider CRDs",
		"capiCount", len(capiCRDs),
		"capociCount", len(capociCRDs),
	)

	// apply all the CRDs
	createdCount := 0
	existingCount := 0
	for _, crd := range append(capiCRDs, capociCRDs...) {
		setupLog.Info("Applying CRD", "name", crd.GetName())
		err = client.Create(ctx, &crd)
		if err != nil && !errors.IsAlreadyExists(err) {
			setupLog.Error(err, "Failed to apply CRD", "name", crd.GetName())
			return err
		}
		if errors.IsAlreadyExists(err) {
			existingCount++
			setupLog.Info("CRD already exists", "name", crd.GetName())
			continue
		}
		createdCount++
		setupLog.Info("Created CRD", "name", crd.GetName())
	}
	setupLog.Info("All CRDs applied successfully", "created", createdCount, "alreadyExists", existingCount)
	return nil
}
