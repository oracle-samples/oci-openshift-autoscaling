/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package controllers

import (
	"context"
	stderrors "errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	capiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	"github.com/openshift/oci-capi-operator/internal/components"
	"github.com/openshift/oci-capi-operator/internal/components/autoscaler"
	"github.com/openshift/oci-capi-operator/internal/components/capi"
	"github.com/openshift/oci-capi-operator/internal/components/capoci"
	enableautoscaler "github.com/openshift/oci-capi-operator/internal/components/enable_autoscaler"
	applog "github.com/openshift/oci-capi-operator/internal/logging"

	"github.com/openshift/oci-capi-operator/internal/utils"
	infrastructurev1beta2 "github.com/oracle/cluster-api-provider-oci/api/v1beta2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"

	securityv1 "github.com/openshift/api/security/v1"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const kubeconfigSecretRefreshInterval = 12 * time.Hour

// OCIClusterAutoscalerReconciler reconciles a OCIClusterAutoscaler object
type OCIClusterAutoscalerReconciler struct {
	RestConfig *rest.Config
	client.Client
	Scheme            *runtime.Scheme
	CAPOCICredentials capoci.CAPOCICredentials
	ProviderConfig    ProviderConfig
	CAPOCIProvider    ProviderConfig
	AutoScalingConfig enableautoscaler.Config
	NamespaceConfig   NamespaceConfig
	EventRecorder     record.EventRecorder
}

type ProviderConfig struct {
	CAPIVersion   string `envconfig:"CAPI_VERSION" default:""`
	CAPOCIVersion string `envconfig:"CAPOCI_VERSION" default:""`
	Version       string `envconfig:"CAPOCI_VERSION" default:""`
}

// +kubebuilder:rbac:groups=capi.openshift.io,resources=ociclusterautoscalers;ociclusterautoscalers/status;ociclusterautoscalers/finalizers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;clusters/finalizers;machinetemplates;machinetemplates/status;machinedeployments;machinedeployments/status;machinedeployments/finalizers;clusterclasses;machinedrainrules;machinehealthchecks;machinepools;machines;machinesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=*/status;*/finalizers,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=namespaces;serviceaccounts;secrets;configmaps;services;events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts/token,verbs=create
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterroles/aggregation;clusterrolebindings;roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations;mutatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;create;update
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=ocicluster/status;ocimachines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=ipam.cluster.x-k8s.io,resources=ipaddressclaims;ipaddresses;ipaddressclaims/status,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=runtime.cluster.x-k8s.io,resources=extensionconfigs;extensionconfigs/status,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=authentication.k8s.io;authorization.k8s.io,resources=tokenreviews;subjectaccessreviews,verbs=create
// +kubebuilder:rbac:groups=addons.cluster.x-k8s.io;bootstrap.cluster.x-k8s.io;controlplane.cluster.x-k8s.io;infrastructure.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.openshift.io,resources=infrastructures;networks,verbs=get;list;watch

func (r *OCIClusterAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	instance := &capiv1alpha1.OCIClusterAutoscaler{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("OCIClusterAutoscaler resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get OCIClusterAutoscaler")
		return ctrl.Result{}, err
	}

	// Initialize status if not set
	if instance.Status.Phase == "" {
		instance.Status.Phase = PhaseInitializing
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionReady, metav1.ConditionUnknown, ReasonInitializing, "OCIClusterAutoscaler reconciliation has started")
		if err := r.Status().Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Set up finalizer
	if instance.ObjectMeta.DeletionTimestamp.IsZero() {
		owner, conflict, err := r.singletonConflict(ctx, instance)
		if err != nil {
			return ctrl.Result{}, err
		}
		if conflict {
			message := fmt.Sprintf("OCIClusterAutoscaler is singleton; active owner is %s/%s", owner.Namespace, owner.Name)
			instance.Status.Phase = PhaseBlocked
			instance.Status.ObservedGeneration = instance.Generation
			r.setConditionAndRecord(instance, capiv1alpha1.ConditionPolicyAccepted, metav1.ConditionFalse, ReasonSingletonConflict, message)
			r.setConditionAndRecord(instance, capiv1alpha1.ConditionReady, metav1.ConditionFalse, ReasonSingletonConflict, message)
			return ctrl.Result{RequeueAfter: time.Minute}, r.Status().Update(ctx, instance)
		}
		if !controllerutil.ContainsFinalizer(instance, FinalizerName) {
			controllerutil.AddFinalizer(instance, FinalizerName)
			return ctrl.Result{}, r.Update(ctx, instance)
		}
	} else {
		if controllerutil.ContainsFinalizer(instance, FinalizerName) {
			ownsSharedResources, err := r.isSingletonOwner(ctx, instance)
			if err != nil {
				return ctrl.Result{}, err
			}
			if ownsSharedResources {
				instance.Status.Phase = PhaseCleaningUp
				instance.Status.ObservedGeneration = instance.Generation
				r.setConditionAndRecord(instance, capiv1alpha1.ConditionCleanupSucceeded, metav1.ConditionUnknown, ReasonCleanupStarted, "Deleting managed autoscaler resources")
				if err := r.Status().Update(ctx, instance); err != nil {
					return ctrl.Result{}, err
				}
				if err := r.cleanup(ctx, instance); err != nil {
					logger.Error(err, "Failed to cleanup")
					instance.Status.Phase = PhaseError
					instance.Status.ObservedGeneration = instance.Generation
					message := fmt.Sprintf("Cleanup failed: %v", err)
					r.setConditionAndRecord(instance, capiv1alpha1.ConditionCleanupSucceeded, metav1.ConditionFalse, ReasonCleanupFailed, message)
					r.setConditionAndRecord(instance, capiv1alpha1.ConditionReady, metav1.ConditionFalse, ReasonCleanupFailed, message)
					if updateErr := r.Status().Update(ctx, instance); updateErr != nil {
						logger.Error(updateErr, "Failed to update status after cleanup error")
						return ctrl.Result{}, stderrors.Join(err, fmt.Errorf("failed to update status after cleanup error: %w", updateErr))
					}
					return ctrl.Result{}, err
				}
				instance.Status.ObservedGeneration = instance.Generation
				r.setConditionAndRecord(instance, capiv1alpha1.ConditionCleanupSucceeded, metav1.ConditionTrue, ReasonCleanupSucceeded, "Managed autoscaler resources were deleted")
				if err := r.Status().Update(ctx, instance); err != nil {
					return ctrl.Result{}, err
				}
			} else {
				logger.Info("Skipping cleanup for non-owner OCIClusterAutoscaler", "resource", instance.Name, "namespace", instance.Namespace)
				instance.Status.ObservedGeneration = instance.Generation
				r.setConditionAndRecord(instance, capiv1alpha1.ConditionCleanupSucceeded, metav1.ConditionTrue, ReasonCleanupSkipped, "Cleanup skipped because this resource is not the active singleton owner")
				if err := r.Status().Update(ctx, instance); err != nil {
					return ctrl.Result{}, err
				}
			}
			controllerutil.RemoveFinalizer(instance, FinalizerName)
			return ctrl.Result{}, r.Update(ctx, instance)
		}
		return ctrl.Result{}, nil
	}

	originalStatus := instance.Status.DeepCopy()

	// Reconcile the OCI CAPI stack
	result, err := r.reconcileOCICapiStack(ctx, instance)
	if err != nil {
		if !r.setReadyConditionFromFailedStage(instance) {
			instance.Status.Phase = PhaseError
			r.setConditionAndRecord(instance, capiv1alpha1.ConditionReady, metav1.ConditionFalse, ReasonReconcileError, err.Error())
		}
		instance.Status.ObservedGeneration = instance.Generation
		if updateErr := r.Status().Update(ctx, instance); updateErr != nil {
			logger.Error(updateErr, "Failed to update status after reconcile error")
			return result, stderrors.Join(err, fmt.Errorf("failed to update status after reconcile error: %w", updateErr))
		}
		return result, err
	}

	// Update status
	r.setAggregateReadyCondition(instance)

	if !reflect.DeepEqual(originalStatus, &instance.Status) {
		if err := r.Status().Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
	}

	return result, nil
}

func (r *OCIClusterAutoscalerReconciler) reconcileOCICapiStack(ctx context.Context, instance *capiv1alpha1.OCIClusterAutoscaler) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	useCAPOCIHostNetwork := r.CAPOCICredentials.UsesInstancePrincipal()
	namespaces := r.Namespaces()
	managedResourceNamespace := managedResourceNamespaceFor(instance, namespaces)
	logger.Info("Reconciling OCI CAPI autoscaler stack",
		"resource", instance.Name,
		"namespace", instance.Namespace,
		"useCAPOCIHostNetwork", useCAPOCIHostNetwork,
		"operatorNamespace", namespaces.OperatorNamespace,
		"capiProviderNamespace", namespaces.CAPIProviderNamespace,
		"capociProviderNamespace", namespaces.CAPOCIProviderNamespace,
		"managedResourceNamespace", managedResourceNamespace,
		"autoscalerNamespace", namespaces.AutoscalerNamespace,
		"autoscalerDiscoveryNamespace", namespaces.AutoscalerDiscoveryNamespace,
	)

	// Validate the autoscaler spec
	if err := validate(instance, r.AutoScalingConfig); err != nil {
		logger.Error(err, "Invalid autoscaler spec")
		message := fmt.Sprintf("Autoscaler policy rejected: %v", err)
		instance.Status.Phase = PhaseBlocked
		instance.Status.ObservedGeneration = instance.Generation
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionPolicyAccepted, metav1.ConditionFalse, ReasonPolicyRejected, message)
		r.setReadyConditionFromFailedStage(instance)
		if updateErr := r.Status().Update(ctx, instance); updateErr != nil {
			logger.Error(updateErr, "Failed to update status after policy validation failure")
			return ctrl.Result{}, stderrors.Join(err, fmt.Errorf("failed to update status after policy validation failure: %w", updateErr))
		}
		return ctrl.Result{}, err
	}
	r.setConditionAndRecord(instance, capiv1alpha1.ConditionPolicyAccepted, metav1.ConditionTrue, ReasonPolicyAccepted, "Autoscaler policy accepted")

	// Step 1: Reconcile local prerequisites. Provider install and upgrade are
	// handled outside the steady-state reconcile path, like CCM/CSI manifests.
	capiComponent := capi.GetComponents(namespaces.CAPIProviderNamespace, namespaces.CAPOCIProviderNamespace, CAPIServiceAccountName, CAPOCIServiceAccountName, instance, useCAPOCIHostNetwork)
	err := reconcileComponents(ctx, r.Client, capiComponent)
	if err != nil {
		logger.Error(err, "Failed to reconcile CAPI components")
		message := fmt.Sprintf("Failed to reconcile CAPI provider prerequisites: %v", err)
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionProvidersReady, metav1.ConditionFalse, ReasonProviderPrerequisitesFailed, message)
		return ctrl.Result{}, err
	}
	if !conditionIsTrue(instance, capiv1alpha1.ConditionProvidersReady) {
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionProvidersReady, metav1.ConditionUnknown, ReasonProviderPrerequisitesReady, "Provider prerequisites reconciled; checking provider controller rollout")
	}
	logger.Info("CAPI prerequisites reconciled")

	// Step 2: Reconcile CAPOCI components
	capociComponent := capoci.GetComponents(namespaces.CAPOCIProviderNamespace, instance, &r.CAPOCICredentials)
	err = reconcileComponents(ctx, r.Client, capociComponent)
	if err != nil {
		logger.Error(err, "Failed to reconcile CAPOCI components")
		message := fmt.Sprintf("Failed to reconcile CAPOCI provider configuration: %v", err)
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionProvidersReady, metav1.ConditionFalse, ReasonProviderPrerequisitesFailed, message)
		return ctrl.Result{RequeueAfter: time.Second * 20}, nil
	}
	logger.Info("CAPOCI configuration reconciled")

	// Step 3: Check provider rollouts.
	capiInstalled, err := r.checkCAPIInstallation(ctx, instance)
	if err != nil && !apierrors.IsNotFound(err) {
		logger.Info("Provider controller managers not ready yet; requeuing", "reason", err.Error(), "requeueAfter", "20s")
		instance.Status.CAPIInstalled = false
		message := fmt.Sprintf("Provider controller managers are not ready: %v", err)
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionProvidersReady, metav1.ConditionUnknown, ReasonProviderRolloutPending, message)
		return ctrl.Result{RequeueAfter: time.Second * 20}, nil
	}
	instance.Status.CAPIInstalled = capiInstalled

	if !capiInstalled {
		logger.Info("Provider controller managers are not installed, requeuing...")
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionProvidersReady, metav1.ConditionUnknown, ReasonProviderRolloutPending, "Provider controller managers are not installed yet")
		return ctrl.Result{RequeueAfter: time.Second * 20}, nil
	}
	r.setConditionAndRecord(instance, capiv1alpha1.ConditionProvidersReady, metav1.ConditionTrue, ReasonProvidersReady, "CAPI and CAPOCI provider controller managers are available")
	logger.Info("Provider controller managers are installed")

	// Step 4: Reconcile Cluster Autoscaler components
	autoscalerDeploymentValues := getAutoscalerDeploymentValues(instance, namespaces)

	autoscalerComponents := autoscaler.GetComponents(&autoscalerDeploymentValues, instance, r.Scheme)

	err = reconcileComponents(ctx, r.Client, autoscalerComponents)
	if err != nil {
		logger.Error(err, "Failed to reconcile autoscaler components")
		instance.Status.ClusterAutoscalerDeployed = false
		message := fmt.Sprintf("Failed to reconcile autoscaler RBAC components: %v", err)
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionAutoscalerReady, metav1.ConditionFalse, ReasonAutoscalerRBACFailed, message)
		return ctrl.Result{}, err
	}
	logger.Info("Autoscaler components created",
		"component", autoscalerComponents.Name,
		"subcomponents", subcomponentNames(autoscalerComponents),
	)

	// Install the autoscaler Helm chart
	err = autoscaler.InstallAutoscaler(instance, &autoscalerDeploymentValues, r.RestConfig)
	if err != nil {
		logger.Error(err, "Failed to install autoscaler Helm chart")
		instance.Status.ClusterAutoscalerDeployed = false
		message := fmt.Sprintf("Failed to install or upgrade autoscaler Helm chart: %v", err)
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionAutoscalerReady, metav1.ConditionFalse, ReasonAutoscalerInstallFailed, message)
		return ctrl.Result{}, err
	}
	logger.Info("Autoscaler Helm chart installed",
		"release", autoscalerDeploymentValues.Name,
		"chart", autoscalerDeploymentValues.Chart,
		"version", autoscalerDeploymentValues.Version,
		"namespace", autoscalerDeploymentValues.Namespace,
	)
	if err := r.checkDeploymentAvailable(ctx, autoscalerDeploymentValues.Namespace, autoscalerDeploymentValues.Name); err != nil {
		logger.Info("cluster-autoscaler deployment is not available yet; requeuing", "reason", err.Error(), "requeueAfter", "20s")
		instance.Status.ClusterAutoscalerDeployed = false
		message := fmt.Sprintf("cluster-autoscaler deployment %s/%s is not available: %v", autoscalerDeploymentValues.Namespace, autoscalerDeploymentValues.Name, err)
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionAutoscalerReady, metav1.ConditionFalse, ReasonAutoscalerDeploymentUnavailable, message)
		return ctrl.Result{RequeueAfter: time.Second * 20}, nil
	}
	instance.Status.ClusterAutoscalerDeployed = true
	r.setConditionAndRecord(instance, capiv1alpha1.ConditionAutoscalerReady, metav1.ConditionTrue, ReasonAutoscalerReady, fmt.Sprintf("cluster-autoscaler deployment %s/%s is available", autoscalerDeploymentValues.Namespace, autoscalerDeploymentValues.Name))

	// Step 5: Reconcile Enable Autoscaler components
	clusterName, err := clusterNameFor(ctx, r.Client, instance)
	if err != nil {
		logger.Error(err, "Failed to get cluster name")
		message := fmt.Sprintf("Failed to discover OpenShift cluster name for managed scaling resources: %v", err)
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionScalingResourcesReady, metav1.ConditionFalse, ReasonClusterDiscoveryFailed, message)
		return ctrl.Result{}, err
	}
	if err := enableautoscaler.ValidateNodePoolName(enableautoscaler.NodePoolName(clusterName, instance)); err != nil {
		message := fmt.Sprintf("Invalid generated autoscaler node pool name for cluster %q: %v", clusterName, err)
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionScalingResourcesReady, metav1.ConditionFalse, ReasonScalingConfigFailed, message)
		return ctrl.Result{}, fmt.Errorf("invalid generated autoscaler node pool name: %w", err)
	}

	autoscalerConfig, err := enableautoscaler.SetAutoScalingConfig(ctx, r.Client, instance, r.AutoScalingConfig)
	if err != nil {
		logger.Error(err, "Failed to set autoscaler config")
		message := fmt.Sprintf("Failed to resolve autoscaling configuration: %v", err)
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionScalingResourcesReady, metav1.ConditionFalse, ReasonScalingConfigFailed, message)
		return ctrl.Result{}, err
	}

	// Debug: Log key inputs impacting BM secondary VNIC attachment
	isBM := enableautoscaler.IsBareMetalShape(autoscalerConfig.AutoScalingConfig.Shape)
	logger.Info("EnableAutoscaler config resolved",
		"cluster", clusterName,
		"useInstancePrincipal", r.CAPOCICredentials.UsesInstancePrincipal(),
		"shape", autoscalerConfig.AutoScalingConfig.Shape,
		"isBM", isBM,
		"cpus", autoscalerConfig.AutoScalingConfig.CPUs,
		"memoryGB", autoscalerConfig.AutoScalingConfig.Memory,
		"minNodes", autoscalerConfig.AutoScalingConfig.MinNodes,
		"maxNodes", autoscalerConfig.AutoScalingConfig.MaxNodes,
		"imageID", applog.SafeResourceIdentifier(autoscalerConfig.AutoScalingConfig.ImageID),
		"definedTagsNamespace", autoscalerConfig.AutoScalingConfig.DefinedTagsNamespace,
		"compartmentID", applog.SafeResourceIdentifier(autoscalerConfig.ClusterConfig.CompartmentID),
		"vcnID", applog.SafeResourceIdentifier(autoscalerConfig.NetworkConfig.VCNID),
		"ocpSubnetID", applog.SafeResourceIdentifier(autoscalerConfig.NetworkConfig.OCPSubnetID),
		"ocpSubnetName", autoscalerConfig.NetworkConfig.OCPSubnetName,
		"bareMetalSubnetID", applog.SafeResourceIdentifier(autoscalerConfig.NetworkConfig.BareMetalSubnetID),
		"bareMetalSubnetName", autoscalerConfig.NetworkConfig.BareMetalSubnetName,
		"computeNSGName", autoscalerConfig.NetworkConfig.ComputeNsgName,
		"computeNSGID", applog.SafeResourceIdentifier(autoscalerConfig.NetworkConfig.NetworkSecurityGroupID),
		"apiServerLBID", applog.SafeResourceIdentifier(autoscalerConfig.NetworkConfig.APIServerLoadBalancerID),
		"controlPlaneEndpointConfigured", autoscalerConfig.NetworkConfig.ControlPlaneEndpoint != "",
		"clusterNetworkCIDRConfigured", autoscalerConfig.NetworkConfig.ClusterNetworkCIDRBlock != "",
		"serviceNetworkCIDRConfigured", autoscalerConfig.NetworkConfig.ServiceNetworkCIDRBlock != "",
	)

	enableAutoscalerComponent := enableautoscaler.GetComponents(ctx, r.Client, managedResourceNamespace, namespaces.CAPIProviderNamespace, clusterName, CAPIServiceAccountName, instance, autoscalerConfig, r.CAPOCICredentials.UsesInstancePrincipal())

	err = reconcileComponents(ctx, r.Client, enableAutoscalerComponent)
	if err != nil {
		logger.Error(err, "Failed to reconcile Enable Autoscaler components")
		message := fmt.Sprintf("Failed to reconcile managed scaling resources: %v", err)
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionScalingResourcesReady, metav1.ConditionFalse, ReasonScalingResourcesFailed, message)
		return ctrl.Result{}, err
	}
	logger.Info("Enable Autoscaler components created",
		"component", enableAutoscalerComponent.Name,
		"subcomponents", subcomponentNames(enableAutoscalerComponent),
	)

	if err := r.markExistingClusterControlPlaneInitialized(ctx, managedResourceNamespace, clusterName); err != nil {
		logger.Error(err, "Failed to mark existing CAPI cluster control plane initialized")
		message := fmt.Sprintf("Failed to mark CAPI Cluster %q initialized: %v", clusterName, err)
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionScalingResourcesReady, metav1.ConditionFalse, ReasonScalingResourcesFailed, message)
		return ctrl.Result{}, err
	}
	r.setConditionAndRecord(instance, capiv1alpha1.ConditionScalingResourcesReady, metav1.ConditionTrue, ReasonScalingResourcesReady, fmt.Sprintf("Managed scaling resources for cluster %q are reconciled", clusterName))

	// Debug: Fetch rendered OCIMachineTemplate to verify VNIC attachment state.
	templateName := enableautoscaler.AutoscalingResourceName(clusterName, instance)
	ocimt := &infrastructurev1beta2.OCIMachineTemplate{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: managedResourceNamespace, Name: templateName}, ocimt); err != nil {
		logger.Info("OCIMachineTemplate not found yet (might be created asynchronously)", "name", templateName, "err", err)
	} else {
		s := ocimt.Spec.Template.Spec
		attachments := make([]map[string]any, 0, len(s.VnicAttachments))
		for _, a := range s.VnicAttachments {
			m := map[string]any{"subnetName": a.SubnetName}
			if a.NicIndex != nil {
				m["nicIndex"] = *a.NicIndex
			}
			attachments = append(attachments, m)
		}
		logger.Info("OCIMachineTemplate snapshot",
			"name", ocimt.Name,
			"shape", s.Shape,
			"subnetName", s.SubnetName,
			"definedTagKeys", applog.SortedNestedMapKeys(s.DefinedTags),
			"hasLaunchOptions", s.LaunchOptions != nil,
			"vnicAttachments", attachments,
		)
	}
	return ctrl.Result{RequeueAfter: kubeconfigSecretRefreshInterval}, nil
}

