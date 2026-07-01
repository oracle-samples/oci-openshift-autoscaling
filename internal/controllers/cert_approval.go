/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package controllers

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"strings"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	certificatesv1client "k8s.io/client-go/kubernetes/typed/certificates/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	defaultMachineNamespace  = "oci-openshift-autoscaling-operator"
	clusterNameLabelKey      = "cluster.x-k8s.io/cluster-name"
	nodeBootstrapperUsername = "system:serviceaccount:openshift-machine-config-operator:node-bootstrapper"
	systemNodesGroup         = "system:nodes"
)

// CertificateApprovalReconciler reconciles CertificateSigningRequests for OCI machines
type CertificateApprovalReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	CSRClient        certificatesv1client.CertificatesV1Client
	MachineNamespace string
	ClusterName      string
}

type certificateApprovalScope struct {
	namespace   string
	clusterName string
}

// +kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests/approval,verbs=update
// +kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests/status,verbs=update
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=ocimachines,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

// Reconcile handles certificate approval for OCI machines
func (r *CertificateApprovalReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Reconciling certificate approval", "name", req.Name, "namespace", req.Namespace)

	// Fetch the CSR
	csr := &certificatesv1.CertificateSigningRequest{}
	err := r.Get(ctx, req.NamespacedName, csr)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip if already approved or denied
	if isCSRApproved(csr) || isCSRDenied(csr) {
		logger.V(1).Info("Skipping CSR because it is already approved or denied",
			"csr", csr.Name,
			"signerName", csr.Spec.SignerName,
			"username", csr.Spec.Username,
		)
		return ctrl.Result{}, nil
	}

	// Only process kubelet client bootstrap CSRs and kubelet serving CSRs for
	// CAPI-managed OCI machines.
	if !isSupportedKubeletCSR(csr) {
		logger.V(1).Info("Skipping CSR because it is not a supported kubelet CSR",
			"csr", csr.Name,
			"signerName", csr.Spec.SignerName,
			"username", csr.Spec.Username,
		)
		return ctrl.Result{}, nil
	}

	// Extract hostname from CSR
	hostname, err := getCSRHostname(csr)
	if err != nil {
		logger.Error(err, "Failed to extract hostname from CSR", "csr", csr.Name, "signerName", csr.Spec.SignerName)
		return ctrl.Result{}, nil
	}

	if hostname == "" {
		logger.V(1).Info("No hostname found in CSR", "csr", csr.Name, "signerName", csr.Spec.SignerName)
		return ctrl.Result{}, nil
	}

	// Check if there's a matching OCIMachine
	match, machineName, err := r.hasMatchingOCIMachine(ctx, hostname)
	if err != nil {
		logger.Error(err, "Failed to resolve OCIMachine scope for CSR approval", "csr", csr.Name, "hostname", hostname)
		return ctrl.Result{}, err
	}
	if match {
		if csr.Spec.SignerName == certificatesv1.KubeletServingSignerName {
			if err := r.validateServingCSR(ctx, csr, hostname); err != nil {
				logger.Error(err, "Skipping serving CSR approval because node identity or SAN validation failed",
					"csr", csr.Name,
					"hostname", hostname,
					"machine", machineName,
				)
				return ctrl.Result{}, nil
			}
		}

		logger.Info("Approving certificate for OCI machine", "csr", csr.Name, "hostname", hostname, "machine", machineName)

		// Approve the CSR
		now := metav1.NewTime(time.Now())
		csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
			Type:               certificatesv1.CertificateApproved,
			Status:             corev1.ConditionTrue,
			Reason:             "AutoApproval",
			Message:            "Automatically approved by OCI CAPI operator for matching OCIMachine",
			LastUpdateTime:     now,
			LastTransitionTime: now,
		})

		if _, err := r.CSRClient.CertificateSigningRequests().UpdateApproval(ctx, csr.Name, csr, metav1.UpdateOptions{}); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("Approved CSR", "csr", csr.Name, "hostname", hostname, "machine", machineName)
		return ctrl.Result{}, nil
	}
	logger.V(1).Info("Skipping CSR approval because no matching OCIMachine was found",
		"csr", csr.Name,
		"hostname", hostname,
		"signerName", csr.Spec.SignerName,
	)

	return ctrl.Result{}, nil
}

func isKubeletBootstrapClientCSR(csr *certificatesv1.CertificateSigningRequest) bool {
	return csr.Spec.SignerName == certificatesv1.KubeAPIServerClientKubeletSignerName &&
		csr.Spec.Username == nodeBootstrapperUsername
}

func isSupportedKubeletCSR(csr *certificatesv1.CertificateSigningRequest) bool {
	return isKubeletBootstrapClientCSR(csr) || isKubeletServingCSR(csr)
}

