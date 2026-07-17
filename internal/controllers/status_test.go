/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package controllers

import (
	"context"
	"strings"
	"testing"
	"time"

	capiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSetConditionAndRecord(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	reconciler := &OCIClusterAutoscalerReconciler{EventRecorder: recorder}
	instance := &capiv1alpha1.OCIClusterAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "ociclusterautoscaler",
			Namespace:  "default",
			Generation: 7,
		},
	}

	reconciler.setConditionAndRecord(instance, capiv1alpha1.ConditionProvidersReady, metav1.ConditionFalse, ReasonProviderRolloutPending, "provider deployment is not available")

	condition := meta.FindStatusCondition(instance.Status.Conditions, capiv1alpha1.ConditionProvidersReady)
	if condition == nil {
		t.Fatal("expected ProvidersReady condition")
	}
	if condition.Status != metav1.ConditionFalse {
		t.Fatalf("condition status = %s, want False", condition.Status)
	}
	if condition.ObservedGeneration != 7 {
		t.Fatalf("ObservedGeneration = %d, want 7", condition.ObservedGeneration)
	}

	select {
	case event := <-recorder.Events:
		if !strings.Contains(event, corev1.EventTypeWarning) ||
			!strings.Contains(event, ReasonProviderRolloutPending) ||
			!strings.Contains(event, "provider deployment is not available") {
			t.Fatalf("event = %q, want warning event with reason and message", event)
		}
	case <-time.After(time.Second):
		t.Fatal("expected event to be recorded")
	}

	reconciler.setConditionAndRecord(instance, capiv1alpha1.ConditionProvidersReady, metav1.ConditionFalse, ReasonProviderRolloutPending, "provider deployment is not available")

	select {
	case event := <-recorder.Events:
		t.Fatalf("unexpected duplicate event %q", event)
	default:
	}
}

func TestSetConditionAndRecordSuppressesUnknownEvents(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	reconciler := &OCIClusterAutoscalerReconciler{EventRecorder: recorder}
	instance := &capiv1alpha1.OCIClusterAutoscaler{}

	reconciler.setConditionAndRecord(instance, capiv1alpha1.ConditionProvidersReady, metav1.ConditionUnknown, ReasonProviderRolloutPending, "provider deployment is pending")

	condition := meta.FindStatusCondition(instance.Status.Conditions, capiv1alpha1.ConditionProvidersReady)
	if condition == nil {
		t.Fatal("expected ProvidersReady condition")
	}
	if condition.Status != metav1.ConditionUnknown {
		t.Fatalf("condition status = %s, want Unknown", condition.Status)
	}

	select {
	case event := <-recorder.Events:
		t.Fatalf("unexpected event for Unknown condition %q", event)
	default:
	}
}

func TestCheckDeploymentAvailable(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add apps scheme: %v", err)
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-autoscaler",
			Namespace: "autoscaler-system",
		},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	reconciler := &OCIClusterAutoscalerReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(deployment).
			Build(),
	}

	if err := reconciler.checkDeploymentAvailable(context.Background(), deployment.Namespace, deployment.Name); err != nil {
		t.Fatalf("checkDeploymentAvailable returned error: %v", err)
	}
}

func TestCheckDeploymentAvailableReportsUnavailableDeployment(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add apps scheme: %v", err)
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-autoscaler",
			Namespace: "autoscaler-system",
		},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:    appsv1.DeploymentAvailable,
					Status:  corev1.ConditionFalse,
					Reason:  "MinimumReplicasUnavailable",
					Message: "Deployment does not have minimum availability.",
				},
			},
		},
	}
	reconciler := &OCIClusterAutoscalerReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(deployment).
			Build(),
	}

	err := reconciler.checkDeploymentAvailable(context.Background(), deployment.Namespace, deployment.Name)
	if err == nil {
		t.Fatal("expected unavailable deployment error")
	}
	if !strings.Contains(err.Error(), "MinimumReplicasUnavailable") {
		t.Fatalf("error = %q, want condition reason", err.Error())
	}
}