func (r *OCIClusterAutoscalerReconciler) cleanup(ctx context.Context, instance *capiv1alpha1.OCIClusterAutoscaler) error {
	logger := log.FromContext(ctx)
	logger.Info("Starting cleanup of all resources", "resource", instance.Name, "namespace", instance.Namespace)
	namespaces := r.Namespaces()
	managedResourceNamespace := managedResourceNamespaceFor(instance, namespaces)
	autoscalerValues := getAutoscalerDeploymentValues(instance, namespaces)
	clusterName, err := clusterNameFor(ctx, r.Client, instance)
	if err != nil {
		logger.Error(err, "Failed to get cluster name")
		return err
	}
	autoscalerComponents := enableautoscaler.GetComponents(ctx, r.Client, managedResourceNamespace, namespaces.CAPIProviderNamespace, clusterName, CAPIServiceAccountName, instance, r.AutoScalingConfig, r.CAPOCICredentials.UsesInstancePrincipal())

	err = removeComponent(ctx, r.Client, autoscalerComponents)
	if err != nil {
		if meta.IsNoMatchError(err) {
			logger.Info("Skipping enable autoscaler cleanup because API kinds are no longer registered", "error", err)
		} else {
			logger.Error(err, "Failed to remove enable autoscaler components")
			r.setConditionAndRecord(instance, capiv1alpha1.ConditionCleanupSucceeded, metav1.ConditionFalse, ReasonCleanupFailed, fmt.Sprintf("Failed to remove managed scaling resources: %v", err))
			return err
		}
	}
	logger.Info("Enable autoscaler components removed")

	err = autoscaler.RemoveAutoscaler(&autoscalerValues, r.RestConfig)
	if err != nil {
		logger.Error(err, "Failed to remove autoscaler Helm chart")
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionCleanupSucceeded, metav1.ConditionFalse, ReasonCleanupFailed, fmt.Sprintf("Failed to remove autoscaler Helm release: %v", err))
		return err
	}
	logger.Info("Autoscaler Helm chart removed")

	autoscalerRBAC := autoscaler.GetComponents(&autoscalerValues, instance, r.Scheme)
	err = removeComponent(ctx, r.Client, autoscalerRBAC)
	if err != nil {
		logger.Error(err, "Failed to remove autoscaler RBAC components")
		r.setConditionAndRecord(instance, capiv1alpha1.ConditionCleanupSucceeded, metav1.ConditionFalse, ReasonCleanupFailed, fmt.Sprintf("Failed to remove autoscaler RBAC components: %v", err))
		return err
	}
	logger.Info("Autoscaler RBAC components removed")

	logger.Info("Cleanup completed successfully")
	return nil
}

