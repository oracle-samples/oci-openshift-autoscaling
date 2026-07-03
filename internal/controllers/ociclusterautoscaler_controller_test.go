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
	"k8s.io/apimachinery/pkg/api/errors"
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
			Namespace: "default", // TODO(user):Modify as needed
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
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
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
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
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
