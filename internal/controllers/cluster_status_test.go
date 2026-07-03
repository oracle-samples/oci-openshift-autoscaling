/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package controllers

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMarkExistingClusterControlPlaneInitialized(t *testing.T) {
	ctx := context.Background()
	cluster := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "cluster.x-k8s.io/v1beta2",
			"kind":       "Cluster",
			"metadata": map[string]interface{}{
				"name":      "test-cluster",
				"namespace": "oci-openshift-autoscaling-operator",
			},
			"status": map[string]interface{}{
				"initialization": map[string]interface{}{
					"infrastructureProvisioned": true,
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(runtime.NewScheme()).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()
	reconciler := &OCIClusterAutoscalerReconciler{Client: c}

	if err := reconciler.markExistingClusterControlPlaneInitialized(ctx, "oci-openshift-autoscaling-operator", "test-cluster"); err != nil {
		t.Fatalf("markExistingClusterControlPlaneInitialized returned error: %v", err)
	}

	got := &unstructured.Unstructured{}
	got.SetAPIVersion("cluster.x-k8s.io/v1beta2")
	got.SetKind("Cluster")
	if err := c.Get(ctx, client.ObjectKey{Namespace: "oci-openshift-autoscaling-operator", Name: "test-cluster"}, got); err != nil {
		t.Fatalf("failed to get Cluster: %v", err)
	}

	initialized, found, err := unstructured.NestedBool(got.Object, "status", "initialization", "controlPlaneInitialized")
	if err != nil {
		t.Fatalf("failed to read controlPlaneInitialized: %v", err)
	}
	if !found || !initialized {
		t.Fatalf("expected controlPlaneInitialized=true, found=%t value=%t", found, initialized)
	}

	conditions, found, err := unstructured.NestedSlice(got.Object, "status", "conditions")
	if err != nil {
		t.Fatalf("failed to read conditions: %v", err)
	}
	if !found {
		t.Fatal("expected ControlPlaneInitialized condition")
	}
	for _, condition := range conditions {
		conditionMap, ok := condition.(map[string]interface{})
		if !ok {
			continue
		}
		if conditionMap["type"] == "ControlPlaneInitialized" {
			if conditionMap["status"] != "True" {
				t.Fatalf("expected ControlPlaneInitialized=True, got %v", conditionMap["status"])
			}
			return
		}
	}
	t.Fatal("expected ControlPlaneInitialized condition")
}
