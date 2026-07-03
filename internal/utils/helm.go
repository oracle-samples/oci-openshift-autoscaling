/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package utils

import (
	"context"
	"fmt"
	"strings"

	helmclient "github.com/mittwald/go-helm-client"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/repo"
	"k8s.io/client-go/rest"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// GetHelmClient creates a new Helm client from the provided namespace and configuration
func GetHelmClient(namespace string, cfg *rest.Config) (helmclient.Client, error) {
	logger := ctrllog.Log.WithName("helm").WithValues("namespace", namespace)
	helmClient, err := helmclient.NewClientFromRestConf(&helmclient.RestConfClientOptions{
		Options: &helmclient.Options{
			Namespace: namespace,
		},
		RestConfig: cfg,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating helm client: %w", err)
	}
	logger.V(1).Info("Created Helm client")
	return helmClient, nil
}

// ChartExists checks if a chart exists in the Helm client
func ChartExists(helmClient helmclient.Client, name string) (bool, error) {
	logger := ctrllog.Log.WithName("helm").WithValues("chart", name)
	chart, _, err := helmClient.GetChart(name, &action.ChartPathOptions{})
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return false, fmt.Errorf("error getting chart: %w", err)
	}
	logger.V(1).Info("Checked chart presence", "exists", chart != nil)
	return chart != nil, nil
}

// AddChartRepo adds a chart repository to the Helm client
func AddChartRepo(helmClient helmclient.Client, name, url string) error {
	logger := ctrllog.Log.WithName("helm").WithValues("repo", name, "repoURL", url)
	chartRepo := repo.Entry{
		Name: name,
		URL:  url,
	}
	err := helmClient.AddOrUpdateChartRepo(chartRepo)
	if err != nil {
		return fmt.Errorf("error adding chart repo: %w", err)
	}
	logger.Info("Added or updated chart repository")
	return nil
}

// ReleaseExists checks if a release exists in the Helm client
func ReleaseExists(helmClient helmclient.Client, name string) (bool, error) {
	logger := ctrllog.Log.WithName("helm").WithValues("release", name)
	release, err := helmClient.GetRelease(name)
	if err != nil {
		return false, fmt.Errorf("error getting release: %w", err)
	}
	logger.V(1).Info("Checked release presence", "exists", release != nil)
	return release != nil, nil
}

// InstallHelmChart installs a Helm chart
func InstallHelmChart(helmClient helmclient.Client, chartSpec *helmclient.ChartSpec) error {
	logger := ctrllog.Log.WithName("helm").WithValues(
		"release", chartSpec.ReleaseName,
		"chart", chartSpec.ChartName,
		"namespace", chartSpec.Namespace,
		"version", chartSpec.Version,
		"timeout", chartSpec.Timeout.String(),
	)
	logger.Info("Installing Helm chart")
	_, err := helmClient.InstallChart(context.Background(), chartSpec, &helmclient.GenericHelmOptions{})
	if err != nil {
		return fmt.Errorf("error installing chart: %w", err)
	}
	logger.Info("Helm chart install finished")
	return nil
}

// RemoveHelmChart removes a Helm chart
func RemoveHelmChart(helmClient helmclient.Client, chartSpec *helmclient.ChartSpec) error {
	logger := ctrllog.Log.WithName("helm").WithValues(
		"release", chartSpec.ReleaseName,
		"chart", chartSpec.ChartName,
		"namespace", chartSpec.Namespace,
	)
	logger.Info("Removing Helm chart")
	err := helmClient.UninstallRelease(chartSpec)
	if err != nil {
		return fmt.Errorf("error removing chart: %w", err)
	}
	logger.Info("Helm chart removal finished")
	return nil
}
