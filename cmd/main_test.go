/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package main

import (
	"context"
	"testing"

	"github.com/kelseyhightower/envconfig"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/oci-capi-operator/internal/components/capoci"
	"github.com/openshift/oci-capi-operator/internal/controllers"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestValidateOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options Options
		wantErr string
	}{
		{
			name: "accepts valid key based credentials",
			options: Options{
				CAPOCICredentials: capoci.CAPOCICredentials{
					TenancyID:            "ocid1.tenancy.oc1..example",
					UserID:               "ocid1.user.oc1..example",
					Region:               "us-phoenix-1",
					Fingerprint:          "11:22:33",
					PrivateKey:           "-----BEGIN PRIVATE KEY-----",
					UseInstancePrincipal: "false",
				},
				CSRApprovalConfig: CSRApprovalConfig{ClusterName: "test-cluster"},
			},
		},
		{
			name: "accepts valid instance principal credentials",
			options: Options{
				CAPOCICredentials: capoci.CAPOCICredentials{
					Region:               "us-phoenix-1",
					UseInstancePrincipal: "true",
				},
				CSRApprovalConfig: CSRApprovalConfig{ClusterName: "test-cluster"},
			},
		},
		{
			name: "rejects invalid startup credentials",
			options: Options{
				CAPOCICredentials: capoci.CAPOCICredentials{
					UseInstancePrincipal: "false",
				},
				CSRApprovalConfig: CSRApprovalConfig{ClusterName: "test-cluster"},
			},
			wantErr: "key-based auth requires OCI_TENANCY_ID, OCI_USER_ID, OCI_REGION, OCI_CREDENTIALS_FINGERPRINT, OCI_CREDENTIALS_KEY",
		},
		{
			name: "rejects missing CSR cluster scope",
			options: Options{
				CAPOCICredentials: capoci.CAPOCICredentials{
					Region:               "us-phoenix-1",
					UseInstancePrincipal: "true",
				},
			},
			wantErr: "CSR approval requires CSR_CLUSTER_NAME or CLUSTER_NAME",
		},
		{
			name: "rejects invalid namespace config",
			options: Options{
				CAPOCICredentials: capoci.CAPOCICredentials{
					Region:               "us-phoenix-1",
					UseInstancePrincipal: "true",
				},
				NamespaceConfig:   controllers.NamespaceConfig{OperatorNamespace: "Invalid_Namespace"},
				CSRApprovalConfig: CSRApprovalConfig{ClusterName: "test-cluster"},
			},
			wantErr: "operator namespace \"Invalid_Namespace\" must be a valid Kubernetes namespace: a lowercase RFC 1123 label must consist of lower case alphanumeric characters or '-', and must start and end with an alphanumeric character (e.g. 'my-name',  or '123-abc', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateOptions(tt.options)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateOptions() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateOptions() error = nil, want %q", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("validateOptions() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestNamespaceConfigEnvOverrides(t *testing.T) {
	t.Setenv("OPERATOR_NAMESPACE", "operator-ns")
	t.Setenv("CAPI_PROVIDER_NAMESPACE", "capi-ns")
	t.Setenv("CAPOCI_PROVIDER_NAMESPACE", "capoci-ns")
	t.Setenv("MANAGED_RESOURCE_NAMESPACE", "managed-ns")
	t.Setenv("AUTOSCALER_NAMESPACE", "autoscaler-ns")
	t.Setenv("AUTOSCALER_DISCOVERY_NAMESPACE", "discovery-ns")

	options := Options{}
	if err := envconfig.Process("", &options); err != nil {
		t.Fatalf("envconfig.Process() error = %v", err)
	}

	namespaces := options.NamespaceConfig.WithDefaults()
	if namespaces.OperatorNamespace != "operator-ns" {
		t.Fatalf("OperatorNamespace = %q, want operator-ns", namespaces.OperatorNamespace)
	}
	if namespaces.CAPIProviderNamespace != "capi-ns" {
		t.Fatalf("CAPIProviderNamespace = %q, want capi-ns", namespaces.CAPIProviderNamespace)
	}
	if namespaces.CAPOCIProviderNamespace != "capoci-ns" {
		t.Fatalf("CAPOCIProviderNamespace = %q, want capoci-ns", namespaces.CAPOCIProviderNamespace)
	}
	if namespaces.ManagedResourceNamespace != "managed-ns" {
		t.Fatalf("ManagedResourceNamespace = %q, want managed-ns", namespaces.ManagedResourceNamespace)
	}
	if namespaces.AutoscalerNamespace != "autoscaler-ns" {
		t.Fatalf("AutoscalerNamespace = %q, want autoscaler-ns", namespaces.AutoscalerNamespace)
	}
	if namespaces.AutoscalerDiscoveryNamespace != "discovery-ns" {
		t.Fatalf("AutoscalerDiscoveryNamespace = %q, want discovery-ns", namespaces.AutoscalerDiscoveryNamespace)
	}
}

func TestResolveCSRApprovalConfig(t *testing.T) {
	defaultMachineNamespace := "managed-ns"
	tests := []struct {
		name        string
		config      CSRApprovalConfig
		envCluster  string
		infraName   string
		wantCluster string
		wantMachine string
		wantErr     bool
	}{
		{
			name:        "keeps explicit CSR cluster name",
			config:      CSRApprovalConfig{ClusterName: "configured-cluster"},
			infraName:   "infra-cluster",
			wantCluster: "configured-cluster",
			wantMachine: defaultMachineNamespace,
		},
		{
			name:        "keeps explicit CSR machine namespace",
			config:      CSRApprovalConfig{ClusterName: "configured-cluster", MachineNamespace: "custom-machine-ns"},
			infraName:   "infra-cluster",
			wantCluster: "configured-cluster",
			wantMachine: "custom-machine-ns",
		},
		{
			name:        "falls back to legacy CLUSTER_NAME environment variable",
			envCluster:  "env-cluster",
			infraName:   "infra-cluster",
			wantCluster: "env-cluster",
			wantMachine: defaultMachineNamespace,
		},
		{
			name:        "discovers cluster name from OpenShift infrastructure",
			infraName:   "infra-cluster",
			wantCluster: "infra-cluster",
			wantMachine: defaultMachineNamespace,
		},
		{
			name:    "rejects empty resolved cluster scope",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLUSTER_NAME", tt.envCluster)
			scheme := runtime.NewScheme()
			if err := configv1.Install(scheme); err != nil {
				t.Fatalf("install config scheme: %v", err)
			}

			builder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.infraName != "" {
				builder = builder.WithObjects(&configv1.Infrastructure{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Status: configv1.InfrastructureStatus{
						InfrastructureName: tt.infraName,
					},
				})
			}

			config := tt.config
			err := resolveCSRApprovalConfig(context.Background(), builder.Build(), &config, defaultMachineNamespace)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveCSRApprovalConfig() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveCSRApprovalConfig() unexpected error: %v", err)
			}
			if config.ClusterName != tt.wantCluster {
				t.Fatalf("ClusterName = %q, want %q", config.ClusterName, tt.wantCluster)
			}
			if config.MachineNamespace != tt.wantMachine {
				t.Fatalf("MachineNamespace = %q, want %q", config.MachineNamespace, tt.wantMachine)
			}
		})
	}
}

