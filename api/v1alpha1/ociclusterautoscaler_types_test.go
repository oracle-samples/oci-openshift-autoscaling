/*
Copyright (c) 2025, 2026, Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("OCIClusterAutoscaler CRD", func() {
	var (
		k8sClient client.Client
		ctx       context.Context
	)
	const namespace = "default"

	BeforeEach(func() {
		ctx = context.Background()
		scheme := clientgoscheme.Scheme
		err := AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient).NotTo(BeNil())
	})

	AfterEach(func() {
		// Delete any OCIClusterAutoscaler resources created during the test
		err := k8sClient.DeleteAllOf(ctx, &OCIClusterAutoscaler{}, client.InNamespace(namespace))
		Expect(err).NotTo(HaveOccurred())

		// Wait for deletion to complete
		Eventually(func() bool {
			list := &OCIClusterAutoscalerList{}
			err := k8sClient.List(ctx, list, client.InNamespace(namespace))
			if err != nil {
				return false
			}
			return len(list.Items) == 0
		}, "10s", "1s").Should(BeTrue())
	})

	It("should properly marshal and unmarshal JSON", func() {
		original := &OCIClusterAutoscaler{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "autoscaling.openshift.io/v1alpha1",
				Kind:       "OCIClusterAutoscaler",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoscaler",
				Namespace: namespace,
			},
			Spec: OCIClusterAutoscalerSpec{
				Autoscaling: AutoscalingConfig{
					MinNodes: ptr.To[int32](1),
					MaxNodes: ptr.To[int32](10),
					Shape:    "VM.Standard.E4.Flex",
					ShapeConfig: &ShapeConfig{
						CPUs:   ptr.To[int32](4),
						Memory: ptr.To[int32](16),
					},
					ImageID:        "ocid1.image.oc1.test",
					PoolIdentifier: "bm01",
				},
				CAPI: CAPIConfig{
					ClusterName: "test-cluster",
				},
				ClusterAutoscaler: ClusterAutoscalerConfig{
					Name:                 "cluster-autoscaler",
					ServiceAccountName:   "cluster-autoscaler",
					CloudProvider:        "oci",
					CreateRBAC:           ptr.To(true),
					CreateServiceAccount: ptr.To(true),
					Version:              "1.0.0",
				},
			},
			Status: OCIClusterAutoscalerStatus{
				Phase:                     "Running",
				CAPIInstalled:             true,
				ClusterAutoscalerDeployed: true,
				ObservedGeneration:        1,
			},
		}

		// Marshal to JSON
		jsonData, err := json.Marshal(original)
		Expect(err).NotTo(HaveOccurred())

		// Unmarshal back to object
		restored := &OCIClusterAutoscaler{}
		err = json.Unmarshal(jsonData, restored)
		Expect(err).NotTo(HaveOccurred())

		// Verify fields are preserved
		Expect(restored.Name).To(Equal(original.Name))
		Expect(restored.Namespace).To(Equal(original.Namespace))
		Expect(restored.Spec.Autoscaling.MinNodes).To(Equal(original.Spec.Autoscaling.MinNodes))
		Expect(restored.Spec.Autoscaling.MaxNodes).To(Equal(original.Spec.Autoscaling.MaxNodes))
		Expect(restored.Spec.Autoscaling.Shape).To(Equal(original.Spec.Autoscaling.Shape))
		Expect(restored.Spec.Autoscaling.ShapeConfig.CPUs).To(Equal(original.Spec.Autoscaling.ShapeConfig.CPUs))
		Expect(restored.Spec.Autoscaling.ShapeConfig.Memory).To(Equal(original.Spec.Autoscaling.ShapeConfig.Memory))
		Expect(restored.Spec.Autoscaling.PoolIdentifier).To(Equal(original.Spec.Autoscaling.PoolIdentifier))
		Expect(restored.Status.Phase).To(Equal(original.Status.Phase))
		Expect(restored.Status.CAPIInstalled).To(Equal(original.Status.CAPIInstalled))
		Expect(restored.Status.ClusterAutoscalerDeployed).To(Equal(original.Status.ClusterAutoscalerDeployed))
	})

	It("should distinguish omitted autoscaling node counts from explicit zero", func() {
		omitted := &OCIClusterAutoscaler{}
		err := json.Unmarshal([]byte(`{"spec":{"autoscaling":{}}}`), omitted)
		Expect(err).NotTo(HaveOccurred())
		Expect(omitted.Spec.Autoscaling.MinNodes).To(BeNil())
		Expect(omitted.Spec.Autoscaling.MaxNodes).To(BeNil())

		explicitZero := &OCIClusterAutoscaler{}
		err = json.Unmarshal([]byte(`{"spec":{"autoscaling":{"minNodes":0,"maxNodes":0}}}`), explicitZero)
		Expect(err).NotTo(HaveOccurred())
		Expect(explicitZero.Spec.Autoscaling.MinNodes).NotTo(BeNil())
		Expect(*explicitZero.Spec.Autoscaling.MinNodes).To(Equal(int32(0)))
		Expect(explicitZero.Spec.Autoscaling.MaxNodes).NotTo(BeNil())
		Expect(*explicitZero.Spec.Autoscaling.MaxNodes).To(Equal(int32(0)))
	})

	It("should validate minimum node count", func() {
		autoscaler := &OCIClusterAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoscaler",
				Namespace: namespace,
			},
			Spec: OCIClusterAutoscalerSpec{
				Autoscaling: AutoscalingConfig{
					MinNodes: ptr.To[int32](-1), // This should fail validation
					MaxNodes: ptr.To[int32](10),
				},
			},
		}

		// Try to create the resource - this should fail validation
		err := k8sClient.Create(ctx, autoscaler)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.autoscaling.minNodes"))
	})

	It("should validate maximum node count", func() {
		autoscaler := &OCIClusterAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoscaler-negative-max",
				Namespace: namespace,
			},
			Spec: OCIClusterAutoscalerSpec{
				Autoscaling: AutoscalingConfig{
					MinNodes: ptr.To[int32](0),
					MaxNodes: ptr.To[int32](-1),
				},
			},
		}

		err := k8sClient.Create(ctx, autoscaler)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.autoscaling.maxNodes"))
	})

	It("should validate min node count does not exceed max node count", func() {
		autoscaler := &OCIClusterAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoscaler-invalid-range",
				Namespace: namespace,
			},
			Spec: OCIClusterAutoscalerSpec{
				Autoscaling: AutoscalingConfig{
					MinNodes: ptr.To[int32](5),
					MaxNodes: ptr.To[int32](3),
				},
			},
		}

		err := k8sClient.Create(ctx, autoscaler)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("minNodes must be less than or equal to maxNodes"))
	})

	It("should validate flexible shape CPU and memory", func() {
		invalidCPU := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": GroupVersion.String(),
				"kind":       "OCIClusterAutoscaler",
				"metadata": map[string]any{
					"name":      "test-autoscaler-zero-cpu",
					"namespace": namespace,
				},
				"spec": map[string]any{
					"autoscaling": map[string]any{
						"minNodes": int64(0),
						"maxNodes": int64(1),
						"shapeConfig": map[string]any{
							"cpus":   int64(0),
							"memory": int64(16),
						},
					},
				},
			},
		}

		err := k8sClient.Create(ctx, invalidCPU)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.autoscaling.shapeConfig.cpus"))

		invalidMemory := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": GroupVersion.String(),
				"kind":       "OCIClusterAutoscaler",
				"metadata": map[string]any{
					"name":      "test-autoscaler-zero-memory",
					"namespace": namespace,
				},
				"spec": map[string]any{
					"autoscaling": map[string]any{
						"minNodes": int64(0),
						"maxNodes": int64(1),
						"shapeConfig": map[string]any{
							"cpus":   int64(2),
							"memory": int64(0),
						},
					},
				},
			},
		}

		err = k8sClient.Create(ctx, invalidMemory)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.autoscaling.shapeConfig.memory"))
	})

	It("should prune unsupported namespace override fields", func() {
		autoscaler := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": GroupVersion.String(),
				"kind":       "OCIClusterAutoscaler",
				"metadata": map[string]any{
					"name":      "test-autoscaler-namespace-prune",
					"namespace": namespace,
				},
				"spec": map[string]any{
					"autoscaling": map[string]any{
						"minNodes": int64(0),
						"maxNodes": int64(1),
					},
					"capi": map[string]any{
						"namespace":   "managed-a",
						"clusterName": "cluster-a",
					},
					"clusterAutoscaler": map[string]any{
						"namespace": "autoscaler-a",
						"name":      "autoscaler-a",
					},
				},
			},
		}

		Expect(k8sClient.Create(ctx, autoscaler)).To(Succeed())

		stored := &unstructured.Unstructured{}
		stored.SetAPIVersion(GroupVersion.String())
		stored.SetKind("OCIClusterAutoscaler")
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "test-autoscaler-namespace-prune", Namespace: namespace}, stored)).To(Succeed())

		_, found, err := unstructured.NestedString(stored.Object, "spec", "capi", "namespace")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeFalse())

		_, found, err = unstructured.NestedString(stored.Object, "spec", "clusterAutoscaler", "namespace")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeFalse())
	})

	It("should validate autoscaler pool identifier format", func() {
		autoscaler := &OCIClusterAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoscaler",
				Namespace: namespace,
			},
			Spec: OCIClusterAutoscalerSpec{
				Autoscaling: AutoscalingConfig{
					MinNodes:       ptr.To[int32](1),
					MaxNodes:       ptr.To[int32](10),
					PoolIdentifier: "BM001",
				},
			},
		}

		err := k8sClient.Create(ctx, autoscaler)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.autoscaling.poolIdentifier"))
	})

	It("should validate autoscaler pool identifier maximum length", func() {
		autoscaler := &OCIClusterAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoscaler",
				Namespace: namespace,
			},
			Spec: OCIClusterAutoscalerSpec{
				Autoscaling: AutoscalingConfig{
					MinNodes:       ptr.To[int32](1),
					MaxNodes:       ptr.To[int32](10),
					PoolIdentifier: "abcdef",
				},
			},
		}

		err := k8sClient.Create(ctx, autoscaler)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.autoscaling.poolIdentifier"))
	})

	It("should validate generated node pool name maximum length", func() {
		autoscaler := &OCIClusterAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoscaler",
				Namespace: namespace,
			},
			Spec: OCIClusterAutoscalerSpec{
				Autoscaling: AutoscalingConfig{
					MinNodes:       ptr.To[int32](1),
					MaxNodes:       ptr.To[int32](10),
					PoolIdentifier: "pool1",
				},
				CAPI: CAPIConfig{
					ClusterName: strings.Repeat("a", 46),
				},
			},
		}

		err := k8sClient.Create(ctx, autoscaler)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("node pool name"))
	})

	It("should validate Kubernetes object name fields", func() {
		tests := []struct {
			name      string
			fieldPath string
			mutate    func(*OCIClusterAutoscaler)
		}{
			{
				name:      "capi-cluster-name",
				fieldPath: "spec.capi.clusterName",
				mutate: func(autoscaler *OCIClusterAutoscaler) {
					autoscaler.Spec.CAPI.ClusterName = "Invalid_Name"
				},
			},
			{
				name:      "cluster-autoscaler-name",
				fieldPath: "spec.clusterAutoscaler.name",
				mutate: func(autoscaler *OCIClusterAutoscaler) {
					autoscaler.Spec.ClusterAutoscaler.Name = "Invalid_Name"
				},
			},
			{
				name:      "cluster-autoscaler-service-account",
				fieldPath: "spec.clusterAutoscaler.serviceAccountName",
				mutate: func(autoscaler *OCIClusterAutoscaler) {
					autoscaler.Spec.ClusterAutoscaler.ServiceAccountName = "Invalid_Name"
				},
			},
		}

		for _, tt := range tests {
			autoscaler := &OCIClusterAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("invalid-%s", tt.name),
					Namespace: namespace,
				},
				Spec: OCIClusterAutoscalerSpec{
					Autoscaling: AutoscalingConfig{
						MinNodes: ptr.To[int32](0),
						MaxNodes: ptr.To[int32](1),
					},
				},
			}
			tt.mutate(autoscaler)

			err := k8sClient.Create(ctx, autoscaler)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(tt.fieldPath))
		}
	})

	It("should reject identity field updates", func() {
		tests := []struct {
			name   string
			mutate func(*OCIClusterAutoscaler)
		}{
			{
				name: "pool-identifier",
				mutate: func(autoscaler *OCIClusterAutoscaler) {
					autoscaler.Spec.Autoscaling.PoolIdentifier = "bm02"
				},
			},
			{
				name: "capi-cluster-name",
				mutate: func(autoscaler *OCIClusterAutoscaler) {
					autoscaler.Spec.CAPI.ClusterName = "cluster-b"
				},
			},
			{
				name: "autoscaler-name",
				mutate: func(autoscaler *OCIClusterAutoscaler) {
					autoscaler.Spec.ClusterAutoscaler.Name = "autoscaler-b"
				},
			},
		}

		for _, tt := range tests {
			autoscaler := &OCIClusterAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("immutable-%s", tt.name),
					Namespace: namespace,
				},
				Spec: OCIClusterAutoscalerSpec{
					Autoscaling: AutoscalingConfig{
						MinNodes:       ptr.To[int32](0),
						MaxNodes:       ptr.To[int32](1),
						PoolIdentifier: "bm01",
					},
					CAPI: CAPIConfig{
						ClusterName: "cluster-a",
					},
					ClusterAutoscaler: ClusterAutoscalerConfig{
						Name: "autoscaler-a",
					},
				},
			}

			Expect(k8sClient.Create(ctx, autoscaler)).To(Succeed())
			stored := &OCIClusterAutoscaler{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: autoscaler.Name, Namespace: autoscaler.Namespace}, stored)).To(Succeed())
			tt.mutate(stored)

			err := k8sClient.Update(ctx, stored)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("immutable"))
		}
	})

	It("should handle nil ShapeConfig", func() {
		autoscaler := &OCIClusterAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoscaler",
				Namespace: namespace,
			},
			Spec: OCIClusterAutoscalerSpec{
				Autoscaling: AutoscalingConfig{
					MinNodes: ptr.To[int32](1),
					MaxNodes: ptr.To[int32](10),
					Shape:    "VM.Standard.E4.Flex",
					// ShapeConfig is intentionally nil
				},
			},
		}

		// Verify we can marshal/unmarshal with nil ShapeConfig
		jsonData, err := json.Marshal(autoscaler)
		Expect(err).NotTo(HaveOccurred())

		restored := &OCIClusterAutoscaler{}
		err = json.Unmarshal(jsonData, restored)
		Expect(err).NotTo(HaveOccurred())
		Expect(restored.Spec.Autoscaling.ShapeConfig).To(BeNil())

		// Should be able to create the resource
		err = k8sClient.Create(ctx, autoscaler)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should properly handle status conditions", func() {
		autoscaler := &OCIClusterAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoscaler",
				Namespace: namespace,
			},
			Spec: OCIClusterAutoscalerSpec{
				Autoscaling: AutoscalingConfig{
					MinNodes: ptr.To[int32](1),
					MaxNodes: ptr.To[int32](10),
				},
			},
		}

		// Create the resource
		err := k8sClient.Create(ctx, autoscaler)
		Expect(err).NotTo(HaveOccurred())

		// Verify there are no conditions
		updated := &OCIClusterAutoscaler{}
		err = k8sClient.Get(ctx, client.ObjectKey{Name: autoscaler.Name, Namespace: autoscaler.Namespace}, updated)
		Expect(err).NotTo(HaveOccurred())
		Expect(updated.Status.Conditions).To(HaveLen(0))

		// Add a condition
		condition := metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "TestReason",
			Message:            "Test message",
			LastTransitionTime: metav1.Now(),
		}

		autoscaler.Status.Conditions = append(autoscaler.Status.Conditions, condition)
		err = k8sClient.Status().Update(ctx, autoscaler)
		Expect(err).NotTo(HaveOccurred())

		// Verify condition was saved
		err = k8sClient.Get(ctx, client.ObjectKey{Name: autoscaler.Name, Namespace: autoscaler.Namespace}, updated)
		Expect(err).NotTo(HaveOccurred())
		Expect(updated.Status.Conditions).To(HaveLen(1))
		Expect(updated.Status.Conditions[0].Type).To(Equal("Ready"))
		Expect(updated.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
		Expect(updated.Status.Conditions[0].Reason).To(Equal("TestReason"))
		Expect(updated.Status.Conditions[0].Message).To(Equal("Test message"))
	})
})
