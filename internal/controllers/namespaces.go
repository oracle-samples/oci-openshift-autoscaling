/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package controllers

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

type NamespaceConfig struct {
	OperatorNamespace            string `envconfig:"OPERATOR_NAMESPACE" default:"oci-openshift-autoscaling-operator"`
	CAPIProviderNamespace        string `envconfig:"CAPI_PROVIDER_NAMESPACE" default:""`
	CAPOCIProviderNamespace      string `envconfig:"CAPOCI_PROVIDER_NAMESPACE" default:""`
	ManagedResourceNamespace     string `envconfig:"MANAGED_RESOURCE_NAMESPACE" default:""`
	AutoscalerNamespace          string `envconfig:"AUTOSCALER_NAMESPACE" default:""`
	AutoscalerDiscoveryNamespace string `envconfig:"AUTOSCALER_DISCOVERY_NAMESPACE" default:""`
}

func (c NamespaceConfig) WithDefaults() NamespaceConfig {
	c.OperatorNamespace = defaultNamespace(c.OperatorNamespace, DefaultOperatorNamespace)
	c.CAPIProviderNamespace = defaultNamespace(c.CAPIProviderNamespace, c.OperatorNamespace)
	c.CAPOCIProviderNamespace = defaultNamespace(c.CAPOCIProviderNamespace, c.OperatorNamespace)
	c.ManagedResourceNamespace = defaultNamespace(c.ManagedResourceNamespace, c.OperatorNamespace)
	c.AutoscalerNamespace = defaultNamespace(c.AutoscalerNamespace, c.OperatorNamespace)
	c.AutoscalerDiscoveryNamespace = defaultNamespace(c.AutoscalerDiscoveryNamespace, c.ManagedResourceNamespace)
	return c
}

func (c NamespaceConfig) Validate() error {
	c = c.WithDefaults()
	fields := []struct {
		name  string
		value string
	}{
		{name: "operator namespace", value: c.OperatorNamespace},
		{name: "CAPI provider namespace", value: c.CAPIProviderNamespace},
		{name: "CAPOCI provider namespace", value: c.CAPOCIProviderNamespace},
		{name: "managed resource namespace", value: c.ManagedResourceNamespace},
		{name: "autoscaler namespace", value: c.AutoscalerNamespace},
		{name: "autoscaler discovery namespace", value: c.AutoscalerDiscoveryNamespace},
	}
	for _, field := range fields {
		if err := validateRequiredNamespace(field.name, field.value); err != nil {
			return err
		}
	}
	return nil
}

func validateRequiredNamespace(name, value string) error {
	if errs := validation.IsDNS1123Label(value); len(errs) > 0 {
		return fmt.Errorf("%s %q must be a valid Kubernetes namespace: %s", name, value, strings.Join(errs, "; "))
	}
	return nil
}

func validateOptionalNamespace(name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return validateRequiredNamespace(name, value)
}

func validateOptionalObjectName(name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if errs := validation.IsDNS1123Label(value); len(errs) > 0 {
		return fmt.Errorf("%s %q must be a valid Kubernetes object name: %s", name, value, strings.Join(errs, "; "))
	}
	return nil
}

func defaultNamespace(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func (r *OCIClusterAutoscalerReconciler) Namespaces() NamespaceConfig {
	if r == nil {
		return NamespaceConfig{}.WithDefaults()
	}
	return r.NamespaceConfig.WithDefaults()
}