func TestAutoscalerStackReadyRequiresConcreteStatusFields(t *testing.T) {
	instance := &capiv1alpha1.OCIClusterAutoscaler{}
	setAutoscalerCondition(instance, capiv1alpha1.ConditionProvidersReady, metav1.ConditionTrue, ReasonProvidersReady, "providers ready")
	setAutoscalerCondition(instance, capiv1alpha1.ConditionAutoscalerReady, metav1.ConditionTrue, ReasonAutoscalerReady, "autoscaler ready")
	setAutoscalerCondition(instance, capiv1alpha1.ConditionScalingResourcesReady, metav1.ConditionTrue, ReasonScalingResourcesReady, "scaling resources ready")

	if autoscalerStackReady(instance) {
		t.Fatal("expected stack to require concrete status fields")
	}

	instance.Status.CAPIInstalled = true
	instance.Status.ClusterAutoscalerDeployed = true
	if autoscalerStackReady(instance) {
		t.Fatal("expected stack to require accepted policy")
	}

	setAutoscalerCondition(instance, capiv1alpha1.ConditionPolicyAccepted, metav1.ConditionTrue, ReasonPolicyAccepted, "policy accepted")
	if !autoscalerStackReady(instance) {
		t.Fatal("expected stack to be ready")
	}
}

func TestSetAggregateReadyConditionReportsPolicyRejection(t *testing.T) {
	reconciler := &OCIClusterAutoscalerReconciler{}
	instance := &capiv1alpha1.OCIClusterAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Generation: 3},
	}
	setAutoscalerCondition(instance, capiv1alpha1.ConditionPolicyAccepted, metav1.ConditionFalse, ReasonPolicyRejected, "policy rejected")

	reconciler.setAggregateReadyCondition(instance)

	if instance.Status.Phase != PhaseBlocked {
		t.Fatalf("phase = %s, want %s", instance.Status.Phase, PhaseBlocked)
	}
	ready := meta.FindStatusCondition(instance.Status.Conditions, capiv1alpha1.ConditionReady)
	if ready == nil {
		t.Fatal("expected Ready condition")
	}
	if ready.Status != metav1.ConditionFalse || ready.Reason != ReasonPolicyRejected || ready.Message != "policy rejected" {
		t.Fatalf("Ready condition = %#v, want False PolicyRejected", ready)
	}
	if ready.ObservedGeneration != 3 {
		t.Fatalf("ObservedGeneration = %d, want 3", ready.ObservedGeneration)
	}
}

func TestSetAggregateReadyConditionReportsProviderFailure(t *testing.T) {
	reconciler := &OCIClusterAutoscalerReconciler{}
	instance := &capiv1alpha1.OCIClusterAutoscaler{}
	setAutoscalerCondition(instance, capiv1alpha1.ConditionPolicyAccepted, metav1.ConditionTrue, ReasonPolicyAccepted, "policy accepted")
	setAutoscalerCondition(instance, capiv1alpha1.ConditionProvidersReady, metav1.ConditionFalse, ReasonProviderPrerequisitesFailed, "provider failed")

	reconciler.setAggregateReadyCondition(instance)

	if instance.Status.Phase != PhaseError {
		t.Fatalf("phase = %s, want %s", instance.Status.Phase, PhaseError)
	}
	ready := meta.FindStatusCondition(instance.Status.Conditions, capiv1alpha1.ConditionReady)
	if ready == nil {
		t.Fatal("expected Ready condition")
	}
	if ready.Status != metav1.ConditionFalse || ready.Reason != ReasonProviderPrerequisitesFailed || ready.Message != "provider failed" {
		t.Fatalf("Ready condition = %#v, want provider failure", ready)
	}
}

