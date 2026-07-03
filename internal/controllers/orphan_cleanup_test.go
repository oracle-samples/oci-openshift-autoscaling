/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	capiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	"github.com/openshift/oci-capi-operator/internal/components/capoci"
	enableautoscaler "github.com/openshift/oci-capi-operator/internal/components/enable_autoscaler"
	"github.com/openshift/oci-capi-operator/internal/utils"
	infrastructurev1beta2 "github.com/oracle/cluster-api-provider-oci/api/v1beta2"
	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCleanupOrphanOCIInstancesTerminatesOnlyTaggedOrphans(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := capiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := infrastructurev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	autoscaler := &capiv1alpha1.OCIClusterAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ociclusterautoscaler",
			Namespace: "oci-openshift-autoscaling-operator",
		},
	}
	activeMachine := &infrastructurev1beta2.OCIMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active-machine",
			Namespace: CAPISystemNamespace,
			Labels: map[string]string{
				"cluster.x-k8s.io/cluster-name": "test-cluster",
			},
		},
	}

	oldTime := common.SDKTime{Time: time.Now().Add(-(orphanInstanceGracePeriod + time.Minute))}
	youngTime := common.SDKTime{Time: time.Now().Add(-time.Minute)}
	computeClient := &fakeOrphanComputeClient{
		listResponse: core.ListInstancesResponse{
			Items: []core.Instance{
				{
					Id:             common.String("orphan-id"),
					DisplayName:    common.String("orphan-machine"),
					LifecycleState: core.InstanceLifecycleStateRunning,
					TimeCreated:    &oldTime,
					FreeformTags:   autoscalerInstanceTags(autoscaler, "test-cluster"),
				},
				{
					Id:             common.String("active-id"),
					DisplayName:    common.String("active-machine"),
					LifecycleState: core.InstanceLifecycleStateRunning,
					TimeCreated:    &oldTime,
					FreeformTags:   autoscalerInstanceTags(autoscaler, "test-cluster"),
				},
				{
					Id:             common.String("young-id"),
					DisplayName:    common.String("young-machine"),
					LifecycleState: core.InstanceLifecycleStateRunning,
					TimeCreated:    &youngTime,
					FreeformTags:   autoscalerInstanceTags(autoscaler, "test-cluster"),
				},
				{
					Id:             common.String("untagged-id"),
					DisplayName:    common.String("untagged-machine"),
					LifecycleState: core.InstanceLifecycleStateRunning,
					TimeCreated:    &oldTime,
				},
			},
		},
	}

	reconciler := &OCIClusterAutoscalerReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(activeMachine).
			Build(),
		ComputeClientFactory: func(context.Context, capoci.CAPOCICredentials) (ociComputeClient, error) {
			return computeClient, nil
		},
	}
	config := enableautoscaler.Config{
		ClusterConfig: enableautoscaler.ClusterConfig{
			CompartmentID: "ocid1.compartment.oc1..example",
		},
	}

	if err := reconciler.cleanupOrphanOCIInstances(ctx, autoscaler, config, "test-cluster"); err != nil {
		t.Fatalf("cleanupOrphanOCIInstances() error = %v", err)
	}
	if got, want := computeClient.terminatedIDs, []string{"orphan-id"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("terminated IDs = %v, want %v", got, want)
	}
}

func TestOrphanCleanupFailureBackoff(t *testing.T) {
	reconciler := &OCIClusterAutoscalerReconciler{}
	now := time.Now()

	if skip, _, _ := reconciler.shouldSkipOrphanCleanup(now); skip {
		t.Fatal("should not skip before any cleanup failure")
	}

	reconciler.recordOrphanCleanupFailure(now, errors.New("metadata service unavailable"))
	skip, retryAfter, previousError := reconciler.shouldSkipOrphanCleanup(now.Add(time.Minute))
	if !skip {
		t.Fatal("should skip while cleanup failure backoff is active")
	}
	if retryAfter <= 0 {
		t.Fatalf("retryAfter = %s, want positive duration", retryAfter)
	}
	if previousError != "metadata service unavailable" {
		t.Fatalf("previousError = %q, want metadata service unavailable", previousError)
	}

	if skip, _, _ := reconciler.shouldSkipOrphanCleanup(now.Add(orphanCleanupFailureRetry + time.Second)); skip {
		t.Fatal("should not skip after cleanup failure backoff expires")
	}

	reconciler.recordOrphanCleanupFailure(now, errors.New("temporary error"))
	reconciler.recordOrphanCleanupSuccess()
	if skip, _, _ := reconciler.shouldSkipOrphanCleanup(now.Add(time.Minute)); skip {
		t.Fatal("should not skip after successful cleanup clears backoff")
	}
}

func autoscalerInstanceTags(autoscaler *capiv1alpha1.OCIClusterAutoscaler, clusterName string) map[string]string {
	return map[string]string{
		utils.OCIInstanceManagedByTag:             autoscaler.Name,
		utils.OCIInstanceAutoscalerNamespaceTag:   autoscaler.Namespace,
		utils.OCIInstanceAutoscalerClusterNameTag: clusterName,
	}
}

type fakeOrphanComputeClient struct {
	listResponse  core.ListInstancesResponse
	terminatedIDs []string
}

func (f *fakeOrphanComputeClient) ListInstances(context.Context, core.ListInstancesRequest) (core.ListInstancesResponse, error) {
	return f.listResponse, nil
}

func (f *fakeOrphanComputeClient) ListVnicAttachments(context.Context, core.ListVnicAttachmentsRequest) (core.ListVnicAttachmentsResponse, error) {
	return core.ListVnicAttachmentsResponse{}, nil
}

func (f *fakeOrphanComputeClient) TerminateInstance(_ context.Context, request core.TerminateInstanceRequest) (core.TerminateInstanceResponse, error) {
	if request.InstanceId != nil {
		f.terminatedIDs = append(f.terminatedIDs, *request.InstanceId)
	}
	return core.TerminateInstanceResponse{}, nil
}