func (r *OCIClusterAutoscalerReconciler) checkCAPIInstallation(ctx context.Context, instance *capiv1alpha1.OCIClusterAutoscaler) (bool, error) {
	namespaces := r.Namespaces()
	if err := r.checkDeploymentAvailable(ctx, namespaces.CAPIProviderNamespace, CAPIDeploymentName); err != nil {
		return false, err
	}
	if err := r.checkDeploymentAvailable(ctx, namespaces.CAPOCIProviderNamespace, CAPOCIDeploymentName); err != nil {
		return false, err
	}
	return true, nil
}

func (r *OCIClusterAutoscalerReconciler) checkDeploymentAvailable(ctx context.Context, namespace, name string) error {
	logger := log.FromContext(ctx)
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, deployment); err != nil {
		return fmt.Errorf("deployment %s/%s is not installed: %w", namespace, name, err)
	}

	condition := utils.GetDeploymentCondition(deployment.Status.Conditions, appsv1.DeploymentAvailable)
	if condition == nil {
		logger.Info("Deployment has no available condition",
			"deployment", deployment.Name,
			"namespace", deployment.Namespace,
			"replicas", deployment.Status.Replicas,
			"readyReplicas", deployment.Status.ReadyReplicas,
			"availableReplicas", deployment.Status.AvailableReplicas,
			"updatedReplicas", deployment.Status.UpdatedReplicas,
		)
		return fmt.Errorf("deployment %s/%s is not available", namespace, name)
	}

	if condition.Status != corev1.ConditionTrue {
		logger.Info("Deployment is not available",
			"deployment", deployment.Name,
			"namespace", deployment.Namespace,
			"replicas", deployment.Status.Replicas,
			"readyReplicas", deployment.Status.ReadyReplicas,
			"availableReplicas", deployment.Status.AvailableReplicas,
			"updatedReplicas", deployment.Status.UpdatedReplicas,
			"conditionReason", condition.Reason,
			"conditionMessage", condition.Message,
		)
		return fmt.Errorf("deployment %s/%s is not available: %s: %s", namespace, name, condition.Reason, condition.Message)
	}

	logger.V(1).Info("Deployment is available",
		"deployment", deployment.Name,
		"namespace", deployment.Namespace,
		"replicas", deployment.Status.Replicas,
		"readyReplicas", deployment.Status.ReadyReplicas,
		"availableReplicas", deployment.Status.AvailableReplicas,
	)
	return nil
}

