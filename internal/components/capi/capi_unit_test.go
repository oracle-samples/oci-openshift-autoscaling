/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package capi

import (
	"testing"

	securityv1 "github.com/openshift/api/security/v1"
	capiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetComponentsOmitsPrivilegedBootstrapArtifacts(t *testing.T) {
	instance := &capiv1alpha1.OCIClusterAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "test-autoscaler"},
	}

	component := GetComponents("oci-openshift-autoscaling-operator", "capoci-system", "capi-sa", "capoci-sa", instance, false)
	if len(component.Subcomponents) != 3 {
		t.Fatalf("expected 3 subcomponents, got %d", len(component.Subcomponents))
	}

	if _, ok := component.Subcomponents[0].Object.(*securityv1.SecurityContextConstraints); !ok {
		t.Fatalf("expected first subcomponent to be SCC, got %T", component.Subcomponents[0].Object)
	}
	if _, ok := component.Subcomponents[1].Object.(*securityv1.SecurityContextConstraints); !ok {
		t.Fatalf("expected second subcomponent to be SCC, got %T", component.Subcomponents[1].Object)
	}
	if _, ok := component.Subcomponents[2].Object.(*corev1.Namespace); !ok {
		t.Fatalf("expected third subcomponent to be Namespace, got %T", component.Subcomponents[2].Object)
	}
	for _, sub := range component.Subcomponents {
		if sub.Name == "clusterRoleBinding" || sub.Name == "serviceAccountSecret" {
			t.Fatalf("unexpected privileged bootstrap subcomponent %q present", sub.Name)
		}
	}
}
