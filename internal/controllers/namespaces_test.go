/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package controllers

import (
	"context"
	"testing"

	capiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	infrastructurev1beta2 "github.com/oracle/cluster-api-provider-oci/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNamespaceConfigDefaults(t *testing.T) {
	namespaces := NamespaceConfig{
		OperatorNamespace: "operator-ns",
	}.WithDefaults()

	if namespaces.CAPIProviderNamespace != "operator-ns" {
		t.Fatalf("CAPIProviderNamespace = %q, want operator-ns", namespaces.CAPIProviderNamespace)
	}
	if namespaces.CAPOCIProviderNamespace != "operator-ns" {
		t.Fatalf("CAPOCIProviderNamespace = %q, want operator-ns", namespaces.CAPOCIProviderNamespace)
	}
	if namespaces.AutoscalerNamespace != "operator-ns" {
		t.Fatalf("AutoscalerNamespace = %q, want operator-ns", namespaces.AutoscalerNamespace)
	}
	if namespaces.ManagedResourceNamespace != "operator-ns" {
		t.Fatalf("ManagedResourceNamespace = %q, want operator-ns", namespaces.ManagedResourceNamespace)
	}
	if namespaces.AutoscalerDiscoveryNamespace != "operator-ns" {
		t.Fatalf("AutoscalerDiscoveryNamespace = %q, want operator-ns", namespaces.AutoscalerDiscoveryNamespace)
	}
}

func TestNamespaceConfigAllowsNonDefaultNamespaces(t *testing.T) {
	namespaces := NamespaceConfig{
		OperatorNamespace:            "operator-ns",
		CAPIProviderNamespace:        "capi-ns",
		CAPOCIProviderNamespace:      "capoci-ns",
		ManagedResourceNamespace:     "managed-ns",
		AutoscalerNamespace:          "autoscaler-ns",
		AutoscalerDiscoveryNamespace: "discovery-ns",
	}.WithDefaults()

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

func TestNamespaceConfigValidation(t *testing.T) {
	err := (NamespaceConfig{OperatorNamespace: "Invalid_Namespace"}).Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want invalid namespace error")
	}
}

func TestValidateOptionalNamespace(t *testing.T) {
	if err := validateOptionalNamespace("namespace", ""); err != nil {
		t.Fatalf("validateOptionalNamespace(empty) error = %v, want nil", err)
	}
	if err := validateOptionalNamespace("namespace", "valid-ns"); err != nil {
		t.Fatalf("validateOptionalNamespace(valid) error = %v, want nil", err)
	}
	if err := validateOptionalNamespace("namespace", "Invalid_Namespace"); err == nil {
		t.Fatal("validateOptionalNamespace(invalid) error = nil, want error")
	}
}

func TestGetAutoscalerDeploymentValuesUsesConfiguredNamespaces(t *testing.T) {
	instance := &capiv1alpha1.OCIClusterAutoscaler{}
	namespaces := NamespaceConfig{
		OperatorNamespace:            "operator-ns",
		ManagedResourceNamespace:     "managed-ns",
		AutoscalerNamespace:          "autoscaler-ns",
		AutoscalerDiscoveryNamespace: "discovery-ns",
	}

	values := getAutoscalerDeploymentValues(instance, namespaces)
	if values.Namespace != "autoscaler-ns" {
		t.Fatalf("Namespace = %q, want autoscaler-ns", values.Namespace)
	}
	if values.AutoDiscoveryNamespace != "discovery-ns" {
		t.Fatalf("AutoDiscoveryNamespace = %q, want discovery-ns", values.AutoDiscoveryNamespace)
	}

	values = getAutoscalerDeploymentValues(instance, namespaces)
	if values.Namespace != "autoscaler-ns" {
		t.Fatalf("Namespace changed to %q, want autoscaler-ns", values.Namespace)
	}
}

func TestGetAutoscalerDeploymentValuesDefaultsDiscoveryToCAPIResourceNamespace(t *testing.T) {
	instance := &capiv1alpha1.OCIClusterAutoscaler{}
	namespaces := NamespaceConfig{
		OperatorNamespace:        "operator-ns",
		ManagedResourceNamespace: "managed-ns",
	}

	values := getAutoscalerDeploymentValues(instance, namespaces)
	if values.AutoDiscoveryNamespace != "managed-ns" {
		t.Fatalf("AutoDiscoveryNamespace = %q, want managed-ns", values.AutoDiscoveryNamespace)
	}
}

func TestClusterNameForUsesSpecOverride(t *testing.T) {
	instance := &capiv1alpha1.OCIClusterAutoscaler{
		Spec: capiv1alpha1.OCIClusterAutoscalerSpec{
			CAPI: capiv1alpha1.CAPIConfig{
				ClusterName: " custom-cluster ",
			},
		},
	}

	clusterName, err := clusterNameFor(context.Background(), nil, instance)
	if err != nil {
		t.Fatalf("clusterNameFor() error = %v, want nil", err)
	}
	if clusterName != "custom-cluster" {
		t.Fatalf("clusterNameFor() = %q, want custom-cluster", clusterName)
	}
}

func TestRequestsForOCIMachineUsesEffectiveManagedResourceNamespace(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := capiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	owner := &capiv1alpha1.OCIClusterAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ociclusterautoscaler",
			Namespace: "operator-ns",
		},
	}
	reconciler := &OCIClusterAutoscalerReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(owner).
			Build(),
		NamespaceConfig: NamespaceConfig{
			OperatorNamespace:        "operator-ns",
			ManagedResourceNamespace: "managed-ns",
		},
	}

	machine := &infrastructurev1beta2.OCIMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-1",
			Namespace: "managed-ns",
		},
	}
	requests := reconciler.requestsForOCIMachine(ctx, machine)
	if len(requests) != 1 || requests[0].NamespacedName.Namespace != "operator-ns" || requests[0].NamespacedName.Name != "ociclusterautoscaler" {
		t.Fatalf("requestsForOCIMachine() = %#v, want singleton owner", requests)
	}

	machine.Namespace = "other-ns"
	if requests := reconciler.requestsForOCIMachine(ctx, machine); len(requests) != 0 {
		t.Fatalf("requestsForOCIMachine() = %#v, want no requests for unrelated namespace", requests)
	}
}
