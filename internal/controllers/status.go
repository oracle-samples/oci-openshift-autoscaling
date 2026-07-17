/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package controllers

import (
	capiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	PhaseInitializing = "Initializing"
	PhaseBlocked      = "Blocked"
	PhaseReconciling  = "Reconciling"
	PhaseReady        = "Ready"
	PhaseError        = "Error"
	PhaseCleaningUp   = "CleaningUp"

	ReasonInitializing                    = "Initializing"
	ReasonReconciling                     = "Reconciling"
	ReasonPolicyAccepted                  = "PolicyAccepted"
	ReasonPolicyRejected                  = "PolicyRejected"
	ReasonProviderPrerequisitesReady      = "ProviderPrerequisitesReady"
	ReasonProviderPrerequisitesFailed     = "ProviderPrerequisitesFailed"
	ReasonProviderRolloutPending          = "ProviderRolloutPending"
	ReasonProvidersReady                  = "ProvidersReady"
	ReasonAutoscalerRBACFailed            = "AutoscalerRBACFailed"
	ReasonAutoscalerInstallFailed         = "AutoscalerInstallFailed"
	ReasonAutoscalerDeploymentUnavailable = "AutoscalerDeploymentUnavailable"
	ReasonAutoscalerReady                 = "AutoscalerReady"
	ReasonClusterDiscoveryFailed          = "ClusterDiscoveryFailed"
	ReasonScalingConfigFailed             = "ScalingConfigFailed"
	ReasonScalingResourcesFailed          = "ScalingResourcesFailed"
	ReasonScalingResourcesReady           = "ScalingResourcesReady"
	ReasonCleanupStarted                  = "CleanupStarted"
	ReasonCleanupSkipped                  = "CleanupSkipped"
	ReasonCleanupFailed                   = "CleanupFailed"
	ReasonCleanupSucceeded                = "CleanupSucceeded"
	ReasonReconcileError                  = "ReconcileError"
	ReasonReconcileSuccess                = "ReconcileSuccess"
)

func setAutoscalerCondition(instance *capiv1alpha1.OCIClusterAutoscaler, conditionType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: instance.Generation,
	})
}

func conditionIsTrue(instance *capiv1alpha1.OCIClusterAutoscaler, conditionType string) bool {
	condition := meta.FindStatusCondition(instance.Status.Conditions, conditionType)
	return condition != nil && condition.Status == metav1.ConditionTrue
}

func autoscalerStackReady(instance *capiv1alpha1.OCIClusterAutoscaler) bool {
	return instance.Status.CAPIInstalled &&
		instance.Status.ClusterAutoscalerDeployed &&
		conditionIsTrue(instance, capiv1alpha1.ConditionPolicyAccepted) &&
		conditionIsTrue(instance, capiv1alpha1.ConditionProvidersReady) &&
		conditionIsTrue(instance, capiv1alpha1.ConditionAutoscalerReady) &&
		conditionIsTrue(instance, capiv1alpha1.ConditionScalingResourcesReady)
}

func (r *OCIClusterAutoscalerReconciler) setReadyConditionFromFailedStage(instance *capiv1alpha1.OCIClusterAutoscaler) bool {
	failedCondition := firstFalseCondition(
		instance,
		capiv1alpha1.ConditionPolicyAccepted,
		capiv1alpha1.ConditionProvidersReady,
		capiv1alpha1.ConditionAutoscalerReady,
		capiv1alpha1.ConditionScalingResourcesReady,
	)
	if failedCondition == nil {
		return false
	}
	if failedCondition.Type == capiv1alpha1.ConditionPolicyAccepted {
		instance.Status.Phase = PhaseBlocked
	} else {
		instance.Status.Phase = PhaseError
	}
	r.setConditionAndRecord(instance, capiv1alpha1.ConditionReady, metav1.ConditionFalse, failedCondition.Reason, failedCondition.Message)
	return true
}

func (r *OCIClusterAutoscalerReconciler) setAggregateReadyCondition(instance *capiv1alpha1.OCIClusterAutoscaler) {
	instance.Status.ObservedGeneration = instance.Generation
	if r.setReadyConditionFromFailedStage(instance) {
		return
	} else if autoscalerStackReady(instance) {
		instance.Status.Phase = PhaseReady
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionReady, metav1.ConditionTrue, ReasonReconcileSuccess, "OCI CAPI autoscaler is ready")
	} else {
		instance.Status.Phase = PhaseReconciling
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionReady, metav1.ConditionUnknown, ReasonReconciling, "OCI CAPI autoscaler reconciliation is still in progress")
	}
}

func firstFalseCondition(instance *capiv1alpha1.OCIClusterAutoscaler, conditionTypes ...string) *metav1.Condition {
	for _, conditionType := range conditionTypes {
		condition := meta.FindStatusCondition(instance.Status.Conditions, conditionType)
		if condition != nil && condition.Status == metav1.ConditionFalse {
			return condition
		}
	}
	return nil
}

func conditionChanged(instance *capiv1alpha1.OCIClusterAutoscaler, conditionType string, status metav1.ConditionStatus, reason, message string) bool {
	condition := meta.FindStatusCondition(instance.Status.Conditions, conditionType)
	return condition == nil ||
		condition.Status != status ||
		condition.Reason != reason ||
		condition.Message != message
}

func eventTypeForStatus(status metav1.ConditionStatus) string {
	switch status {
	case metav1.ConditionFalse:
		return corev1.EventTypeWarning
	case metav1.ConditionTrue:
		return corev1.EventTypeNormal
	default:
		return ""
	}
}

func (r *OCIClusterAutoscalerReconciler) recordEvent(instance *capiv1alpha1.OCIClusterAutoscaler, eventType, reason, message string) {
	if r == nil || r.EventRecorder == nil || instance == nil {
		return
	}
	r.EventRecorder.Event(instance, eventType, reason, message)
}

func (r *OCIClusterAutoscalerReconciler) setConditionAndRecord(instance *capiv1alpha1.OCIClusterAutoscaler, conditionType string, status metav1.ConditionStatus, reason, message string) {
	shouldRecord := conditionChanged(instance, conditionType, status, reason, message)
	setAutoscalerCondition(instance, conditionType, status, reason, message)
	if shouldRecord {
		if eventType := eventTypeForStatus(status); eventType != "" {
			r.recordEvent(instance, eventType, reason, message)
		}
	}
}
