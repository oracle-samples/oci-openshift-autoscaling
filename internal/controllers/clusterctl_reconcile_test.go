/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package controllers

import (
	"context"
	"reflect"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcileClusterctlComponentsCorrectsFullObjectDrift(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		desired unstructured.Unstructured
		current unstructured.Unstructured
		assert  func(t *testing.T, got *unstructured.Unstructured)
	}{
		{
			name:    "deployment args selector and volumes",
			desired: desiredDeployment(),
			current: driftedDeployment(),
			assert: func(t *testing.T, got *unstructured.Unstructured) {
				selector, found, err := unstructured.NestedStringMap(got.Object, "spec", "selector", "matchLabels")
				if err != nil || !found {
					t.Fatalf("deployment selector not found: found=%v err=%v", found, err)
				}
				if selector["cluster.x-k8s.io/provider"] != "cluster-api" {
					t.Fatalf("selector drift was not corrected: %#v", selector)
				}

				containers, found, err := unstructured.NestedSlice(got.Object, "spec", "template", "spec", "containers")
				if err != nil || !found || len(containers) != 1 {
					t.Fatalf("deployment containers not found: found=%v len=%d err=%v", found, len(containers), err)
				}
				args, found, err := unstructured.NestedStringSlice(containers[0].(map[string]interface{}), "args")
				if err != nil || !found {
					t.Fatalf("deployment args not found: found=%v err=%v", found, err)
				}
				if !reflect.DeepEqual(args, []string{"--leader-elect", "--diagnostics-address=:8443"}) {
					t.Fatalf("deployment args drift was not corrected: %#v", args)
				}

				volumes, found, err := unstructured.NestedSlice(got.Object, "spec", "template", "spec", "volumes")
				if err != nil || !found || len(volumes) != 1 {
					t.Fatalf("deployment volumes drift was not corrected: found=%v len=%d err=%v", found, len(volumes), err)
				}
			},
		},
		{
			name:    "clusterrole rules",
			desired: desiredClusterRole(),
			current: driftedClusterRole(),
			assert: func(t *testing.T, got *unstructured.Unstructured) {
				rules, found, err := unstructured.NestedSlice(got.Object, "rules")
				if err != nil || !found {
					t.Fatalf("clusterrole rules not found: found=%v err=%v", found, err)
				}
				if !reflect.DeepEqual(rules, desiredClusterRole().Object["rules"]) {
					t.Fatalf("clusterrole rules drift was not corrected: %#v", rules)
				}
			},
		},
		{
			name:    "validating webhook client config",
			desired: desiredValidatingWebhookConfiguration(),
			current: driftedValidatingWebhookConfiguration(),
			assert: func(t *testing.T, got *unstructured.Unstructured) {
				webhooks, found, err := unstructured.NestedSlice(got.Object, "webhooks")
				if err != nil || !found || len(webhooks) != 1 {
					t.Fatalf("webhooks not found: found=%v len=%d err=%v", found, len(webhooks), err)
				}
				path, found, err := unstructured.NestedString(webhooks[0].(map[string]interface{}), "clientConfig", "service", "path")
				if err != nil || !found {
					t.Fatalf("webhook service path not found: found=%v err=%v", found, err)
				}
				if path != "/validate-cluster-x-k8s-io-v1beta1-cluster" {
					t.Fatalf("webhook client config drift was not corrected: %q", path)
				}
				caBundle, found, err := unstructured.NestedFieldCopy(webhooks[0].(map[string]interface{}), "clientConfig", "caBundle")
				if err != nil || !found || !isExpectedWebhookCABundle(caBundle) {
					t.Fatalf("webhook injected caBundle was not preserved: found=%v caBundle=%#v err=%v", found, caBundle, err)
				}
			},
		},
		{
			name:    "service selector and ports",
			desired: desiredService(),
			current: driftedService(),
			assert: func(t *testing.T, got *unstructured.Unstructured) {
				selector, found, err := unstructured.NestedStringMap(got.Object, "spec", "selector")
				if err != nil || !found {
					t.Fatalf("service selector not found: found=%v err=%v", found, err)
				}
				if selector["control-plane"] != "capi-controller-manager" {
					t.Fatalf("service selector drift was not corrected: %#v", selector)
				}

				ports, found, err := unstructured.NestedSlice(got.Object, "spec", "ports")
				if err != nil || !found || len(ports) != 1 {
					t.Fatalf("service ports not found: found=%v len=%d err=%v", found, len(ports), err)
				}
				port := ports[0].(map[string]interface{})
				if port["targetPort"] != int64(9443) {
					t.Fatalf("service port drift was not corrected: %#v", port)
				}
				clusterIP, found, err := unstructured.NestedString(got.Object, "spec", "clusterIP")
				if err != nil || !found || clusterIP != "10.0.0.12" {
					t.Fatalf("service clusterIP was not preserved: found=%v clusterIP=%q err=%v", found, clusterIP, err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().
				WithScheme(clusterctlTestScheme(t)).
				WithRuntimeObjects(tt.current.DeepCopy()).
				Build()

			if err := reconcileClusterctlComponents(ctx, c, []unstructured.Unstructured{tt.desired}); err != nil {
				t.Fatalf("reconcileClusterctlComponents returned error: %v", err)
			}

			got := &unstructured.Unstructured{}
			got.SetGroupVersionKind(tt.desired.GroupVersionKind())
			if err := c.Get(ctx, client.ObjectKeyFromObject(&tt.desired), got); err != nil {
				t.Fatalf("failed to get reconciled object: %v", err)
			}
			tt.assert(t, got)
		})
	}
}

func clusterctlTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	for _, addToScheme := range []func(*runtime.Scheme) error{
		appsv1.AddToScheme,
		corev1.AddToScheme,
		rbacv1.AddToScheme,
		admissionregistrationv1.AddToScheme,
	} {
		if err := addToScheme(scheme); err != nil {
			t.Fatalf("failed to add test scheme: %v", err)
		}
	}
	return scheme
}

func isExpectedWebhookCABundle(value interface{}) bool {
	switch typed := value.(type) {
	case string:
		return typed == "aW5qZWN0ZWQtY2EtYnVuZGxl"
	case []byte:
		return string(typed) == "injected-ca-bundle"
	default:
		return false
	}
}

func unstructuredObject(apiVersion, kind, namespace, name string, object map[string]interface{}) unstructured.Unstructured {
	obj := unstructured.Unstructured{Object: object}
	obj.SetAPIVersion(apiVersion)
	obj.SetKind(kind)
	obj.SetNamespace(namespace)
	obj.SetName(name)
	return obj
}

func desiredDeployment() unstructured.Unstructured {
	return unstructuredObject("apps/v1", "Deployment", "oci-openshift-autoscaling-operator", "capi-controller-manager", map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]interface{}{
				"cluster.x-k8s.io/provider": "cluster-api",
			},
		},
		"spec": map[string]interface{}{
			"selector": map[string]interface{}{
				"matchLabels": map[string]interface{}{
					"cluster.x-k8s.io/provider": "cluster-api",
				},
			},
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"cluster.x-k8s.io/provider": "cluster-api",
					},
				},
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "manager",
							"image": "example.com/cluster-api-controller:v1",
							"args": []interface{}{
								"--leader-elect",
								"--diagnostics-address=:8443",
							},
						},
					},
					"volumes": []interface{}{
						map[string]interface{}{
							"name": "cert",
							"secret": map[string]interface{}{
								"secretName": "capi-webhook-service-cert",
							},
						},
					},
				},
			},
		},
	})
}

