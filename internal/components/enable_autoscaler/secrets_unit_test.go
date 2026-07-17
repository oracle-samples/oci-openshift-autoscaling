/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package enableautoscaler

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	ocicapioperatorv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type tokenSubresourceClient struct {
	parent *tokenRequestClient
}

func (s *tokenSubresourceClient) Get(context.Context, client.Object, client.Object, ...client.SubResourceGetOption) error {
	return nil
}

func (s *tokenSubresourceClient) Create(_ context.Context, obj client.Object, subResource client.Object, _ ...client.SubResourceCreateOption) error {
	sa := obj.(*corev1.ServiceAccount)
	tokenRequest := subResource.(*authenticationv1.TokenRequest)
	s.parent.tokenServiceAccount = client.ObjectKeyFromObject(sa)
	s.parent.tokenAudiences = append([]string{}, tokenRequest.Spec.Audiences...)
	if tokenRequest.Spec.ExpirationSeconds != nil {
		expirationSeconds := *tokenRequest.Spec.ExpirationSeconds
		s.parent.tokenExpirationSeconds = &expirationSeconds
	}
	tokenRequest.Status.Token = s.parent.tokenValue
	return nil
}

func (s *tokenSubresourceClient) Update(context.Context, client.Object, ...client.SubResourceUpdateOption) error {
	return nil
}

func (s *tokenSubresourceClient) Patch(context.Context, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
	return nil
}

type tokenRequestClient struct {
	MockClient
	tokenValue             string
	tokenServiceAccount    client.ObjectKey
	tokenAudiences         []string
	tokenExpirationSeconds *int64
}

func (m *tokenRequestClient) Get(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	switch typed := obj.(type) {
	case *corev1.ConfigMap:
		typed.ObjectMeta = metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}
		typed.Data = map[string]string{"ca.crt": "test-ca"}
	}
	return nil
}

func (m *tokenRequestClient) SubResource(subResource string) client.SubResourceClient {
	return &tokenSubresourceClient{parent: m}
}

func (m *tokenRequestClient) GroupVersionKindFor(runtime.Object) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, nil
}

func TestKubeConfigSecretUsesTokenRequest(t *testing.T) {
	ctx := context.Background()
	instance := &ocicapioperatorv1alpha1.OCIClusterAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "test-autoscaler"},
	}
	mockClient := &tokenRequestClient{tokenValue: "short-lived-token"}

	obj, mutateFn := KubeConfigSecret(ctx, mockClient, "managed-resource-ns", "managed-resource-ns", "capi-provider-ns", "test-cluster", "capi-manager", instance)
	if err := mutateFn(); err != nil {
		t.Fatalf("mutateFn() error = %v", err)
	}

	if mockClient.tokenServiceAccount != (client.ObjectKey{Name: "capi-manager", Namespace: "capi-provider-ns"}) {
		t.Fatalf("unexpected token service account: %#v", mockClient.tokenServiceAccount)
	}
	if len(mockClient.tokenAudiences) != 1 || mockClient.tokenAudiences[0] != kubeAPIServerAudience {
		t.Fatalf("unexpected token audiences: %#v", mockClient.tokenAudiences)
	}
	if mockClient.tokenExpirationSeconds == nil || *mockClient.tokenExpirationSeconds != serviceAccountTokenTTLSec {
		t.Fatalf("unexpected token expiration: %#v", mockClient.tokenExpirationSeconds)
	}

	secret, ok := obj.(*corev1.Secret)
	if !ok {
		t.Fatalf("expected Secret, got %T", obj)
	}
	if secret.Namespace != "managed-resource-ns" {
		t.Fatalf("secret namespace = %q, want managed-resource-ns", secret.Namespace)
	}
	rendered := string(secret.Data["value"])
	if !strings.Contains(rendered, "token: short-lived-token") {
		t.Fatalf("rendered kubeconfig did not contain requested token: %q", rendered)
	}
	if !strings.Contains(rendered, "namespace: managed-resource-ns") {
		t.Fatalf("rendered kubeconfig did not contain expected context namespace: %q", rendered)
	}
	expectedCA := base64.StdEncoding.EncodeToString([]byte("test-ca"))
	if !strings.Contains(rendered, "certificate-authority-data: "+expectedCA) {
		t.Fatalf("rendered kubeconfig did not contain expected CA: %q", rendered)
	}
	if got := secret.Labels["cluster.x-k8s.io/cluster-name"]; got != "test-cluster" {
		t.Fatalf("unexpected cluster-name label: %q", got)
	}
	if _, ok := secret.Labels["clusterctl.cluster.x-k8s.io/move"]; !ok {
		t.Fatalf("expected clusterctl move label to be set")
	}
}
