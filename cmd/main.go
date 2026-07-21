/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	certificatesv1client "k8s.io/client-go/kubernetes/typed/certificates/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	configv1 "github.com/openshift/api/config/v1"
	securityv1 "github.com/openshift/api/security/v1"

	"github.com/go-logr/logr"
	"github.com/kelseyhightower/envconfig"
	ocicapioperatorv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	infrastructurev1beta2 "github.com/oracle/cluster-api-provider-oci/api/v1beta2"
	"github.com/spf13/cobra"
	"go.uber.org/zap/zapcore"
	capiv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"

	"github.com/openshift/oci-capi-operator/internal/components/capoci"
	enableautoscaler "github.com/openshift/oci-capi-operator/internal/components/enable_autoscaler"
	"github.com/openshift/oci-capi-operator/internal/controllers"
	"github.com/openshift/oci-capi-operator/internal/utils"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(rbacv1.AddToScheme(scheme))
	utilruntime.Must(admissionregistrationv1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(configv1.AddToScheme(scheme))

	utilruntime.Must(ocicapioperatorv1alpha1.AddToScheme(scheme))
	utilruntime.Must(infrastructurev1beta2.AddToScheme(scheme))
	utilruntime.Must(capiv1beta1.AddToScheme(scheme))
	utilruntime.Must(securityv1.Install(scheme))

	// +kubebuilder:scaffold:scheme
}

func main() {
	ctrl.SetLogger(zap.New(zap.JSONEncoder(func(o *zapcore.EncoderConfig) {
		o.EncodeTime = zapcore.RFC3339TimeEncoder
	})))
	cmd := &cobra.Command{
		Use: "oci-capi-operator",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
			os.Exit(1)
		},
	}
	cmd.AddCommand(NewInitCommand())
	cmd.AddCommand(NewRunCommand())

	if err := cmd.Execute(); err != nil {
		setupLog.Error(err, "problem running operator")
		os.Exit(1)
	}
}

type Options struct {
	CAPOCICredentials capoci.CAPOCICredentials
	ProviderConfig    controllers.ProviderConfig
	AutoScalingConfig enableautoscaler.Config
	CAPOCIProvider    controllers.ProviderConfig
	NamespaceConfig   controllers.NamespaceConfig
	CSRApprovalConfig CSRApprovalConfig
	RunOptions        RunOptions
}

type CSRApprovalConfig struct {
	MachineNamespace string `envconfig:"CSR_MACHINE_NAMESPACE" default:""`
	ClusterName      string `envconfig:"CSR_CLUSTER_NAME" default:""`
}

const (
	defaultCSRMachineNamespace         = controllers.DefaultManagedResourceNamespace
	defaultLeaderElectionLeaseDuration = 15 * time.Second
	defaultLeaderElectionRenewDeadline = 10 * time.Second
	defaultLeaderElectionRetryPeriod   = 2 * time.Second
)

type RunOptions struct {
	EnableLeaderElection bool
	MetricsAddr          string
	ProbeAddr            string
	EnableHTTP2          bool
}

func applyLeaderElectionTiming(options *ctrl.Options) {
	leaseDuration := defaultLeaderElectionLeaseDuration
	renewDeadline := defaultLeaderElectionRenewDeadline
	retryPeriod := defaultLeaderElectionRetryPeriod
	options.LeaseDuration = &leaseDuration
	options.RenewDeadline = &renewDeadline
	options.RetryPeriod = &retryPeriod
	options.LeaderElectionReleaseOnCancel = true
}