func validate(instance *capiv1alpha1.OCIClusterAutoscaler, config enableautoscaler.Config) error {
	if err := enableautoscaler.ValidateMinMaxNodes(instance, config); err != nil {
		return fmt.Errorf("invalid Min/Max nodes set in either the autoscaler spec or the config: %w", err)
	}
	if err := validateOptionalNamespace("spec.capi.namespace", instance.Spec.CAPI.Namespace); err != nil {
		return err
	}
	if err := validateOptionalNamespace("spec.clusterAutoscaler.namespace", instance.Spec.ClusterAutoscaler.Namespace); err != nil {
		return err
	}
	if err := validateOptionalObjectName("spec.capi.clusterName", instance.Spec.CAPI.ClusterName); err != nil {
		return err
	}
	if err := validateOptionalObjectName("spec.clusterAutoscaler.name", instance.Spec.ClusterAutoscaler.Name); err != nil {
		return err
	}
	if err := validateOptionalObjectName("spec.clusterAutoscaler.serviceAccountName", instance.Spec.ClusterAutoscaler.ServiceAccountName); err != nil {
		return err
	}
	if clusterName := strings.TrimSpace(instance.Spec.CAPI.ClusterName); clusterName != "" {
		if err := enableautoscaler.ValidateNodePoolName(enableautoscaler.NodePoolName(clusterName, instance)); err != nil {
			return fmt.Errorf("spec.capi.clusterName/spec.autoscaling.poolIdentifier: %w", err)
		}
	}
	return nil
}