func TestSetAggregateReadyConditionRequiresPolicyBeforeReady(t *testing.T) {
	reconciler := &OCIClusterAutoscalerReconciler{}
	instance := &capiv1alpha1.OCIClusterAutoscaler{}
	instance.Status.CAPIInstalled = true
	instance.Status.ClusterAutoscalerDeployed = true
	setAutoscalerCondition(instance, capiv1alpha1.ConditionProvidersReady, metav1.ConditionTrue, ReasonProvidersReady, "providers ready")
	setAutoscalerCondition(instance, capiv1alpha1.ConditionAutoscalerReady, metav1.ConditionTrue, ReasonAutoscalerReady, "autoscaler ready")
	setAutoscalerCondition(instance, capiv1alpha1.ConditionScalingResourcesReady, metav1.ConditionTrue, ReasonScalingResourcesReady, "scaling resources ready")

	reconciler.setAggregateReadyCondition(instance)

	if instance.Status.Phase != PhaseReconciling {
		t.Fatalf("phase = %s, want %s", instance.Status.Phase, PhaseReconciling)
	}
	ready := meta.FindStatusCondition(instance.Status.Conditions, capiv1alpha1.ConditionReady)
	if ready == nil {
		t.Fatal("expected Ready condition")
	}
	if ready.Status != metav1.ConditionUnknown || ready.Reason != ReasonReconciling {
		t.Fatalf("Ready condition = %#v, want Unknown Reconciling", ready)
	}
}

func TestSetAggregateReadyConditionReportsReady(t *testing.T) {
	reconciler := &OCIClusterAutoscalerReconciler{}
	instance := &capiv1alpha1.OCIClusterAutoscaler{}
	instance.Status.CAPIInstalled = true
	instance.Status.ClusterAutoscalerDeployed = true
	setAutoscalerCondition(instance, capiv1alpha1.ConditionPolicyAccepted, metav1.ConditionTrue, ReasonPolicyAccepted, "policy accepted")
	setAutoscalerCondition(instance, capiv1alpha1.ConditionProvidersReady, metav1.ConditionTrue, ReasonProvidersReady, "providers ready")
	setAutoscalerCondition(instance, capiv1alpha1.ConditionAutoscalerReady, metav1.ConditionTrue, ReasonAutoscalerReady, "autoscaler ready")
	setAutoscalerCondition(instance, capiv1alpha1.ConditionScalingResourcesReady, metav1.ConditionTrue, ReasonScalingResourcesReady, "scaling resources ready")

	reconciler.setAggregateReadyCondition(instance)

	if instance.Status.Phase != PhaseReady {
		t.Fatalf("phase = %s, want %s", instance.Status.Phase, PhaseReady)
	}
	ready := meta.FindStatusCondition(instance.Status.Conditions, capiv1alpha1.ConditionReady)
	if ready == nil {
		t.Fatal("expected Ready condition")
	}
	if ready.Status != metav1.ConditionTrue || ready.Reason != ReasonReconcileSuccess {
		t.Fatalf("Ready condition = %#v, want True ReconcileSuccess", ready)
	}
}

func TestFirstFalseConditionReturnsFirstFailedStage(t *testing.T) {
	instance := &capiv1alpha1.OCIClusterAutoscaler{}
	setAutoscalerCondition(instance, capiv1alpha1.ConditionAutoscalerReady, metav1.ConditionFalse, ReasonAutoscalerInstallFailed, "autoscaler install failed")
	setAutoscalerCondition(instance, capiv1alpha1.ConditionProvidersReady, metav1.ConditionFalse, ReasonProviderPrerequisitesFailed, "provider prerequisites failed")

	condition := firstFalseCondition(
		instance,
		capiv1alpha1.ConditionPolicyAccepted,
		capiv1alpha1.ConditionProvidersReady,
		capiv1alpha1.ConditionAutoscalerReady,
	)
	if condition == nil {
		t.Fatal("expected a failed condition")
	}
	if condition.Type != capiv1alpha1.ConditionProvidersReady {
		t.Fatalf("condition type = %s, want %s", condition.Type, capiv1alpha1.ConditionProvidersReady)
	}
}