func isKubeletServingCSR(csr *certificatesv1.CertificateSigningRequest) bool {
	return csr.Spec.SignerName == certificatesv1.KubeletServingSignerName &&
		strings.HasPrefix(csr.Spec.Username, "system:node:") &&
		containsString(csr.Spec.Groups, systemNodesGroup)
}

func (r *CertificateApprovalReconciler) hasMatchingOCIMachine(ctx context.Context, hostname string) (bool, string, error) {
	logger := log.FromContext(ctx)
	scopes, err := r.certificateApprovalScopes(ctx)
	if err != nil {
		return false, "", err
	}
	if len(scopes) == 0 {
		logger.Info("Refusing to match OCIMachine for CSR approval because cluster scope is empty",
			"hostname", hostname,
		)
		return false, "", nil
	}

	for _, scope := range scopes {
		machineList := &metav1.PartialObjectMetadataList{}
		machineList.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "infrastructure.cluster.x-k8s.io",
			Version: "v1beta2",
			Kind:    "OCIMachine",
		})

		err := r.List(ctx, machineList,
			client.InNamespace(scope.namespace),
			client.MatchingLabels{clusterNameLabelKey: scope.clusterName},
		)
		if err != nil {
			logger.Error(err, "Failed to list OCIMachines", "namespace", scope.namespace, "clusterName", scope.clusterName)
			return false, "", err
		}

		for _, machine := range machineList.Items {
			if machine.Name == hostname {
				logger.V(1).Info("Found matching OCIMachine", "hostname", hostname, "machine", machine.Name, "namespace", scope.namespace, "clusterName", scope.clusterName)
				return true, machine.Name, nil
			}
		}
	}

	logger.V(1).Info("No matching OCIMachine found",
		"hostname", hostname,
		"scopeCount", len(scopes),
	)
	return false, "", nil
}

func (r *CertificateApprovalReconciler) certificateApprovalScopes(ctx context.Context) ([]certificateApprovalScope, error) {
	logger := log.FromContext(ctx)
	baseNamespace := strings.TrimSpace(r.MachineNamespace)
	if baseNamespace == "" {
		baseNamespace = defaultMachineNamespace
	}
	baseClusterName := strings.TrimSpace(r.ClusterName)

	scopes := make([]certificateApprovalScope, 0, 2)
	addScope := func(namespace, clusterName string) {
		namespace = strings.TrimSpace(namespace)
		clusterName = strings.TrimSpace(clusterName)
		if namespace == "" || clusterName == "" {
			return
		}
		for _, scope := range scopes {
			if scope.namespace == namespace && scope.clusterName == clusterName {
				return
			}
		}
		scopes = append(scopes, certificateApprovalScope{namespace: namespace, clusterName: clusterName})
	}

	owner, found, err := resolveSingletonOwner(ctx, r.Client, false)
	if err != nil {
		logger.Error(err, "Failed to list OCIClusterAutoscalers while resolving CSR approval scope")
		return nil, err
	}
	if found {
		clusterName := baseClusterName
		if override := strings.TrimSpace(owner.Spec.CAPI.ClusterName); override != "" {
			clusterName = override
		}
		addScope(baseNamespace, clusterName)
		return scopes, nil
	}

	addScope(baseNamespace, baseClusterName)
	return scopes, nil
}

func getCSRHostname(csr *certificatesv1.CertificateSigningRequest) (string, error) {
	switch csr.Spec.SignerName {
	case certificatesv1.KubeAPIServerClientKubeletSignerName:
		return getHostnameFromClientKubeletCSR(csr)
	case certificatesv1.KubeletServingSignerName:
		return getHostnameFromServingCSR(csr)
	default:
		return "", nil
	}
}

func getHostnameFromClientKubeletCSR(csr *certificatesv1.CertificateSigningRequest) (string, error) {
	if csr.Spec.Username != nodeBootstrapperUsername {
		return "", fmt.Errorf("unexpected kubelet client CSR username %q", csr.Spec.Username)
	}

	certReq, err := parseCSRRequest(csr)
	if err != nil {
		return "", err
	}

	if !containsString(certReq.Subject.Organization, systemNodesGroup) {
		return "", fmt.Errorf("certificate request subject organizations do not include %q", systemNodesGroup)
	}

	if len(certReq.DNSNames) > 0 || len(certReq.IPAddresses) > 0 || len(certReq.EmailAddresses) > 0 || len(certReq.URIs) > 0 {
		return "", fmt.Errorf("kubelet client CSR must not request subject alternative names")
	}

	hostname, err := extractNodeName(certReq.Subject.CommonName)
	if err != nil {
		return "", fmt.Errorf("invalid CommonName %q: %w", certReq.Subject.CommonName, err)
	}
	return hostname, nil
}