func (r *OCIClusterAutoscalerReconciler) singletonConflict(ctx context.Context, instance *capiv1alpha1.OCIClusterAutoscaler) (types.NamespacedName, bool, error) {
	owner, found, err := r.singletonOwner(ctx, false)
	if err != nil || !found {
		return types.NamespacedName{}, false, err
	}
	current := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Name}
	return owner, owner != current, nil
}

func (r *OCIClusterAutoscalerReconciler) isSingletonOwner(ctx context.Context, instance *capiv1alpha1.OCIClusterAutoscaler) (bool, error) {
	owner, found, err := r.singletonOwner(ctx, true)
	if err != nil || !found {
		return false, err
	}
	return owner == types.NamespacedName{Namespace: instance.Namespace, Name: instance.Name}, nil
}

func (r *OCIClusterAutoscalerReconciler) singletonOwner(ctx context.Context, includeDeleting bool) (types.NamespacedName, bool, error) {
	return resolveSingletonOwnerName(ctx, r.Client, includeDeleting)
}

func resolveSingletonOwnerName(ctx context.Context, reader client.Reader, includeDeleting bool) (types.NamespacedName, bool, error) {
	owner, found, err := resolveSingletonOwner(ctx, reader, includeDeleting)
	if err != nil || !found {
		return types.NamespacedName{}, found, err
	}
	return types.NamespacedName{Namespace: owner.Namespace, Name: owner.Name}, true, nil
}

func resolveSingletonOwner(ctx context.Context, reader client.Reader, includeDeleting bool) (capiv1alpha1.OCIClusterAutoscaler, bool, error) {
	list := &capiv1alpha1.OCIClusterAutoscalerList{}
	if err := reader.List(ctx, list); err != nil {
		return capiv1alpha1.OCIClusterAutoscaler{}, false, err
	}
	owner, found := selectSingletonOwner(list.Items, includeDeleting)
	return owner, found, nil
}

func selectSingletonOwner(items []capiv1alpha1.OCIClusterAutoscaler, includeDeleting bool) (capiv1alpha1.OCIClusterAutoscaler, bool) {
	candidates := make([]capiv1alpha1.OCIClusterAutoscaler, 0, len(items))
	for _, item := range items {
		if !includeDeleting && !item.ObjectMeta.DeletionTimestamp.IsZero() {
			continue
		}
		candidates = append(candidates, item)
	}
	if len(candidates) == 0 {
		return capiv1alpha1.OCIClusterAutoscaler{}, false
	}

	sort.Slice(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if !left.CreationTimestamp.Equal(&right.CreationTimestamp) {
			return left.CreationTimestamp.Before(&right.CreationTimestamp)
		}
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		return left.Name < right.Name
	})

	return candidates[0], true
}

func (r *OCIClusterAutoscalerReconciler) requestsForOCIMachine(ctx context.Context, obj client.Object) []reconcile.Request {
	if obj == nil {
		return nil
	}
	owner, found, err := r.singletonOwner(ctx, false)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to map OCIMachine event to OCIClusterAutoscaler")
		return nil
	}
	if !found {
		return nil
	}

	instance := &capiv1alpha1.OCIClusterAutoscaler{}
	if err := r.Get(ctx, owner, instance); err != nil {
		log.FromContext(ctx).Error(err, "Failed to fetch OCIClusterAutoscaler while mapping OCIMachine event")
		return nil
	}
	if obj.GetNamespace() != managedResourceNamespaceFor(instance, r.Namespaces()) {
		return nil
	}
	return []reconcile.Request{{NamespacedName: owner}}
}

// SetupWithManager sets up the controller with the Manager.
func (r *OCIClusterAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capiv1alpha1.OCIClusterAutoscaler{}).
		Watches(&infrastructurev1beta2.OCIMachine{}, handler.EnqueueRequestsFromMapFunc(r.requestsForOCIMachine)).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&appsv1.Deployment{}).
		Owns(&securityv1.SecurityContextConstraints{}).
		Owns(&corev1.Secret{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&admissionregistrationv1.ValidatingWebhookConfiguration{}).
		Owns(&admissionregistrationv1.MutatingWebhookConfiguration{}).
		Owns(&corev1.Service{}).
		Complete(r)
}

