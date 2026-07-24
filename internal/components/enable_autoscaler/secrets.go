/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package enableautoscaler

import (
	"context"
	"encoding/base64"
	"fmt"

	ocicapioperatorv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	"github.com/openshift/oci-capi-operator/internal/utils"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	kubeAPIServerAudience     = "https://kubernetes.default.svc"
	serviceAccountTokenTTLSec = int64(24 * 60 * 60)
)

// BootstrapConfigSecret creates a secret that contains the bootstrap config for additional workernodes that are added to the cluster.
func BootstrapConfigSecret(ctx context.Context, client client.Client, secretNamespace string, clusterName string, instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler) (client.Object, func() error) {
	bootstrapConfigSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-bootstrap", clusterName),
			Namespace: secretNamespace,
		},
	}

	mutateFn := func() error {
		logger := ctrllog.Log.WithName("enableautoscaler").WithValues(
			"component", "BootstrapConfigSecret",
			"cluster", clusterName,
			"secret", bootstrapConfigSecret.Name,
			"namespace", secretNamespace,
		)
		utils.SetDefaultLabels(bootstrapConfigSecret, instance.Name)
		logger.Info("Generating bootstrap ignition secret")
		ignitionConfig, err := utils.GenerateIgnitionConfig(ctx, client)
		if err != nil {
			return fmt.Errorf("failed to generate ignition config: %w", err)
		}

		bootstrapConfigSecret.Data = map[string][]byte{
			"value":  []byte(ignitionConfig),
			"format": []byte("ignition"),
		}
		logger.Info("Bootstrap ignition secret ready", "format", "ignition", "bytes", len(ignitionConfig))
		return nil
	}

	return bootstrapConfigSecret, mutateFn
}

var kubeconfigFmt = `apiVersion: v1
kind: Config
clusters:
- name: %s
  cluster:
    server: https://kubernetes.default.svc
    certificate-authority-data: %s
contexts:
- name: %s
  context:
    cluster: %s
    user: %s
    namespace: %s
current-context: %s
users:
- name: %s
  user:
    token: %s
`

// KubeConfigSecret creates a secret for CAPI so it can access this cluster.
func KubeConfigSecret(ctx context.Context, client client.Client, secretNamespace string, contextNamespace string, tokenServiceAccountNamespace string, clusterName string, capiServiceAccountName string, instance *ocicapioperatorv1alpha1.OCIClusterAutoscaler) (client.Object, func() error) {
	kubeConfigSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-kubeconfig", clusterName),
			Namespace: secretNamespace,
		},
	}

	mutateFn := func() error {
		logger := ctrllog.Log.WithName("enableautoscaler").WithValues(
			"component", "KubeConfigSecret",
			"cluster", clusterName,
			"secret", kubeConfigSecret.Name,
			"namespace", secretNamespace,
			"contextNamespace", contextNamespace,
			"serviceAccount", capiServiceAccountName,
			"serviceAccountNamespace", tokenServiceAccountNamespace,
		)
		serviceAccount := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      capiServiceAccountName,
				Namespace: tokenServiceAccountNamespace,
			},
		}
		tokenRequest := &authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				Audiences:         []string{kubeAPIServerAudience},
				ExpirationSeconds: func() *int64 { v := serviceAccountTokenTTLSec; return &v }(),
			},
		}
		logger.Info("Requesting service account token for kubeconfig secret",
			"audience", kubeAPIServerAudience,
			"expirationSeconds", serviceAccountTokenTTLSec,
		)
		if err := client.SubResource("token").Create(ctx, serviceAccount, tokenRequest); err != nil {
			return fmt.Errorf("failed to create service account token request: %w", err)
		}
		if tokenRequest.Status.Token == "" {
			return fmt.Errorf("service account token request returned an empty token")
		}

		caCrt, err := utils.GetKubeconfigCA(ctx, client)
		if err != nil {
			return fmt.Errorf("failed to get kubeconfig CA: %w", err)
		}

		kubeconfig := fmt.Sprintf(
			kubeconfigFmt,
			clusterName,
			base64.StdEncoding.EncodeToString([]byte(caCrt)),
			clusterName,
			clusterName,
			capiServiceAccountName,
			contextNamespace,
			clusterName,
			capiServiceAccountName,
			tokenRequest.Status.Token,
		)

		utils.SetDefaultLabels(kubeConfigSecret, instance.Name)
		kubeConfigSecret.Data = map[string][]byte{
			"value": []byte(kubeconfig),
		}
		labels := kubeConfigSecret.GetLabels()
		labels["cluster.x-k8s.io/cluster-name"] = clusterName
		labels["clusterctl.cluster.x-k8s.io/move"] = ""
		kubeConfigSecret.SetLabels(labels)
		logger.Info("Kubeconfig secret ready", "bytes", len(kubeconfig))
		return nil
	}

	return kubeConfigSecret, mutateFn
}
