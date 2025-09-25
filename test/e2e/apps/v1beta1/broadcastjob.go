/*
Copyright 2021 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"context"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	appsv1beta1 "github.com/openkruise/kruise/apis/apps/v1beta1"
	kruiseclientset "github.com/openkruise/kruise/pkg/client/clientset/versioned"
	"github.com/openkruise/kruise/pkg/util"
	"github.com/openkruise/kruise/test/e2e/framework/common"
	"github.com/openkruise/kruise/test/e2e/framework/v1beta1"
)

var _ = ginkgo.Describe("BroadcastJob v1beta1", ginkgo.Label("BroadcastJob", "job", "workload", "v1beta1"), func() {
	f := v1beta1.NewDefaultFramework("broadcastjobs-v1beta1")
	var ns string
	var c clientset.Interface
	var kc kruiseclientset.Interface
	var tester *v1beta1.BroadcastJobTester
	var nodeTester *v1beta1.NodeTester
	var randStr string

	ginkgo.BeforeEach(func() {
		c = f.ClientSet
		kc = f.KruiseClientSet
		ns = f.Namespace.Name
		tester = v1beta1.NewBroadcastJobTester(c, kc, ns)
		nodeTester = v1beta1.NewNodeTester(c)
		randStr = rand.String(10)
	})

	ginkgo.AfterEach(func() {
		err := nodeTester.DeleteFakeNode(randStr)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	f.AfterEachActions = []func(){
		func() {
			// Print debug info if it fails
			if ginkgo.CurrentSpecReport().Failed() {
				allNodes, err := nodeTester.ListNodesWithFake()
				if err != nil {
					common.Logf("[FAILURE_DEBUG] List Nodes error: %v", err)
				} else {
					common.Logf("[FAILURE_DEBUG] List Nodes: %v", allNodes)
				}
				job, err := tester.GetBroadcastJob("job-" + randStr)
				if err != nil {
					common.Logf("[FAILURE_DEBUG] Get BroadcastJob %s error: %v", "job-"+randStr, err)
				} else {
					common.Logf("[FAILURE_DEBUG] Get BroadcastJob: %v", util.DumpJSON(job))
				}
			}
		},
	}

	ginkgo.Context("BroadcastJob v1beta1 dispatching", func() {
		ginkgo.It("succeeds for parallelism < number of node", func() {
			ginkgo.By("Create Fake Node " + randStr)
			fakeNode, err := nodeTester.CreateFakeNode(randStr)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("Create BroadcastJob v1beta1 job-" + randStr)
			job := &appsv1beta1.BroadcastJob{
				ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "job-" + randStr},
				Spec: appsv1beta1.BroadcastJobSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Tolerations: []v1.Toleration{{Key: v1beta1.E2eFakeKey, Operator: v1.TolerationOpEqual, Value: randStr, Effect: v1.TaintEffectNoSchedule}},
							Containers: []v1.Container{{
								Name:    "box",
								Image:   common.BusyboxImage,
								Command: []string{"/bin/sh", "-c", "sleep 5"},
							}},
							RestartPolicy: v1.RestartPolicyNever,
						},
					},
					CompletionPolicy: appsv1beta1.CompletionPolicy{Type: appsv1beta1.Always},
				},
			}

			nodes, err := nodeTester.ListRealNodesWithFake(job.Spec.Template.Spec.Tolerations)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			parallelism := intstr.FromInt(max(len(nodes)-1, min(len(nodes), 1)))
			job.Spec.Parallelism = &parallelism

			job, err = tester.CreateBroadcastJob(job)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("Check the status of job")
			gomega.Eventually(func() int32 {
				job, err = tester.GetBroadcastJob(job.Name)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				return job.Status.Desired
			}, 10*time.Second, time.Second).Should(gomega.Equal(int32(len(nodes))))

			gomega.Eventually(func() int {
				pods, err := tester.GetPodsOfJob(job)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				var fakePod *v1.Pod
				for _, p := range pods {
					if p.Spec.NodeName == fakeNode.Name {
						fakePod = p
						break
					}
				}
				if fakePod != nil && fakePod.Status.Phase != v1.PodSucceeded {
					ginkgo.By("Try to update Pod " + fakePod.Name + " to Succeeded")
					err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
						fakePod, err := c.CoreV1().Pods(job.Namespace).Get(context.TODO(), fakePod.Name, metav1.GetOptions{})
						if err != nil {
							return err
						}
						fakePod.Status.Phase = v1.PodSucceeded
						_, err = c.CoreV1().Pods(ns).UpdateStatus(context.TODO(), fakePod, metav1.UpdateOptions{})
						return err
					})
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
				}

				return len(pods)
			}, 180*time.Second, 3*time.Second).Should(gomega.Equal(len(nodes)))

			gomega.Eventually(func() int32 {
				job, err = tester.GetBroadcastJob(job.Name)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				return job.Status.Succeeded
			}, 60*time.Second, time.Second).Should(gomega.Equal(int32(len(nodes))))
		})

		ginkgo.It("test v1beta1 specific features", func() {
			ginkgo.By("Create BroadcastJob v1beta1 with TTL")
			job := &appsv1beta1.BroadcastJob{
				ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "job-ttl-" + randStr},
				Spec: appsv1beta1.BroadcastJobSpec{
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{{
								Name:    "box",
								Image:   common.BusyboxImage,
								Command: []string{"/bin/sh", "-c", "echo 'test'"},
							}},
							RestartPolicy: v1.RestartPolicyNever,
						},
					},
					CompletionPolicy: appsv1beta1.CompletionPolicy{
						Type:                    appsv1beta1.Always,
						TTLSecondsAfterFinished: func() *int32 { ttl := int32(30); return &ttl }(),
					},
				},
			}

			job, err := tester.CreateBroadcastJob(job)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("Verify TTL is set correctly")
			gomega.Expect(job.Spec.CompletionPolicy.TTLSecondsAfterFinished).NotTo(gomega.BeNil())
			gomega.Expect(*job.Spec.CompletionPolicy.TTLSecondsAfterFinished).To(gomega.Equal(int32(30)))
		})
	})
})

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