func reconcileComponents(ctx context.Context, client client.Client, components *components.Component) error {
	allErrs := []error{}
	logger := log.FromContext(ctx)
	for _, component := range components.Subcomponents {
		op, err := controllerutil.CreateOrPatch(ctx, client, component.Object, component.MutateFn)
		if err != nil {
			logger.Error(err, "Failed to reconcile component",
				"parentComponent", components.Name,
				"component", component.Name,
				"kind", objectKind(component.Object),
				"name", component.Object.GetName(),
				"namespace", objectNamespace(component.Object),
			)
			allErrs = append(allErrs, err)
			continue
		}
		logComponentOperation(logger, "Reconciled component", components.Name, component.Name, component.Object, string(op))
	}
	if len(allErrs) > 0 {
		return fmt.Errorf("failed to reconcile components: %v", allErrs)
	}
	return nil
}

func (r *OCIClusterAutoscalerReconciler) markExistingClusterControlPlaneInitialized(ctx context.Context, namespace, clusterName string) error {
	var lastNotFound error
	for _, apiVersion := range []string{"cluster.x-k8s.io/v1beta2", "cluster.x-k8s.io/v1beta1"} {
		if err := r.markExistingClusterControlPlaneInitializedForVersion(ctx, namespace, clusterName, apiVersion); err != nil {
			if apierrors.IsNotFound(err) {
				lastNotFound = err
				continue
			}
			return err
		}
		return nil
	}
	return lastNotFound
}

func (r *OCIClusterAutoscalerReconciler) markExistingClusterControlPlaneInitializedForVersion(ctx context.Context, namespace, clusterName, apiVersion string) error {
	logger := log.FromContext(ctx).WithValues("cluster", clusterName, "namespace", namespace)
	cluster := &unstructured.Unstructured{}
	cluster.SetAPIVersion(apiVersion)
	cluster.SetKind("Cluster")
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: clusterName}, cluster); err != nil {
		return err
	}

	initialized, found, err := unstructured.NestedBool(cluster.Object, "status", "initialization", "controlPlaneInitialized")
	if err != nil {
		return fmt.Errorf("failed to read Cluster status.initialization.controlPlaneInitialized: %w", err)
	}
	conditionInitialized, err := clusterStatusConditionIsTrue(cluster, "ControlPlaneInitialized")
	if err != nil {
		return err
	}
	if found && initialized && conditionInitialized {
		return nil
	}

	before := cluster.DeepCopy()
	if !found || !initialized {
		if err := unstructured.SetNestedField(cluster.Object, true, "status", "initialization", "controlPlaneInitialized"); err != nil {
			return fmt.Errorf("failed to set Cluster status.initialization.controlPlaneInitialized: %w", err)
		}
	}
	if !conditionInitialized {
		if err := setClusterStatusCondition(cluster, map[string]interface{}{
			"type":               "ControlPlaneInitialized",
			"status":             string(metav1.ConditionTrue),
			"reason":             "Initialized",
			"message":            "External OpenShift control plane is initialized",
			"observedGeneration": cluster.GetGeneration(),
			"lastTransitionTime": metav1.Now().Time.Format(time.RFC3339),
		}); err != nil {
			return err
		}
	}
	if err := r.Status().Patch(ctx, cluster, client.MergeFrom(before)); err != nil {
		return fmt.Errorf("failed to patch Cluster control plane initialization status: %w", err)
	}
	logger.Info("Marked existing CAPI cluster control plane initialized", "apiVersion", apiVersion)
	return nil
}

func clusterStatusConditionIsTrue(cluster *unstructured.Unstructured, conditionType string) (bool, error) {
	conditions, found, err := unstructured.NestedSlice(cluster.Object, "status", "conditions")
	if err != nil {
		return false, fmt.Errorf("failed to read Cluster status.conditions: %w", err)
	}
	if !found {
		return false, nil
	}
	for _, condition := range conditions {
		conditionMap, ok := condition.(map[string]interface{})
		if !ok {
			continue
		}
		if conditionMap["type"] == conditionType {
			return conditionMap["status"] == string(metav1.ConditionTrue), nil
		}
	}
	return false, nil
}

func setClusterStatusCondition(cluster *unstructured.Unstructured, desired map[string]interface{}) error {
	conditions, found, err := unstructured.NestedSlice(cluster.Object, "status", "conditions")
	if err != nil {
		return fmt.Errorf("failed to read Cluster status.conditions: %w", err)
	}
	if !found {
		conditions = []interface{}{}
	}
	for i, condition := range conditions {
		conditionMap, ok := condition.(map[string]interface{})
		if !ok {
			continue
		}
		if conditionMap["type"] == desired["type"] {
			conditions[i] = desired
			return unstructured.SetNestedSlice(cluster.Object, conditions, "status", "conditions")
		}
	}
	conditions = append(conditions, desired)
	return unstructured.SetNestedSlice(cluster.Object, conditions, "status", "conditions")
}

func reconcileClusterctlComponents(ctx context.Context, kubeClient client.Client, components []unstructured.Unstructured) error {
	logger := log.FromContext(ctx)
	for i := range components {
		desired := components[i].DeepCopy()
		if err := setDeploymentRolloutDefaults(desired); err != nil {
			logger.Error(err, "Failed to set clusterctl deployment rollout defaults",
				"kind", objectKind(desired),
				"name", desired.GetName(),
				"namespace", objectNamespace(desired),
			)
			return err
		}
		current := &unstructured.Unstructured{}
		current.SetGroupVersionKind(desired.GroupVersionKind())

		err := kubeClient.Get(ctx, client.ObjectKeyFromObject(desired), current)
		if apierrors.IsNotFound(err) {
			if err := kubeClient.Create(ctx, desired); err != nil {
				logger.Error(err, "Failed to create clusterctl component",
					"kind", objectKind(desired),
					"name", desired.GetName(),
					"namespace", objectNamespace(desired),
				)
				return err
			}
			logComponentOperation(logger, "Reconciled clusterctl component", "clusterctl", desired.GetName(), desired, string(controllerutil.OperationResultCreated))
			continue
		}
		if err != nil {
			logger.Error(err, "Failed to reconcile clusterctl component",
				"kind", objectKind(desired),
				"name", desired.GetName(),
				"namespace", objectNamespace(desired),
			)
			return err
		}

		reconciled := desired.DeepCopy()
		prepareClusterctlObjectForUpdate(reconciled, current)
		if reflect.DeepEqual(current.Object, reconciled.Object) {
			logComponentOperation(logger, "Reconciled clusterctl component", "clusterctl", desired.GetName(), desired, string(controllerutil.OperationResultNone))
			continue
		}

		if err := kubeClient.Update(ctx, reconciled); err != nil {
			logger.Error(err, "Failed to update clusterctl component",
				"kind", objectKind(reconciled),
				"name", reconciled.GetName(),
				"namespace", objectNamespace(reconciled),
			)
			return err
		}
		logComponentOperation(logger, "Reconciled clusterctl component", "clusterctl", reconciled.GetName(), reconciled, string(controllerutil.OperationResultUpdated))
	}
	return nil
}

