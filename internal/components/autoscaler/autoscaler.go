/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package autoscaler

import (
	"context"
	"reflect"
	"strings"
	"time"

	helmclient "github.com/mittwald/go-helm-client"
	capiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	"github.com/openshift/oci-capi-operator/internal/components"
	"github.com/openshift/oci-capi-operator/internal/utils"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

// InstallAutoscaler installs the cluster-autoscaler Helm chart
func InstallAutoscaler(instance *capiv1alpha1.OCIClusterAutoscaler, values *AutoscalerDeploymentValues, restConfig *rest.Config) error {
	valuesStr := GetValuesString(values)
	logger := ctrllog.Log.WithName("autoscaler").WithValues("release", values.Name, "namespace", values.Namespace)
	logger.Info("Preparing autoscaler Helm install",
		"chart", values.Chart,
		"version", values.Version,
		"cloudProvider", values.CloudProvider,
		"serviceAccount", values.ServiceAccountName,
	)
	helmClient, err := utils.GetHelmClient(values.Namespace, restConfig)
	if err != nil {
		return err
	}
	chartExists, err := utils.ChartExists(helmClient, values.Chart)
	if err != nil {
		return err
	}
	if !chartExists {
		logger.Info("Chart not found locally; adding repository", "chart", values.Chart, "repoURL", values.RepositoryURL)
		err = utils.AddChartRepo(helmClient, values.Name, values.RepositoryURL)
		if err != nil {
			return err
		}
	}
	releaseExists, err := utils.ReleaseExists(helmClient, values.Name)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return err
	}
	chartSpec := &helmclient.ChartSpec{
		ReleaseName: values.Name,
		ChartName:   values.Chart,
		Namespace:   values.Namespace,
		ValuesYaml:  valuesStr,
		Version:     values.Version,
		Wait:        true,
		Timeout:     300 * time.Second,
		Labels:      utils.GetDefaultLabels(instance.Name),
	}

	if releaseExists {
		upToDate, err := autoscalerReleaseUpToDate(helmClient, values, valuesStr)
		if err != nil {
			logger.Info("Unable to compare existing autoscaler release; upgrading in place", "reason", err.Error())
		} else if upToDate {
			logger.Info("Autoscaler release already up to date; skipping Helm upgrade",
				"chart", values.Chart,
				"version", values.Version,
			)
			return nil
		}

		logger.Info("Autoscaler release already exists; upgrading in place")
		_, err = helmClient.UpgradeChart(context.Background(), chartSpec, &helmclient.GenericHelmOptions{})
		if err != nil {
			return err
		}
		logger.Info("Autoscaler Helm upgrade completed", "chart", values.Chart, "version", values.Version)
		return nil
	}

	err = utils.InstallHelmChart(helmClient, chartSpec)
	if err != nil {
		return err
	}
	logger.Info("Autoscaler Helm install completed", "chart", values.Chart, "version", values.Version)
	return nil
}

func autoscalerReleaseUpToDate(helmClient helmclient.Client, values *AutoscalerDeploymentValues, desiredValuesYAML string) (bool, error) {
	release, err := helmClient.GetRelease(values.Name)
	if err != nil {
		return false, err
	}
	if release == nil || release.Chart == nil || release.Chart.Metadata == nil {
		return false, nil
	}

	currentValues, err := helmClient.GetReleaseValues(values.Name, false)
	if err != nil {
		return false, err
	}
	return helmReleaseMatchesDesired(release.Chart.Metadata.Version, currentValues, values, desiredValuesYAML)
}

func helmReleaseMatchesDesired(currentChartVersion string, currentValues map[string]interface{}, values *AutoscalerDeploymentValues, desiredValuesYAML string) (bool, error) {
	if currentChartVersion != values.Version {
		return false, nil
	}

	desiredValues := map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(desiredValuesYAML), &desiredValues); err != nil {
		return false, err
	}
	normalizedCurrentValues, err := normalizeHelmValues(currentValues)
	if err != nil {
		return false, err
	}
	normalizedDesiredValues, err := normalizeHelmValues(desiredValues)
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(normalizedCurrentValues, normalizedDesiredValues), nil
}

func normalizeHelmValues(values map[string]interface{}) (map[string]interface{}, error) {
	data, err := yaml.Marshal(values)
	if err != nil {
		return nil, err
	}
	normalized := map[string]interface{}{}
	if err := yaml.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func RemoveAutoscaler(values *AutoscalerDeploymentValues, restConfig *rest.Config) error {
	logger := ctrllog.Log.WithName("autoscaler").WithValues("release", values.Name, "namespace", values.Namespace)
	logger.Info("Preparing autoscaler Helm uninstall", "chart", values.Chart)
	helmClient, err := utils.GetHelmClient(values.Namespace, restConfig)
	if err != nil {
		return err
	}
	releaseExists, err := utils.ReleaseExists(helmClient, values.Name)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return err
	}
	if !releaseExists {
		logger.Info("Autoscaler release does not exist; skipping removal")
		return nil
	}
	chartSpec := &helmclient.ChartSpec{
		ReleaseName: values.Name,
		ChartName:   values.Chart,
		Namespace:   values.Namespace,
	}
	if err := utils.RemoveHelmChart(helmClient, chartSpec); err != nil {
		return err
	}
	logger.Info("Autoscaler Helm uninstall completed", "chart", values.Chart)
	return nil
}

// GetComponents returns a Component for the cluster-autoscaler which includes a list of subcomponents
func GetComponents(values *AutoscalerDeploymentValues, instance *capiv1alpha1.OCIClusterAutoscaler, scheme *runtime.Scheme) *components.Component {
	clusterRole, clusterRoleMutateFn := ClusterRole(values.Name, instance)
	clusterRoleBinding, clusterRoleBindingMutateFn := ClusterRoleBinding(values, instance)

	return &components.Component{
		Name: "Autoscaler",
		Subcomponents: components.SubcomponentList{
			{Name: "clusterRole", Object: clusterRole, MutateFn: clusterRoleMutateFn},
			{Name: "clusterRoleBinding", Object: clusterRoleBinding, MutateFn: clusterRoleBindingMutateFn},
		},
	}
}
