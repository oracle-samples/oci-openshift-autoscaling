/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	capiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	"github.com/openshift/oci-capi-operator/internal/components/capoci"
	enableautoscaler "github.com/openshift/oci-capi-operator/internal/components/enable_autoscaler"
	"github.com/openshift/oci-capi-operator/internal/utils"
	infrastructurev1beta2 "github.com/oracle/cluster-api-provider-oci/api/v1beta2"
	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/core"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	orphanInstanceGracePeriod = 10 * time.Minute
	orphanCleanupFailureRetry = 10 * time.Minute
)

type ociComputeClient interface {
	ListInstances(context.Context, core.ListInstancesRequest) (core.ListInstancesResponse, error)
	ListVnicAttachments(context.Context, core.ListVnicAttachmentsRequest) (core.ListVnicAttachmentsResponse, error)
	TerminateInstance(context.Context, core.TerminateInstanceRequest) (core.TerminateInstanceResponse, error)
}

type computeClientFactory func(context.Context, capoci.CAPOCICredentials) (ociComputeClient, error)

func (r *OCIClusterAutoscalerReconciler) cleanupOrphanOCIInstances(ctx context.Context, instance *capiv1alpha1.OCIClusterAutoscaler, config enableautoscaler.Config, clusterName string) error {
	logger := log.FromContext(ctx)
	compartmentID := strings.TrimSpace(config.ClusterConfig.CompartmentID)
	if compartmentID == "" {
		return nil
	}

	activeNames, err := r.activeOCIMachineNames(ctx, clusterName)
	if err != nil {
		return err
	}

	computeClient, err := r.getComputeClient(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	var page *string
	for {
		response, err := computeClient.ListInstances(ctx, core.ListInstancesRequest{
			CompartmentId: common.String(compartmentID),
			Page:          page,
			Limit:         common.Int(100),
			SortBy:        core.ListInstancesSortByTimecreated,
			SortOrder:     core.ListInstancesSortOrderDesc,
		})
		if err != nil {
			return fmt.Errorf("list OCI instances for orphan cleanup: %w", err)
		}

		for _, ociInstance := range response.Items {
			if !shouldTerminateOrphanInstance(ociInstance, instance, clusterName, activeNames, now) {
				continue
			}
			logger.Info("Terminating orphan OCI instance",
				"displayName", stringValue(ociInstance.DisplayName),
				"instanceID", stringValue(ociInstance.Id),
				"lifecycleState", ociInstance.LifecycleState,
			)
			if _, err := computeClient.TerminateInstance(ctx, core.TerminateInstanceRequest{
				InstanceId:                         ociInstance.Id,
				PreserveBootVolume:                 common.Bool(false),
				PreserveDataVolumesCreatedAtLaunch: common.Bool(false),
			}); err != nil {
				return fmt.Errorf("terminate orphan OCI instance %s: %w", stringValue(ociInstance.Id), err)
			}
		}

		if response.OpcNextPage == nil {
			break
		}
		page = response.OpcNextPage
	}

	return nil
}

func (r *OCIClusterAutoscalerReconciler) activeOCIMachineNames(ctx context.Context, clusterName string) (map[string]struct{}, error) {
	machines := &infrastructurev1beta2.OCIMachineList{}
	if err := r.List(ctx, machines, client.InNamespace(CAPISystemNamespace)); err != nil {
		return nil, fmt.Errorf("list active OCIMachines: %w", err)
	}

	activeNames := map[string]struct{}{}
	for _, machine := range machines.Items {
		if !machine.DeletionTimestamp.IsZero() {
			continue
		}
		if labelClusterName := machine.Labels["cluster.x-k8s.io/cluster-name"]; labelClusterName != "" && labelClusterName != clusterName {
			continue
		}
		activeNames[machine.Name] = struct{}{}
	}
	return activeNames, nil
}

func shouldTerminateOrphanInstance(ociInstance core.Instance, autoscaler *capiv1alpha1.OCIClusterAutoscaler, clusterName string, activeNames map[string]struct{}, now time.Time) bool {
	if autoscaler == nil || ociInstance.Id == nil || ociInstance.DisplayName == nil {
		return false
	}
	if ociInstance.LifecycleState == core.InstanceLifecycleStateTerminated ||
		ociInstance.LifecycleState == core.InstanceLifecycleStateTerminating {
		return false
	}
	if !hasAutoscalerTags(ociInstance.FreeformTags, autoscaler, clusterName) {
		return false
	}
	if _, active := activeNames[*ociInstance.DisplayName]; active {
		return false
	}
	if ociInstance.TimeCreated != nil && now.Sub(ociInstance.TimeCreated.Time) < orphanInstanceGracePeriod {
		return false
	}
	return true
}

func hasAutoscalerTags(tags map[string]string, autoscaler *capiv1alpha1.OCIClusterAutoscaler, clusterName string) bool {
	if tags == nil {
		return false
	}
	return tags[utils.OCIInstanceManagedByTag] == autoscaler.Name &&
		tags[utils.OCIInstanceAutoscalerNamespaceTag] == autoscaler.Namespace &&
		tags[utils.OCIInstanceAutoscalerClusterNameTag] == clusterName
}

func (r *OCIClusterAutoscalerReconciler) getComputeClient(ctx context.Context) (ociComputeClient, error) {
	factory := r.ComputeClientFactory
	if factory == nil {
		factory = newOCIComputeClient
	}
	return factory(ctx, r.CAPOCICredentials)
}

func newOCIComputeClient(_ context.Context, credentials capoci.CAPOCICredentials) (ociComputeClient, error) {
	provider, err := newOCIConfigurationProvider(credentials)
	if err != nil {
		return nil, err
	}

	computeClient, err := core.NewComputeClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("create OCI compute client: %w", err)
	}
	return &computeClient, nil
}

