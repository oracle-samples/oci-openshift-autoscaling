/*
Copyright (c) 2025, 2026 Oracle and/or its affiliates.
Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.
*/

package controllers

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capiv1alpha1 "github.com/openshift/oci-capi-operator/api/v1alpha1"
	enableautoscaler "github.com/openshift/oci-capi-operator/internal/components/enable_autoscaler"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("OCIClusterAutoscaler Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		ociclusterautoscaler := &capiv1alpha1.OCIClusterAutoscaler{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind OCIClusterAutoscaler")
			err := k8sClient.Get(ctx, typeNamespacedName, ociclusterautoscaler)
			if err != nil && errors.IsNotFound(err) {
				resource := &capiv1alpha1.OCIClusterAutoscaler{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &capiv1alpha1.OCIClusterAutoscaler{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())
			if controllerutil.ContainsFinalizer(resource, FinalizerName) {
				controllerutil.RemoveFinalizer(resource, FinalizerName)
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())
			}

			By("Cleanup the specific resource instance OCIClusterAutoscaler")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &OCIClusterAutoscalerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			stored := &capiv1alpha1.OCIClusterAutoscaler{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, stored)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(stored, FinalizerName)).To(BeTrue())
			Expect(stored.Status.Phase).To(Equal(PhaseInitializing))

			ready := meta.FindStatusCondition(stored.Status.Conditions, capiv1alpha1.ConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionUnknown))
			Expect(ready.Reason).To(Equal(ReasonInitializing))
		})

		It("persists policy rejection status when validation fails", func() {
			resource := &capiv1alpha1.OCIClusterAutoscaler{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			controllerutil.AddFinalizer(resource, FinalizerName)
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Status.Phase = PhaseInitializing
			Expect(k8sClient.Status().Update(ctx, resource)).To(Succeed())

			controllerReconciler := &OCIClusterAutoscalerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				AutoScalingConfig: enableautoscaler.Config{
					AutoScalingConfig: enableautoscaler.AutoScalingConfig{
						MinNodes: 5,
						MaxNodes: 3,
					},
				},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).To(HaveOccurred())

			stored := &capiv1alpha1.OCIClusterAutoscaler{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, stored)).To(Succeed())
			Expect(stored.Status.Phase).To(Equal(PhaseBlocked))

			policy := meta.FindStatusCondition(stored.Status.Conditions, capiv1alpha1.ConditionPolicyAccepted)
			Expect(policy).NotTo(BeNil())
			Expect(policy.Status).To(Equal(metav1.ConditionFalse))
			Expect(policy.Reason).To(Equal(ReasonPolicyRejected))

			ready := meta.FindStatusCondition(stored.Status.Conditions, capiv1alpha1.ConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(ReasonPolicyRejected))
		})

		It("selects one singleton owner when multiple resources exist", func() {
			contender := &capiv1alpha1.OCIClusterAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "zzz-contender",
					Namespace: "default",
				},
			}
			Expect(k8sClient.Create(ctx, contender)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, contender)
			})

			controllerReconciler := &OCIClusterAutoscalerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			selectedOwner, conflict, err := controllerReconciler.singletonConflict(ctx, contender)
			Expect(err).NotTo(HaveOccurred())
			Expect(conflict).To(BeTrue())
			Expect(selectedOwner).To(Equal(typeNamespacedName))
		})
	})
})