func getHostnameFromServingCSR(csr *certificatesv1.CertificateSigningRequest) (string, error) {
	hostnameFromUsername, err := extractNodeName(csr.Spec.Username)
	if err != nil {
		return "", fmt.Errorf("invalid username: %w", err)
	}

	certReq, err := parseCSRRequest(csr)
	if err != nil {
		return "", err
	}

	if !containsString(certReq.Subject.Organization, systemNodesGroup) {
		return "", fmt.Errorf("certificate request subject organizations do not include %q", systemNodesGroup)
	}

	hostnameFromSubject, err := extractNodeName(certReq.Subject.CommonName)
	if err != nil {
		return "", fmt.Errorf("invalid CommonName %q: %w", certReq.Subject.CommonName, err)
	}

	if hostnameFromSubject != hostnameFromUsername {
		return "", fmt.Errorf("subject and username node identities do not match")
	}

	return hostnameFromUsername, nil
}

func (r *CertificateApprovalReconciler) validateServingCSR(ctx context.Context, csr *certificatesv1.CertificateSigningRequest, hostname string) error {
	node := &corev1.Node{}
	if err := r.Get(ctx, client.ObjectKey{Name: hostname}, node); err != nil {
		return fmt.Errorf("failed to get node %q: %w", hostname, err)
	}

	certReq, err := parseCSRRequest(csr)
	if err != nil {
		return err
	}

	if len(certReq.DNSNames) == 0 && len(certReq.IPAddresses) == 0 {
		return fmt.Errorf("serving CSR must request at least one DNS or IP subject alternative name")
	}

	allowedDNSNames := allowedNodeDNSNames(node)
	for _, dnsName := range certReq.DNSNames {
		if !allowedDNSNames[dnsName] {
			return fmt.Errorf("serving CSR requested DNS SAN %q that is not a node DNS/name address", dnsName)
		}
	}

	allowedIPs := allowedNodeIPs(node)
	for _, ipAddress := range certReq.IPAddresses {
		if !allowedIPs[ipAddress.String()] {
			return fmt.Errorf("serving CSR requested IP SAN %q that is not a node IP address", ipAddress.String())
		}
	}

	return nil
}

func allowedNodeDNSNames(node *corev1.Node) map[string]bool {
	allowed := map[string]bool{
		node.Name: true,
	}
	for _, address := range node.Status.Addresses {
		switch address.Type {
		case corev1.NodeHostName, corev1.NodeInternalDNS, corev1.NodeExternalDNS:
			allowed[address.Address] = true
		}
	}
	return allowed
}

func allowedNodeIPs(node *corev1.Node) map[string]bool {
	allowed := map[string]bool{}
	for _, address := range node.Status.Addresses {
		switch address.Type {
		case corev1.NodeInternalIP, corev1.NodeExternalIP:
			if ip := net.ParseIP(address.Address); ip != nil {
				allowed[ip.String()] = true
			}
		}
	}
	return allowed
}

func parseCSRRequest(csr *certificatesv1.CertificateSigningRequest) (*x509.CertificateRequest, error) {
	if len(csr.Spec.Request) == 0 {
		return nil, fmt.Errorf("CSR request is empty")
	}

	block, _ := pem.Decode(csr.Spec.Request)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, fmt.Errorf("failed to decode PEM block or invalid block type")
	}

	certReq, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate request: %w", err)
	}

	if certReq.Subject.CommonName == "" {
		return nil, fmt.Errorf("certificate request has empty CommonName")
	}

	return certReq, nil
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func extractNodeName(identity string) (string, error) {
	if identity == "" {
		return "", fmt.Errorf("node identity is empty")
	}
	if !strings.HasPrefix(identity, "system:node:") {
		return "", fmt.Errorf("node identity %q does not have expected system:node: prefix", identity)
	}

	hostname := strings.TrimPrefix(identity, "system:node:")
	if dotIndex := strings.Index(hostname, "."); dotIndex > 0 {
		hostname = hostname[:dotIndex]
	}
	if hostname == "" {
		return "", fmt.Errorf("extracted hostname is empty")
	}
	return hostname, nil
}

func isCSRApproved(csr *certificatesv1.CertificateSigningRequest) bool {
	for _, condition := range csr.Status.Conditions {
		if condition.Type == certificatesv1.CertificateApproved {
			return true
		}
	}
	return false
}

func isCSRDenied(csr *certificatesv1.CertificateSigningRequest) bool {
	for _, condition := range csr.Status.Conditions {
		if condition.Type == certificatesv1.CertificateDenied {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *CertificateApprovalReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&certificatesv1.CertificateSigningRequest{}).
		Complete(r)
}