func driftedDeployment() unstructured.Unstructured {
	obj := desiredDeployment()
	_ = unstructured.SetNestedStringMap(obj.Object, map[string]string{
		"cluster.x-k8s.io/provider": "drifted",
	}, "spec", "selector", "matchLabels")
	_ = unstructured.SetNestedSlice(obj.Object, []interface{}{
		map[string]interface{}{
			"name":  "manager",
			"image": "example.com/cluster-api-controller:v1",
			"args": []interface{}{
				"--stale-arg",
			},
		},
	}, "spec", "template", "spec", "containers")
	_ = unstructured.SetNestedSlice(obj.Object, []interface{}{}, "spec", "template", "spec", "volumes")
	return obj
}

func desiredClusterRole() unstructured.Unstructured {
	return unstructuredObject("rbac.authorization.k8s.io/v1", "ClusterRole", "", "capi-manager-role", map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"apiGroups": []interface{}{"cluster.x-k8s.io"},
				"resources": []interface{}{"clusters"},
				"verbs":     []interface{}{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	})
}

func driftedClusterRole() unstructured.Unstructured {
	obj := desiredClusterRole()
	obj.Object["rules"] = []interface{}{
		map[string]interface{}{
			"apiGroups": []interface{}{"cluster.x-k8s.io"},
			"resources": []interface{}{"clusters"},
			"verbs":     []interface{}{"get"},
		},
	}
	return obj
}