func setDeploymentRolloutDefaults(desired *unstructured.Unstructured) error {
	gvk := desired.GroupVersionKind()
	if gvk.Group != "apps" || gvk.Kind != "Deployment" {
		return nil
	}
	if _, found, err := unstructured.NestedMap(desired.Object, "spec", "strategy"); err != nil || found {
		if err != nil {
			return fmt.Errorf("failed to read Deployment strategy: %w", err)
		}
		return nil
	}
	if err := unstructured.SetNestedMap(desired.Object, map[string]interface{}{
		"type": "RollingUpdate",
		"rollingUpdate": map[string]interface{}{
			"maxSurge":       int64(1),
			"maxUnavailable": int64(0),
		},
	}, "spec", "strategy"); err != nil {
		return fmt.Errorf("failed to set Deployment rollout defaults: %w", err)
	}
	return nil
}

func prepareClusterctlObjectForUpdate(desired, current *unstructured.Unstructured) {
	desired.SetResourceVersion(current.GetResourceVersion())
	desired.SetUID(current.GetUID())
	desired.SetCreationTimestamp(current.GetCreationTimestamp())
	desired.SetGeneration(current.GetGeneration())
	desired.SetManagedFields(current.GetManagedFields())
	desired.SetFinalizers(current.GetFinalizers())

	if status, exists, err := unstructured.NestedFieldCopy(current.Object, "status"); err == nil && exists {
		_ = unstructured.SetNestedField(desired.Object, status, "status")
	} else {
		unstructured.RemoveNestedField(desired.Object, "status")
	}

	preserveServiceAllocatedFields(desired, current)
	preserveWebhookInjectedCABundles(desired, current)
}

func preserveServiceAllocatedFields(desired, current *unstructured.Unstructured) {
	gvk := desired.GroupVersionKind()
	if gvk.Group != "" || gvk.Kind != "Service" {
		return
	}

	preserveNestedFieldIfDesiredEmpty(desired, current, "spec", "clusterIP")
	preserveNestedFieldIfDesiredEmpty(desired, current, "spec", "clusterIPs")
	preserveNestedFieldIfDesiredEmpty(desired, current, "spec", "ipFamilies")
	preserveNestedFieldIfDesiredEmpty(desired, current, "spec", "ipFamilyPolicy")
	preserveNestedFieldIfDesiredEmpty(desired, current, "spec", "internalTrafficPolicy")
	preserveNestedFieldIfDesiredEmpty(desired, current, "spec", "healthCheckNodePort")
	preserveServiceNodePorts(desired, current)
}

func preserveNestedFieldIfDesiredEmpty(desired, current *unstructured.Unstructured, fields ...string) {
	currentValue, currentFound, err := unstructured.NestedFieldCopy(current.Object, fields...)
	if err != nil || !currentFound || isEmptyUnstructuredValue(currentValue) {
		return
	}
	desiredValue, desiredFound, err := unstructured.NestedFieldNoCopy(desired.Object, fields...)
	if err != nil || !desiredFound || isEmptyUnstructuredValue(desiredValue) {
		_ = unstructured.SetNestedField(desired.Object, currentValue, fields...)
	}
}

func isEmptyUnstructuredValue(value interface{}) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return typed == ""
	case []interface{}:
		return len(typed) == 0
	case []byte:
		return len(typed) == 0
	case int64:
		return typed == 0
	case int32:
		return typed == 0
	case int:
		return typed == 0
	default:
		return false
	}
}

func preserveServiceNodePorts(desired, current *unstructured.Unstructured) {
	desiredPorts, desiredFound, err := unstructured.NestedSlice(desired.Object, "spec", "ports")
	if err != nil || !desiredFound {
		return
	}
	currentPorts, currentFound, err := unstructured.NestedSlice(current.Object, "spec", "ports")
	if err != nil || !currentFound {
		return
	}

	for i := range desiredPorts {
		desiredPort, ok := desiredPorts[i].(map[string]interface{})
		if !ok {
			continue
		}
		if nodePort, found := desiredPort["nodePort"]; found && !isEmptyUnstructuredValue(nodePort) {
			continue
		}
		if currentPort := matchingServicePort(desiredPort, currentPorts); currentPort != nil {
			if nodePort, found := currentPort["nodePort"]; found && !isEmptyUnstructuredValue(nodePort) {
				desiredPort["nodePort"] = nodePort
				desiredPorts[i] = desiredPort
			}
		}
	}
	_ = unstructured.SetNestedSlice(desired.Object, desiredPorts, "spec", "ports")
}

func matchingServicePort(desiredPort map[string]interface{}, currentPorts []interface{}) map[string]interface{} {
	for _, port := range currentPorts {
		currentPort, ok := port.(map[string]interface{})
		if !ok {
			continue
		}
		if servicePortIdentity(desiredPort, "name") == servicePortIdentity(currentPort, "name") &&
			servicePortIdentity(desiredPort, "port") == servicePortIdentity(currentPort, "port") &&
			servicePortIdentity(desiredPort, "protocol") == servicePortIdentity(currentPort, "protocol") {
			return currentPort
		}
	}
	return nil
}

func servicePortIdentity(port map[string]interface{}, field string) interface{} {
	if value, ok := port[field]; ok {
		return value
	}
	if field == "protocol" {
		return string(corev1.ProtocolTCP)
	}
	return nil
}