func TestApplyProviderCRDPatchesMutableFieldsOnly(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add apiextensions scheme: %v", err)
	}

	existing := testCRD("widgets.example.com", map[string]interface{}{
		"group": "example.com",
		"names": map[string]interface{}{
			"kind":   "Widget",
			"plural": "widgets",
		},
		"scope": "Namespaced",
		"versions": []interface{}{
			map[string]interface{}{
				"name":    "v1",
				"served":  true,
				"storage": true,
				"schema": map[string]interface{}{
					"openAPIV3Schema": map[string]interface{}{"type": "object"},
				},
			},
		},
	})
	existing.SetLabels(map[string]string{"existing": "true"})
	existing.SetAnnotations(map[string]string{"existing": "true"})

	desired := testCRD("widgets.example.com", map[string]interface{}{
		"group": "changed.example.com",
		"names": map[string]interface{}{
			"kind":   "ChangedWidget",
			"plural": "changedwidgets",
		},
		"scope": "Cluster",
		"versions": []interface{}{
			map[string]interface{}{
				"name":    "v1",
				"served":  true,
				"storage": true,
				"schema": map[string]interface{}{
					"openAPIV3Schema": map[string]interface{}{"type": "object"},
				},
			},
			map[string]interface{}{
				"name":    "v2",
				"served":  true,
				"storage": false,
				"schema": map[string]interface{}{
					"openAPIV3Schema": map[string]interface{}{"type": "object"},
				},
			},
		},
		"conversion": map[string]interface{}{
			"strategy": "None",
		},
	})
	desired.SetLabels(map[string]string{"desired": "true"})
	desired.SetAnnotations(map[string]string{"desired": "true"})

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	applied, err := applyProviderCRD(context.Background(), k8sClient, desired)
	if err != nil {
		t.Fatalf("applyProviderCRD() error = %v", err)
	}
	if applied != "updated" {
		t.Fatalf("applyProviderCRD() = %q, want updated", applied)
	}

	got := testCRD("widgets.example.com", nil)
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "widgets.example.com"}, got); err != nil {
		t.Fatalf("get CRD: %v", err)
	}
	group, _, _ := unstructured.NestedString(got.Object, "spec", "group")
	if group != "example.com" {
		t.Fatalf("spec.group = %q, want existing immutable value", group)
	}
	kind, _, _ := unstructured.NestedString(got.Object, "spec", "names", "kind")
	if kind != "Widget" {
		t.Fatalf("spec.names.kind = %q, want existing immutable value", kind)
	}
	scope, _, _ := unstructured.NestedString(got.Object, "spec", "scope")
	if scope != "Namespaced" {
		t.Fatalf("spec.scope = %q, want existing immutable value", scope)
	}
	versions, found, err := unstructured.NestedSlice(got.Object, "spec", "versions")
	if err != nil || !found {
		t.Fatalf("spec.versions found=%v err=%v", found, err)
	}
	if len(versions) != 2 {
		t.Fatalf("spec.versions length = %d, want 2", len(versions))
	}
	conversionStrategy, found, err := unstructured.NestedString(got.Object, "spec", "conversion", "strategy")
	if err != nil || !found || conversionStrategy != "None" {
		t.Fatalf("spec.conversion.strategy = %q found=%v err=%v, want None", conversionStrategy, found, err)
	}
	if got.GetLabels()["existing"] != "true" || got.GetLabels()["desired"] != "true" {
		t.Fatalf("labels = %#v, want existing and desired labels", got.GetLabels())
	}
	if got.GetAnnotations()["existing"] != "true" || got.GetAnnotations()["desired"] != "true" {
		t.Fatalf("annotations = %#v, want existing and desired annotations", got.GetAnnotations())
	}
}

func testCRD(name string, spec map[string]interface{}) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]interface{}{
				"name": name,
			},
		},
	}
	if spec != nil {
		obj.Object["spec"] = spec
	}
	return obj
}