func desiredValidatingWebhookConfiguration() unstructured.Unstructured {
	return unstructuredObject("admissionregistration.k8s.io/v1", "ValidatingWebhookConfiguration", "", "capi-validating-webhook-configuration", map[string]interface{}{
		"webhooks": []interface{}{
			map[string]interface{}{
				"name": "validation.cluster.cluster.x-k8s.io",
				"clientConfig": map[string]interface{}{
					"service": map[string]interface{}{
						"name":      "capi-webhook-service",
						"namespace": "oci-openshift-autoscaling-operator",
						"path":      "/validate-cluster-x-k8s-io-v1beta1-cluster",
					},
				},
				"rules": []interface{}{
					map[string]interface{}{
						"apiGroups":   []interface{}{"cluster.x-k8s.io"},
						"apiVersions": []interface{}{"v1beta1"},
						"operations":  []interface{}{"CREATE", "UPDATE"},
						"resources":   []interface{}{"clusters"},
					},
				},
			},
		},
	})
}

func driftedValidatingWebhookConfiguration() unstructured.Unstructured {
	obj := desiredValidatingWebhookConfiguration()
	webhooks, _, _ := unstructured.NestedSlice(obj.Object, "webhooks")
	webhook := webhooks[0].(map[string]interface{})
	_ = unstructured.SetNestedField(webhook, "/stale-path", "clientConfig", "service", "path")
	_ = unstructured.SetNestedField(webhook, "aW5qZWN0ZWQtY2EtYnVuZGxl", "clientConfig", "caBundle")
	webhooks[0] = webhook
	_ = unstructured.SetNestedSlice(obj.Object, webhooks, "webhooks")
	return obj
}

func desiredService() unstructured.Unstructured {
	return unstructuredObject("v1", "Service", "oci-openshift-autoscaling-operator", "capi-webhook-service", map[string]interface{}{
		"spec": map[string]interface{}{
			"selector": map[string]interface{}{
				"control-plane": "capi-controller-manager",
			},
			"ports": []interface{}{
				map[string]interface{}{
					"name":       "https",
					"port":       int64(443),
					"protocol":   "TCP",
					"targetPort": int64(9443),
				},
			},
		},
	})
}

func driftedService() unstructured.Unstructured {
	obj := desiredService()
	_ = unstructured.SetNestedStringMap(obj.Object, map[string]string{
		"control-plane": "drifted-controller-manager",
	}, "spec", "selector")
	_ = unstructured.SetNestedSlice(obj.Object, []interface{}{
		map[string]interface{}{
			"name":       "https",
			"port":       int64(443),
			"protocol":   "TCP",
			"targetPort": int64(9444),
		},
	}, "spec", "ports")
	_ = unstructured.SetNestedField(obj.Object, "10.0.0.12", "spec", "clusterIP")
	_ = unstructured.SetNestedSlice(obj.Object, []interface{}{"10.0.0.12"}, "spec", "clusterIPs")
	_ = unstructured.SetNestedSlice(obj.Object, []interface{}{"IPv4"}, "spec", "ipFamilies")
	_ = unstructured.SetNestedField(obj.Object, "SingleStack", "spec", "ipFamilyPolicy")
	_ = unstructured.SetNestedField(obj.Object, "Cluster", "spec", "internalTrafficPolicy")
	return obj
}