func preserveWebhookInjectedCABundles(desired, current *unstructured.Unstructured) {
	gvk := desired.GroupVersionKind()
	if gvk.Group != "admissionregistration.k8s.io" ||
		(gvk.Kind != "ValidatingWebhookConfiguration" && gvk.Kind != "MutatingWebhookConfiguration") {
		return
	}

	desiredWebhooks, desiredFound, err := unstructured.NestedSlice(desired.Object, "webhooks")
	if err != nil || !desiredFound {
		return
	}
	currentWebhooks, currentFound, err := unstructured.NestedSlice(current.Object, "webhooks")
	if err != nil || !currentFound {
		return
	}

	currentCABundles := map[string]interface{}{}
	for _, webhook := range currentWebhooks {
		currentWebhook, ok := webhook.(map[string]interface{})
		if !ok {
			continue
		}
		name, found, err := unstructured.NestedString(currentWebhook, "name")
		if err != nil || !found || name == "" {
			continue
		}
		caBundle, found, err := unstructured.NestedFieldCopy(currentWebhook, "clientConfig", "caBundle")
		if err != nil || !found || isEmptyUnstructuredValue(caBundle) {
			continue
		}
		currentCABundles[name] = caBundle
	}

	for i := range desiredWebhooks {
		desiredWebhook, ok := desiredWebhooks[i].(map[string]interface{})
		if !ok {
			continue
		}
		name, found, err := unstructured.NestedString(desiredWebhook, "name")
		if err != nil || !found || name == "" {
			continue
		}
		desiredCABundle, found, err := unstructured.NestedFieldNoCopy(desiredWebhook, "clientConfig", "caBundle")
		if err == nil && found && !isEmptyUnstructuredValue(desiredCABundle) {
			continue
		}
		if caBundle, found := currentCABundles[name]; found {
			_ = unstructured.SetNestedField(desiredWebhook, caBundle, "clientConfig", "caBundle")
			desiredWebhooks[i] = desiredWebhook
		}
	}
	_ = unstructured.SetNestedSlice(desired.Object, desiredWebhooks, "webhooks")
}

func removeComponent(ctx context.Context, client client.Client, component *components.Component) error {
	logger := log.FromContext(ctx)
	for _, subcomponent := range component.Subcomponents {
		if _, ok := subcomponent.Object.(*corev1.Namespace); ok {
			logger.Info("Skipping namespace deletion during component cleanup",
				"parentComponent", component.Name,
				"component", subcomponent.Name,
				"kind", objectKind(subcomponent.Object),
				"name", subcomponent.Object.GetName(),
			)
			continue
		}
		err := client.Delete(ctx, subcomponent.Object)
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, "Failed to delete component",
				"parentComponent", component.Name,
				"component", subcomponent.Name,
				"kind", objectKind(subcomponent.Object),
				"name", subcomponent.Object.GetName(),
				"namespace", objectNamespace(subcomponent.Object),
			)
			return err
		}
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("Component already absent during cleanup",
				"parentComponent", component.Name,
				"component", subcomponent.Name,
				"kind", objectKind(subcomponent.Object),
				"name", subcomponent.Object.GetName(),
				"namespace", objectNamespace(subcomponent.Object),
			)
			continue
		}
		logger.Info("Deleted component",
			"parentComponent", component.Name,
			"component", subcomponent.Name,
			"kind", objectKind(subcomponent.Object),
			"name", subcomponent.Object.GetName(),
			"namespace", objectNamespace(subcomponent.Object),
		)
	}
	return nil
}

func removeClusterctlComponents(ctx context.Context, client client.Client, components []unstructured.Unstructured) error {
	logger := log.FromContext(ctx)
	for _, component := range components {
		err := client.Delete(ctx, &component)
		if err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, "Failed to delete clusterctl component",
				"kind", objectKind(&component),
				"name", component.GetName(),
				"namespace", objectNamespace(&component),
			)
			return err
		}
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("Clusterctl component already absent during cleanup",
				"kind", objectKind(&component),
				"name", component.GetName(),
				"namespace", objectNamespace(&component),
			)
			continue
		}
		logger.Info("Deleted clusterctl component",
			"kind", objectKind(&component),
			"name", component.GetName(),
			"namespace", objectNamespace(&component),
		)
	}
	return nil
}

// getAutoscalerDeploymentValues first sets the default values and then calls the autoscaler.GetAutoscalerDeploymentValues
// to determine if any values are overridden.
// It returns the deployment values used for the autoscaler Helm chart.
func getAutoscalerDeploymentValues(instance *capiv1alpha1.OCIClusterAutoscaler, namespaces NamespaceConfig) autoscaler.AutoscalerDeploymentValues {
	explicitAutoscalerDiscoveryNamespace := strings.TrimSpace(namespaces.AutoscalerDiscoveryNamespace)
	namespaces = namespaces.WithDefaults()
	autoscalerDiscoveryNamespace := explicitAutoscalerDiscoveryNamespace
	if autoscalerDiscoveryNamespace == "" {
		autoscalerDiscoveryNamespace = managedResourceNamespaceFor(instance, namespaces)
	}
	return autoscaler.GetAutoscalerDeploymentValues(autoscaler.AutoscalerDeploymentValues{
		Name:                   AutoscalerDeploymentName,
		Namespace:              namespaces.AutoscalerNamespace,
		AutoDiscoveryNamespace: autoscalerDiscoveryNamespace,
		CloudProvider:          AutoScalerCloudProvider,
		ServiceAccountName:     AutoscalerDeploymentName,
		RepositoryURL:          AutoscalerRepoURL,
		Chart:                  AutoscalerChartName,
		Version:                "9.40.0",
		CreateRBAC:             true,
		CreateServiceAccount:   true,
	}, instance)
}

func clusterNameFor(ctx context.Context, reader client.Reader, instance *capiv1alpha1.OCIClusterAutoscaler) (string, error) {
	if instance != nil {
		if clusterName := strings.TrimSpace(instance.Spec.CAPI.ClusterName); clusterName != "" {
			return clusterName, nil
		}
	}
	return utils.GetClusterName(ctx, reader)
}

func managedResourceNamespaceFor(instance *capiv1alpha1.OCIClusterAutoscaler, namespaces NamespaceConfig) string {
	namespaces = namespaces.WithDefaults()
	if instance != nil {
		if namespace := strings.TrimSpace(instance.Spec.CAPI.Namespace); namespace != "" {
			return namespace
		}
	}
	return namespaces.ManagedResourceNamespace
}

func subcomponentNames(component *components.Component) []string {
	names := make([]string, 0, len(component.Subcomponents))
	for _, subcomponent := range component.Subcomponents {
		names = append(names, subcomponent.Name)
	}
	return names
}

func logComponentOperation(logger logr.Logger, message, parentComponent, componentName string, obj client.Object, operation string) {
	fields := []any{
		"parentComponent", parentComponent,
		"component", componentName,
		"kind", objectKind(obj),
		"name", obj.GetName(),
		"namespace", objectNamespace(obj),
		"operation", operation,
	}
	if operation == string(controllerutil.OperationResultNone) {
		logger.V(1).Info(message, fields...)
		return
	}
	logger.Info(message, fields...)
}

func objectKind(obj client.Object) string {
	if obj == nil {
		return ""
	}
	if kind := obj.GetObjectKind().GroupVersionKind().Kind; kind != "" {
		return kind
	}
	t := reflect.TypeOf(obj)
	if t == nil {
		return ""
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}

func objectNamespace(obj client.Object) string {
	if obj == nil {
		return ""
	}
	return obj.GetNamespace()
}