func run(ctx context.Context, options Options, setupLog *logr.Logger) error {

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !options.RunOptions.EnableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	mgrOpts := ctrl.Options{
		Scheme:                 scheme,
		WebhookServer:          webhookServer,
		Metrics:                metricsserver.Options{BindAddress: options.RunOptions.MetricsAddr},
		HealthProbeBindAddress: options.RunOptions.ProbeAddr,
		LeaderElection:         options.RunOptions.EnableLeaderElection,
		LeaderElectionID:       "1af242a3.openshift.io",
	}
	if mgrOpts.LeaderElection {
		applyLeaderElectionTiming(&mgrOpts)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOpts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	if err = envconfig.Process("", &options); err != nil {
		setupLog.Error(err, "failed to process environment variables")
		os.Exit(1)
	}
	options.NamespaceConfig = options.NamespaceConfig.WithDefaults()
	if err := resolveCSRApprovalConfig(ctx, mgr.GetAPIReader(), &options.CSRApprovalConfig, options.NamespaceConfig.ManagedResourceNamespace); err != nil {
		setupLog.Error(err, "invalid CSR approval configuration")
		return err
	}
	if err := validateOptions(options); err != nil {
		setupLog.Error(err, "invalid operator startup configuration")
		return err
	}
	if err := utils.ValidateProviderVersion(options.ProviderConfig.CAPIVersion); err != nil {
		setupLog.Error(err, "Invalid CAPI_VERSION")
		return err
	}
	if err := utils.ValidateProviderVersion(options.CAPOCIProvider.Version); err != nil {
		setupLog.Error(err, "Invalid CAPOCI_VERSION")
		return err
	}
	setupLog.Info("Resolved operator startup options",
		"leaderElection", options.RunOptions.EnableLeaderElection,
		"healthProbeBindAddress", options.RunOptions.ProbeAddr,
		"http2Enabled", options.RunOptions.EnableHTTP2,
		"capiProviderVersion", options.ProviderConfig.CAPIVersion,
		"capociProviderVersion", options.CAPOCIProvider.Version,
		"capociAuthMode", capociAuthMode(options.CAPOCICredentials),
		"ociRegion", options.CAPOCICredentials.Region,
		"operatorNamespace", options.NamespaceConfig.OperatorNamespace,
		"capiProviderNamespace", options.NamespaceConfig.CAPIProviderNamespace,
		"capociProviderNamespace", options.NamespaceConfig.CAPOCIProviderNamespace,
		"managedResourceNamespace", options.NamespaceConfig.ManagedResourceNamespace,
		"autoscalerNamespace", options.NamespaceConfig.AutoscalerNamespace,
		"autoscalerDiscoveryNamespace", options.NamespaceConfig.AutoscalerDiscoveryNamespace,
		"csrMachineNamespace", options.CSRApprovalConfig.MachineNamespace,
		"csrClusterName", options.CSRApprovalConfig.ClusterName,
	)
	if err = (&controllers.OCIClusterAutoscalerReconciler{
		RestConfig:        mgr.GetConfig(),
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		CAPOCICredentials: options.CAPOCICredentials,
		ProviderConfig:    options.ProviderConfig,
		CAPOCIProvider:    options.CAPOCIProvider,
		AutoScalingConfig: options.AutoScalingConfig,
		NamespaceConfig:   options.NamespaceConfig,
		EventRecorder:     mgr.GetEventRecorderFor("ociclusterautoscaler-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "OCIClusterAutoscaler")
		return err
	}
	setupLog.Info("Registered controller", "controller", "OCIClusterAutoscaler")

	csrClient, err := certificatesv1client.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "Failed to create CSR client")
		return err
	}

	if err = (&controllers.CertificateApprovalReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		CSRClient:        *csrClient,
		MachineNamespace: options.CSRApprovalConfig.MachineNamespace,
		ClusterName:      options.CSRApprovalConfig.ClusterName,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CertificateApproval")
		return err
	}
	setupLog.Info("Registered controller", "controller", "CertificateApproval")
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return err
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}
	return nil
}

func validateOptions(options Options) error {
	if err := options.CAPOCICredentials.Validate(); err != nil {
		return err
	}
	if err := options.NamespaceConfig.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(options.CSRApprovalConfig.ClusterName) == "" {
		return fmt.Errorf("CSR approval requires CSR_CLUSTER_NAME or CLUSTER_NAME")
	}
	return nil
}

func resolveCSRApprovalConfig(ctx context.Context, c client.Reader, config *CSRApprovalConfig, defaultMachineNamespace string) error {
	config.MachineNamespace = strings.TrimSpace(config.MachineNamespace)
	if config.MachineNamespace == "" {
		config.MachineNamespace = strings.TrimSpace(defaultMachineNamespace)
	}
	if config.MachineNamespace == "" {
		config.MachineNamespace = defaultCSRMachineNamespace
	}

	clusterName := strings.TrimSpace(config.ClusterName)
	if clusterName == "" {
		clusterName = strings.TrimSpace(os.Getenv("CLUSTER_NAME"))
	}
	if clusterName == "" {
		discoveredClusterName, err := utils.GetClusterName(ctx, c)
		if err != nil {
			return fmt.Errorf("CSR approval cluster name was not configured and cluster discovery failed: %w", err)
		}
		clusterName = strings.TrimSpace(discoveredClusterName)
	}
	if clusterName == "" {
		return fmt.Errorf("CSR approval cluster name must not be empty; set CSR_CLUSTER_NAME or CLUSTER_NAME")
	}
	config.ClusterName = clusterName
	return nil
}

func capociAuthMode(credentials capoci.CAPOCICredentials) string {
	if credentials.UsesInstancePrincipal() {
		return "instancePrincipal"
	}
	return "userPrincipal"
}

func NewRunCommand() *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Runs the OCI CAPI operator",
	}

	options := Options{}
	runCmd.Flags().StringVar(&options.RunOptions.MetricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to.")
	runCmd.Flags().StringVar(&options.RunOptions.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	runCmd.Flags().BoolVar(&options.RunOptions.EnableLeaderElection, "leader-elect", true,
		"Enable leader election for controller manager. "+
			"Disable only for local development with --leader-elect=false.")
	runCmd.Flags().BoolVar(&options.RunOptions.EnableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the webhook servers")

	runCmd.Run = func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		setupLog = ctrl.Log.WithName("setup")
		if err := run(ctx, options, &setupLog); err != nil {
			setupLog.Error(err, "problem running operator")
			os.Exit(1)
		}
	}
	return runCmd
}
