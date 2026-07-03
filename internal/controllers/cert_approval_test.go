/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package controllers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"net"
	"testing"

	infrastructurev1beta2 "github.com/oracle/cluster-api-provider-oci/api/v1beta2"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestHasMatchingOCIMachine_DoesNotAllowSubstringMatch(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrastructurev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	machine := &infrastructurev1beta2.OCIMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster-worker-1",
			Namespace: "oci-openshift-autoscaling-operator",
			Labels: map[string]string{
				clusterNameLabelKey: "test-cluster",
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(machine).Build()
	r := &CertificateApprovalReconciler{
		Client:           c,
		MachineNamespace: "oci-openshift-autoscaling-operator",
		ClusterName:      "test-cluster",
	}

	matched, _ := r.hasMatchingOCIMachine(context.Background(), "worker-1")
	if matched {
		t.Fatalf("expected substring hostname not to match OCIMachine name")
	}
}

func TestHasMatchingOCIMachine_ExactMatchWithinNamespaceAndCluster(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrastructurev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	machine := &infrastructurev1beta2.OCIMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-1",
			Namespace: "oci-openshift-autoscaling-operator",
			Labels: map[string]string{
				clusterNameLabelKey: "test-cluster",
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(machine).Build()
	r := &CertificateApprovalReconciler{
		Client:           c,
		MachineNamespace: "oci-openshift-autoscaling-operator",
		ClusterName:      "test-cluster",
	}

	matched, machineName := r.hasMatchingOCIMachine(context.Background(), "worker-1")
	if !matched {
		t.Fatalf("expected exact machine match within scope")
	}
	if machineName != "worker-1" {
		t.Fatalf("expected machine name worker-1, got %s", machineName)
	}
}

func TestHasMatchingOCIMachine_RejectsEmptyClusterScope(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrastructurev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	machine := &infrastructurev1beta2.OCIMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-1",
			Namespace: "oci-openshift-autoscaling-operator",
			Labels: map[string]string{
				clusterNameLabelKey: "test-cluster",
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(machine).Build()
	r := &CertificateApprovalReconciler{
		Client:           c,
		MachineNamespace: "oci-openshift-autoscaling-operator",
	}

	matched, _ := r.hasMatchingOCIMachine(context.Background(), "worker-1")
	if matched {
		t.Fatalf("expected no match when cluster scope is unset")
	}
}

func TestHasMatchingOCIMachine_RejectsWrongClusterScope(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrastructurev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	machine := &infrastructurev1beta2.OCIMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-1",
			Namespace: "oci-openshift-autoscaling-operator",
			Labels: map[string]string{
				clusterNameLabelKey: "other-cluster",
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(machine).Build()
	r := &CertificateApprovalReconciler{
		Client:           c,
		MachineNamespace: "oci-openshift-autoscaling-operator",
		ClusterName:      "test-cluster",
	}

	matched, _ := r.hasMatchingOCIMachine(context.Background(), "worker-1")
	if matched {
		t.Fatalf("expected no match when OCIMachine cluster label is outside configured scope")
	}
}

func TestGetCSRHostname_ClientKubeletRejectsNonBootstrapperUsername(t *testing.T) {
	t.Parallel()

	request, err := makeCSRRequestPEM("system:node:worker-1", []string{systemNodesGroup})
	if err != nil {
		t.Fatalf("make csr: %v", err)
	}

	csr := &certificatesv1.CertificateSigningRequest{
		Spec: certificatesv1.CertificateSigningRequestSpec{
			SignerName: "kubernetes.io/kube-apiserver-client-kubelet",
			Username:   "system:node:worker-2",
			Request:    request,
		},
	}

	_, err = getCSRHostname(csr)
	if err == nil {
		t.Fatalf("expected error when CSR CN node identity does not match CSR username")
	}
}

func TestGetCSRHostname_ClientKubeletAcceptsBootstrapperNodeIdentity(t *testing.T) {
	t.Parallel()

	request, err := makeCSRRequestPEM("system:node:worker-1", []string{systemNodesGroup})
	if err != nil {
		t.Fatalf("make csr: %v", err)
	}

	csr := &certificatesv1.CertificateSigningRequest{
		Spec: certificatesv1.CertificateSigningRequestSpec{
			SignerName: certificatesv1.KubeAPIServerClientKubeletSignerName,
			Username:   nodeBootstrapperUsername,
			Request:    request,
		},
	}

	hostname, err := getCSRHostname(csr)
	if err != nil {
		t.Fatalf("expected matching identity to pass, got error: %v", err)
	}
	if hostname != "worker-1" {
		t.Fatalf("expected hostname worker-1, got %s", hostname)
	}
}

func TestGetCSRHostname_ClientKubeletRejectsMissingSystemNodesOrg(t *testing.T) {
	t.Parallel()

	request, err := makeCSRRequestPEM("system:node:worker-1", nil)
	if err != nil {
		t.Fatalf("make csr: %v", err)
	}

	csr := &certificatesv1.CertificateSigningRequest{
		Spec: certificatesv1.CertificateSigningRequestSpec{
			SignerName: certificatesv1.KubeAPIServerClientKubeletSignerName,
			Username:   nodeBootstrapperUsername,
			Request:    request,
		},
	}

	_, err = getCSRHostname(csr)
	if err == nil {
		t.Fatalf("expected error when CSR subject organizations do not include system:nodes")
	}
}

func TestIsKubeletBootstrapClientCSRRejectsServingCSR(t *testing.T) {
	t.Parallel()

	csr := &certificatesv1.CertificateSigningRequest{
		Spec: certificatesv1.CertificateSigningRequestSpec{
			SignerName: certificatesv1.KubeletServingSignerName,
			Username:   nodeBootstrapperUsername,
		},
	}

	if isKubeletBootstrapClientCSR(csr) {
		t.Fatalf("expected serving CSR not to be treated as kubelet bootstrap client CSR")
	}
}

func TestGetCSRHostname_ServingAcceptsMatchingNodeIdentity(t *testing.T) {
	t.Parallel()

	request, err := makeCSRRequestPEM("system:node:worker-1", []string{systemNodesGroup})
	if err != nil {
		t.Fatalf("make csr: %v", err)
	}

	csr := &certificatesv1.CertificateSigningRequest{
		Spec: certificatesv1.CertificateSigningRequestSpec{
			SignerName: certificatesv1.KubeletServingSignerName,
			Username:   "system:node:worker-1",
			Groups:     []string{systemNodesGroup, "system:authenticated"},
			Request:    request,
		},
	}

	hostname, err := getCSRHostname(csr)
	if err != nil {
		t.Fatalf("expected matching serving identity to pass, got error: %v", err)
	}
	if hostname != "worker-1" {
		t.Fatalf("expected hostname worker-1, got %s", hostname)
	}
}

func TestGetCSRHostname_ServingRejectsUsernameMismatch(t *testing.T) {
	t.Parallel()

	request, err := makeCSRRequestPEM("system:node:worker-1", []string{systemNodesGroup})
	if err != nil {
		t.Fatalf("make csr: %v", err)
	}

	csr := &certificatesv1.CertificateSigningRequest{
		Spec: certificatesv1.CertificateSigningRequestSpec{
			SignerName: certificatesv1.KubeletServingSignerName,
			Username:   "system:node:worker-2",
			Groups:     []string{systemNodesGroup, "system:authenticated"},
			Request:    request,
		},
	}

	_, err = getCSRHostname(csr)
	if err == nil {
		t.Fatalf("expected error when serving CSR subject and username do not match")
	}
}

func TestValidateServingCSRMatchesNodeAddresses(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeHostName, Address: "worker-1"},
				{Type: corev1.NodeInternalIP, Address: "10.0.0.10"},
			},
		},
	}

	request, err := makeServingCSRRequestPEM(
		"system:node:worker-1",
		[]string{systemNodesGroup},
		[]string{"worker-1"},
		[]net.IP{net.ParseIP("10.0.0.10")},
	)
	if err != nil {
		t.Fatalf("make csr: %v", err)
	}

	csr := &certificatesv1.CertificateSigningRequest{
		Spec: certificatesv1.CertificateSigningRequestSpec{
			SignerName: certificatesv1.KubeletServingSignerName,
			Username:   "system:node:worker-1",
			Groups:     []string{systemNodesGroup, "system:authenticated"},
			Request:    request,
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
	r := &CertificateApprovalReconciler{Client: c}

	if err := r.validateServingCSR(context.Background(), csr, "worker-1"); err != nil {
		t.Fatalf("expected serving CSR SANs to match node addresses, got error: %v", err)
	}
}

func TestValidateServingCSRRejectsUnknownIP(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeHostName, Address: "worker-1"},
				{Type: corev1.NodeInternalIP, Address: "10.0.0.10"},
			},
		},
	}

	request, err := makeServingCSRRequestPEM(
		"system:node:worker-1",
		[]string{systemNodesGroup},
		[]string{"worker-1"},
		[]net.IP{net.ParseIP("10.0.0.99")},
	)
	if err != nil {
		t.Fatalf("make csr: %v", err)
	}

	csr := &certificatesv1.CertificateSigningRequest{
		Spec: certificatesv1.CertificateSigningRequestSpec{
			SignerName: certificatesv1.KubeletServingSignerName,
			Username:   "system:node:worker-1",
			Groups:     []string{systemNodesGroup, "system:authenticated"},
			Request:    request,
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
	r := &CertificateApprovalReconciler{Client: c}

	if err := r.validateServingCSR(context.Background(), csr, "worker-1"); err == nil {
		t.Fatalf("expected serving CSR with unknown IP SAN to fail validation")
	}
}

func makeCSRRequestPEM(commonName string, organizations []string) ([]byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: organizations,
		},
	}, key)
	if err != nil {
		return nil, err
	}

	block := &pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}
	return pem.EncodeToMemory(block), nil
}

func makeServingCSRRequestPEM(commonName string, organizations []string, dnsNames []string, ipAddresses []net.IP) ([]byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: organizations,
		},
		DNSNames:    dnsNames,
		IPAddresses: ipAddresses,
	}, key)
	if err != nil {
		return nil, err
	}

	block := &pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}
	return pem.EncodeToMemory(block), nil
}
