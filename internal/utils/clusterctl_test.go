/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package utils

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	v1alpha3 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
)

func TestProviderWithVersion(t *testing.T) {
	tests := []struct {
		name            string
		provider        string
		providerVersion string
		want            string
	}{
		{
			name:            "empty version",
			provider:        "oci",
			providerVersion: "",
			want:            "oci",
		},
		{
			name:            "whitespace version",
			provider:        "oci",
			providerVersion: "   ",
			want:            "oci",
		},
		{
			name:            "versioned",
			provider:        "oci",
			providerVersion: "v0.23.0",
			want:            "oci:v0.23.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProviderWithVersion(tt.provider, tt.providerVersion)
			if got != tt.want {
				t.Fatalf("ProviderWithVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateProviderVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{
			name:    "empty",
			version: "",
			wantErr: true,
		},
		{
			name:    "whitespace",
			version: " ",
			wantErr: true,
		},
		{
			name:    "missing v prefix",
			version: "0.22.0",
			wantErr: true,
		},
		{
			name:    "valid semver",
			version: "v0.22.0",
			wantErr: false,
		},
		{
			name:    "valid prerelease",
			version: "v0.22.0-rc.1",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProviderVersion(tt.version)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestProviderRef(t *testing.T) {
	tests := []struct {
		name            string
		provider        string
		providerType    v1alpha3.ProviderType
		versionOverride string
		want            string
		wantErr         bool
	}{
		{
			name:            "uses override for infrastructure provider",
			provider:        "oci",
			providerType:    v1alpha3.InfrastructureProviderType,
			versionOverride: "v0.22.0",
			want:            "oci:v0.22.0",
		},
		{
			name:         "requires explicit infrastructure provider version",
			provider:     "oci",
			providerType: v1alpha3.InfrastructureProviderType,
			wantErr:      true,
		},
		{
			name:         "requires explicit core provider version",
			provider:     "cluster-api",
			providerType: v1alpha3.CoreProviderType,
			wantErr:      true,
		},
		{
			name:         "returns bare provider without matching default",
			provider:     "custom",
			providerType: v1alpha3.InfrastructureProviderType,
			want:         "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := providerRef(tt.provider, tt.providerType, tt.versionOverride)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("providerRef() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("providerRef() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("providerRef() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateGeneratedProviderComponents(t *testing.T) {
	tests := []struct {
		name         string
		namespace    string
		provider     string
		providerType v1alpha3.ProviderType
		objects      []unstructured.Unstructured
		wantErr      bool
	}{
		{
			name:         "accepts compatible core provider components",
			namespace:    "oci-openshift-autoscaling-operator",
			provider:     "cluster-api",
			providerType: v1alpha3.CoreProviderType,
			objects: []unstructured.Unstructured{
				component("Deployment", "oci-openshift-autoscaling-operator", "renamed-core-controller"),
				component("Service", "oci-openshift-autoscaling-operator", "renamed-core-webhook-service"),
				component("ValidatingWebhookConfiguration", "", "renamed-core-validating-webhook"),
				component("MutatingWebhookConfiguration", "", "renamed-core-mutating-webhook"),
				component("CustomResourceDefinition", "", "clusters.cluster.x-k8s.io"),
				component("CustomResourceDefinition", "", "machinedeployments.cluster.x-k8s.io"),
			},
		},
		{
			name:         "rejects core provider missing deployment in target namespace",
			namespace:    "oci-openshift-autoscaling-operator",
			provider:     "cluster-api",
			providerType: v1alpha3.CoreProviderType,
			objects: []unstructured.Unstructured{
				component("Deployment", "wrong-namespace", "some-controller"),
				component("Service", "oci-openshift-autoscaling-operator", "capi-webhook-service"),
				component("ValidatingWebhookConfiguration", "", "capi-validating-webhook-configuration"),
				component("MutatingWebhookConfiguration", "", "capi-mutating-webhook-configuration"),
				component("CustomResourceDefinition", "", "clusters.cluster.x-k8s.io"),
				component("CustomResourceDefinition", "", "machinedeployments.cluster.x-k8s.io"),
			},
			wantErr: true,
		},
		{
			name:         "accepts compatible capoci provider components",
			namespace:    "oci-openshift-autoscaling-operator",
			provider:     "oci",
			providerType: v1alpha3.InfrastructureProviderType,
			objects: []unstructured.Unstructured{
				component("Deployment", "oci-openshift-autoscaling-operator", "renamed-capoci-controller"),
				component("Service", "oci-openshift-autoscaling-operator", "renamed-capoci-webhook-service"),
				component("ValidatingWebhookConfiguration", "", "renamed-capoci-validating-webhook"),
				component("MutatingWebhookConfiguration", "", "renamed-capoci-mutating-webhook"),
				component("CustomResourceDefinition", "", "ociclusters.infrastructure.cluster.x-k8s.io"),
				component("CustomResourceDefinition", "", "ocimachinetemplates.infrastructure.cluster.x-k8s.io"),
			},
		},
		{
			name:         "rejects capoci provider missing key crd",
			namespace:    "oci-openshift-autoscaling-operator",
			provider:     "oci",
			providerType: v1alpha3.InfrastructureProviderType,
			objects: []unstructured.Unstructured{
				component("Deployment", "oci-openshift-autoscaling-operator", "capoci-controller-manager"),
				component("Service", "oci-openshift-autoscaling-operator", "capoci-webhook-service"),
				component("ValidatingWebhookConfiguration", "", "capoci-validating-webhook-configuration"),
				component("MutatingWebhookConfiguration", "", "capoci-mutating-webhook-configuration"),
				component("CustomResourceDefinition", "", "ociclusters.infrastructure.cluster.x-k8s.io"),
			},
			wantErr: true,
		},
		{
			name:         "accepts core provider crd bootstrap path without namespace",
			namespace:    "",
			provider:     "cluster-api",
			providerType: v1alpha3.CoreProviderType,
			objects: []unstructured.Unstructured{
				component("ValidatingWebhookConfiguration", "", "renamed-core-validating-webhook"),
				component("MutatingWebhookConfiguration", "", "renamed-core-mutating-webhook"),
				component("CustomResourceDefinition", "", "clusters.cluster.x-k8s.io"),
				component("CustomResourceDefinition", "", "machinedeployments.cluster.x-k8s.io"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGeneratedProviderComponents(tt.provider, tt.providerType, tt.namespace, tt.objects)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func component(kind, namespace, name string) unstructured.Unstructured {
	obj := unstructured.Unstructured{}
	obj.SetKind(kind)
	obj.SetNamespace(namespace)
	obj.SetName(name)
	return obj
}
