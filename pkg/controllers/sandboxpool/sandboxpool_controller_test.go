// Copyright 2026 ScitiX
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sandboxpool

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

var _ = Describe("SandboxPool Controller", func() {
	const (
		resourceName = "test-resource"
		namespace    = "default"
		timeout      = time.Second * 10
		interval     = time.Millisecond * 250
	)

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: namespace,
	}

	newControllerReconciler := func() *SandboxPoolReconciler {
		return &SandboxPoolReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
	}

	reconcileResource := func() error {
		_, err := newControllerReconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		return err
	}

	AfterEach(func() {
		podList := &corev1.PodList{}
		Expect(k8sClient.List(ctx, podList,
			client.InNamespace(namespace),
			client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
		)).To(Succeed())

		for i := range podList.Items {
			pod := &podList.Items[i]
			pod.Finalizers = nil
			_ = k8sClient.Update(ctx, pod)
			_ = k8sClient.Delete(ctx, pod)
		}

		poolList := &agentsv1alpha1.SandboxPoolList{}
		Expect(k8sClient.List(ctx, poolList, client.InNamespace(namespace))).To(Succeed())

		for i := range poolList.Items {
			pool := &poolList.Items[i]
			pool.Finalizers = nil
			_ = k8sClient.Update(ctx, pool)
			_ = k8sClient.Delete(ctx, pool)
		}

		Eventually(func() (int, error) {
			updatedPoolList := &agentsv1alpha1.SandboxPoolList{}
			if err := k8sClient.List(ctx, updatedPoolList, client.InNamespace(namespace)); err != nil {
				return 0, err
			}

			podList := &corev1.PodList{}
			if err := k8sClient.List(ctx, podList,
				client.InNamespace(namespace),
				client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
			); err != nil {
				return 0, err
			}

			return len(updatedPoolList.Items) + len(podList.Items), nil
		}, timeout, interval).Should(Equal(0))
	})

	Context("When reconciling a resource", func() {
		It("should successfully create and reconcile pods", func() {
			By("creating a SandboxPool resource")
			sandboxpool := &agentsv1alpha1.SandboxPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: agentsv1alpha1.SandboxPoolSpec{
					Replicas: 3,
					EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
						Template: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "test-container",
										Image: "nginx:latest",
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, sandboxpool)).To(Succeed())

			By("waiting for pods to be created")
			Eventually(func() (int32, error) {
				if err := reconcileResource(); err != nil {
					return 0, err
				}

				podList := &corev1.PodList{}
				if err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
				); err != nil {
					return 0, err
				}
				return int32(len(podList.Items)), nil
			}, timeout, interval).Should(Equal(int32(3)))

			By("checking that status is updated correctly")
			Eventually(func() (int32, error) {
				if err := reconcileResource(); err != nil {
					return 0, err
				}

				updatedPool := &agentsv1alpha1.SandboxPool{}
				if err := k8sClient.Get(ctx, typeNamespacedName, updatedPool); err != nil {
					return 0, err
				}
				return updatedPool.Status.IdleReplicas, nil
			}, timeout, interval).Should(Equal(int32(3)))

			By("deleting the SandboxPool")
			Expect(k8sClient.Delete(ctx, sandboxpool)).To(Succeed())

			By("waiting for pods to be deleted")
			Eventually(func() (int32, error) {
				if err := reconcileResource(); err != nil && !errors.IsNotFound(err) {
					return 0, err
				}

				podList := &corev1.PodList{}
				if err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
				); err != nil {
					return 0, err
				}
				return int32(len(podList.Items)), nil
			}, timeout, interval).Should(Equal(int32(0)))
		})
	})

	Context("Pod status aggregation", func() {
		It("should correctly aggregate pod phases", func() {
			By("creating a SandboxPool")
			sandboxpool := &agentsv1alpha1.SandboxPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: agentsv1alpha1.SandboxPoolSpec{
					Replicas: 4,
					EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
						Template: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "test-container",
										Image: "nginx:latest",
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, sandboxpool)).To(Succeed())

			By("waiting for initial pods to be created")
			Eventually(func() (int32, error) {
				if err := reconcileResource(); err != nil {
					return 0, err
				}

				podList := &corev1.PodList{}
				if err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
				); err != nil {
					return 0, err
				}
				return int32(len(podList.Items)), nil
			}, timeout, interval).Should(Equal(int32(4)))

			By("updating pod phases manually")
			// Get the created pods
			podList := &corev1.PodList{}
			Expect(k8sClient.List(ctx, podList,
				client.InNamespace(namespace),
				client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
			)).To(Succeed())

			// Update pod phases: 1 idle, 1 running, 1 starting, 1 failed
			Expect(len(podList.Items)).To(BeNumerically(">=", 4))

			podList.Items[0].Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseIdle
			Expect(k8sClient.Update(ctx, &podList.Items[0])).To(Succeed())

			podList.Items[1].Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseRunning
			Expect(k8sClient.Update(ctx, &podList.Items[1])).To(Succeed())

			podList.Items[2].Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseStarting
			Expect(k8sClient.Update(ctx, &podList.Items[2])).To(Succeed())

			podList.Items[3].Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseFailed
			Expect(k8sClient.Update(ctx, &podList.Items[3])).To(Succeed())

			By("checking that status reflects correct phase counts")
			Eventually(func() (bool, error) {
				if err := reconcileResource(); err != nil {
					return false, err
				}
				updatedPool := &agentsv1alpha1.SandboxPool{}
				if err := k8sClient.Get(ctx, typeNamespacedName, updatedPool); err != nil {
					return false, err
				}
				return updatedPool.Status.IdleReplicas == 1 &&
					updatedPool.Status.RunningReplicas == 1 &&
					updatedPool.Status.StartingReplicas == 1 &&
					updatedPool.Status.FailedReplicas == 1, nil
			}, timeout, interval).Should(BeTrue())

			By("deleting the SandboxPool")
			Expect(k8sClient.Delete(ctx, sandboxpool)).To(Succeed())
		})
	})

	Context("Scale down with priority deletion", func() {
		It("should prioritize deleting idle pods over running pods", func() {
			By("creating a SandboxPool with 5 replicas")
			sandboxpool := &agentsv1alpha1.SandboxPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: agentsv1alpha1.SandboxPoolSpec{
					Replicas: 5,
					EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
						Template: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "test-container",
										Image: "nginx:latest",
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, sandboxpool)).To(Succeed())

			By("waiting for 5 pods to be created")
			Eventually(func() (int32, error) {
				if err := reconcileResource(); err != nil {
					return 0, err
				}

				podList := &corev1.PodList{}
				if err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
				); err != nil {
					return 0, err
				}
				return int32(len(podList.Items)), nil
			}, timeout, interval).Should(Equal(int32(5)))

			By("updating pod phases: 2 idle, 1 running, 1 starting, 1 failed")
			podList := &corev1.PodList{}
			Expect(k8sClient.List(ctx, podList,
				client.InNamespace(namespace),
				client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
			)).To(Succeed())

			podList.Items[0].Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseIdle
			Expect(k8sClient.Update(ctx, &podList.Items[0])).To(Succeed())

			podList.Items[1].Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseIdle
			Expect(k8sClient.Update(ctx, &podList.Items[1])).To(Succeed())

			podList.Items[2].Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseRunning
			Expect(k8sClient.Update(ctx, &podList.Items[2])).To(Succeed())

			podList.Items[3].Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseStarting
			Expect(k8sClient.Update(ctx, &podList.Items[3])).To(Succeed())

			podList.Items[4].Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseFailed
			Expect(k8sClient.Update(ctx, &podList.Items[4])).To(Succeed())

			By("scaling down to 2 replicas (should delete idle pods first)")
			updatedPool := &agentsv1alpha1.SandboxPool{}
			Eventually(func() error {
				if err := k8sClient.Get(ctx, typeNamespacedName, updatedPool); err != nil {
					return err
				}
				return nil
			}, timeout, interval).Should(Succeed())

			updatedPool.Spec.Replicas = 2
			Expect(k8sClient.Update(ctx, updatedPool)).To(Succeed())

			By("triggering reconciliation")
			Eventually(func() error {
				_, err := newControllerReconciler().Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				return err
			}, timeout, interval).Should(Succeed())

			By("waiting for scale down to complete (should have 2 pods)")
			Eventually(func() (int32, error) {
				if err := reconcileResource(); err != nil {
					return 0, err
				}

				podList := &corev1.PodList{}
				if err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
				); err != nil {
					return 0, err
				}
				return int32(len(podList.Items)), nil
			}, timeout, interval).Should(Equal(int32(2)))

			By("verifying that running pod was not deleted")
			podList = &corev1.PodList{}
			Expect(k8sClient.List(ctx, podList,
				client.InNamespace(namespace),
				client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
			)).To(Succeed())

			runningCount := 0
			for _, pod := range podList.Items {
				if pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] == agentsv1alpha1.SandboxPhaseRunning {
					runningCount++
				}
			}
			Expect(runningCount).To(Equal(1), "Running pod should not be deleted")

			By("deleting the SandboxPool")
			Expect(k8sClient.Delete(ctx, sandboxpool)).To(Succeed())
		})

		It("should never delete running pods even when scaling down", func() {
			By("creating a SandboxPool with 3 replicas")
			sandboxpool := &agentsv1alpha1.SandboxPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: agentsv1alpha1.SandboxPoolSpec{
					Replicas: 3,
					EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
						Template: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "test-container",
										Image: "nginx:latest",
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, sandboxpool)).To(Succeed())

			By("waiting for 3 pods to be created")
			Eventually(func() (int32, error) {
				if err := reconcileResource(); err != nil {
					return 0, err
				}

				podList := &corev1.PodList{}
				if err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
				); err != nil {
					return 0, err
				}
				return int32(len(podList.Items)), nil
			}, timeout, interval).Should(Equal(int32(3)))

			By("marking all pods as running")
			podList := &corev1.PodList{}
			Expect(k8sClient.List(ctx, podList,
				client.InNamespace(namespace),
				client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
			)).To(Succeed())

			for i := range podList.Items {
				podList.Items[i].Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseRunning
				Expect(k8sClient.Update(ctx, &podList.Items[i])).To(Succeed())
			}

			By("trying to scale down to 1 replica (should not delete running pods)")
			updatedPool := &agentsv1alpha1.SandboxPool{}
			Eventually(func() error {
				if err := k8sClient.Get(ctx, typeNamespacedName, updatedPool); err != nil {
					return err
				}
				return nil
			}, timeout, interval).Should(Succeed())

			updatedPool.Spec.Replicas = 1
			Expect(k8sClient.Update(ctx, updatedPool)).To(Succeed())

			By("triggering reconciliation")
			Eventually(func() error {
				_, err := newControllerReconciler().Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				return err
			}, timeout, interval).Should(Succeed())

			By("verifying that all 3 running pods still exist (none deleted)")
			Eventually(func() (int32, error) {
				podList := &corev1.PodList{}
				if err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
				); err != nil {
					return 0, err
				}
				return int32(len(podList.Items)), nil
			}, timeout, interval).Should(Equal(int32(3)))

			By("verifying all pods are still marked as running")
			podList = &corev1.PodList{}
			Expect(k8sClient.List(ctx, podList,
				client.InNamespace(namespace),
				client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
			)).To(Succeed())

			runningCount := 0
			for _, pod := range podList.Items {
				if pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] == agentsv1alpha1.SandboxPhaseRunning {
					runningCount++
				}
			}
			Expect(runningCount).To(Equal(3), "All running pods should be preserved")

			By("deleting the SandboxPool")
			Expect(k8sClient.Delete(ctx, sandboxpool)).To(Succeed())
		})
	})

	Context("Finalizer management", func() {
		It("should add finalizer and clean up pods on deletion", func() {
			By("creating a SandboxPool with 2 replicas")
			sandboxpool := &agentsv1alpha1.SandboxPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: agentsv1alpha1.SandboxPoolSpec{
					Replicas: 2,
					EmbeddedSandboxTemplate: agentsv1alpha1.EmbeddedSandboxTemplate{
						Template: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "test-container",
										Image: "nginx:latest",
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, sandboxpool)).To(Succeed())

			By("waiting for pods to be created")
			Eventually(func() (int32, error) {
				if err := reconcileResource(); err != nil {
					return 0, err
				}

				podList := &corev1.PodList{}
				if err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
				); err != nil {
					return 0, err
				}
				return int32(len(podList.Items)), nil
			}, timeout, interval).Should(Equal(int32(2)))

			By("checking that finalizer is added")
			Eventually(func() bool {
				if err := reconcileResource(); err != nil {
					return false
				}

				updatedPool := &agentsv1alpha1.SandboxPool{}
				if err := k8sClient.Get(ctx, typeNamespacedName, updatedPool); err != nil {
					return false
				}
				return containsString(updatedPool.Finalizers, FinalizerName)
			}, timeout, interval).Should(BeTrue())

			By("deleting the SandboxPool")
			Expect(k8sClient.Delete(ctx, sandboxpool)).To(Succeed())

			By("waiting for pods to be deleted")
			Eventually(func() (int32, error) {
				if err := reconcileResource(); err != nil && !errors.IsNotFound(err) {
					return 0, err
				}

				podList := &corev1.PodList{}
				if err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingFields{indexer.IndexFieldSandboxPool: resourceName},
				); err != nil {
					return 0, err
				}
				return int32(len(podList.Items)), nil
			}, timeout, interval).Should(Equal(int32(0)))

			By("checking that SandboxPool is fully deleted")
			Eventually(func() bool {
				_ = reconcileResource()

				pool := &agentsv1alpha1.SandboxPool{}
				err := k8sClient.Get(ctx, typeNamespacedName, pool)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})
	})
})