func newOCIConfigurationProvider(credentials capoci.CAPOCICredentials) (common.ConfigurationProvider, error) {
	if credentials.UsesInstancePrincipal() {
		provider, err := auth.InstancePrincipalConfigurationProviderForRegion(common.StringToRegion(credentials.Region))
		if err != nil {
			return nil, fmt.Errorf("create OCI instance principal configuration provider: %w", err)
		}
		return provider, nil
	}

	var passphrase *string
	if strings.TrimSpace(credentials.Passphrase) != "" {
		passphrase = common.String(credentials.Passphrase)
	}
	return common.NewRawConfigurationProvider(
		credentials.TenancyID,
		credentials.UserID,
		credentials.Region,
		credentials.Fingerprint,
		credentials.PrivateKey,
		passphrase,
	), nil
}

func (r *OCIClusterAutoscalerReconciler) shouldSkipOrphanCleanup(now time.Time) (bool, time.Duration, string) {
	r.orphanCleanupBackoffLock.Lock()
	defer r.orphanCleanupBackoffLock.Unlock()

	if r.orphanCleanupBackoffUntil.IsZero() || !now.Before(r.orphanCleanupBackoffUntil) {
		return false, 0, ""
	}
	return true, r.orphanCleanupBackoffUntil.Sub(now), r.orphanCleanupBackoffError
}

func (r *OCIClusterAutoscalerReconciler) recordOrphanCleanupFailure(now time.Time, err error) {
	r.orphanCleanupBackoffLock.Lock()
	defer r.orphanCleanupBackoffLock.Unlock()

	r.orphanCleanupBackoffUntil = now.Add(orphanCleanupFailureRetry)
	if err != nil {
		r.orphanCleanupBackoffError = err.Error()
	}
}

func (r *OCIClusterAutoscalerReconciler) recordOrphanCleanupSuccess() {
	r.orphanCleanupBackoffLock.Lock()
	defer r.orphanCleanupBackoffLock.Unlock()

	r.orphanCleanupBackoffUntil = time.Time{}
	r.orphanCleanupBackoffError = ""
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
