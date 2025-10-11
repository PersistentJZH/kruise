/*
Copyright 2025 The Kruise Authors.

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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	AppsV1beta1 "github.com/openkruise/kruise/apis/apps/v1beta1"
	kruiseclientset "github.com/openkruise/kruise/pkg/client/clientset/versioned"
)

type ImageListPullJobTester struct {
	c  clientset.Interface
	kc kruiseclientset.Interface
}

func NewImageListPullJobTester(c clientset.Interface, kc kruiseclientset.Interface) *ImageListPullJobTester {
	return &ImageListPullJobTester{
		c:  c,
		kc: kc,
	}
}

func (tester *ImageListPullJobTester) CreateJob(job *AppsV1beta1.ImageListPullJob) error {
	_, err := tester.kc.AppsV1beta1().ImageListPullJobs(job.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	return err
}

func (tester *ImageListPullJobTester) DeleteJob(job *AppsV1beta1.ImageListPullJob) error {
	return tester.kc.AppsV1beta1().ImageListPullJobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{})
}

func (tester *ImageListPullJobTester) DeleteAllJobs(ns string) error {
	return tester.kc.AppsV1beta1().ImageListPullJobs(ns).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{})
}

func (tester *ImageListPullJobTester) GetJob(job *AppsV1beta1.ImageListPullJob) (*AppsV1beta1.ImageListPullJob, error) {
	return tester.kc.AppsV1beta1().ImageListPullJobs(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
}

func (tester *ImageListPullJobTester) ListJobs(ns string) (*AppsV1beta1.ImageListPullJobList, error) {
	return tester.kc.AppsV1beta1().ImageListPullJobs(ns).List(context.TODO(), metav1.ListOptions{})
}

func (tester *ImageListPullJobTester) FailNodeImageFast(name string) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		nodeImage, err := tester.kc.AppsV1beta1().NodeImages().Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if nodeImage.Status.ImageStatuses == nil {
			nodeImage.Status.ImageStatuses = map[string]AppsV1beta1.ImageStatus{}
		}
		var desired int32 = 0
		for image, spec := range nodeImage.Spec.Images {
			desired += int32(len(nodeImage.Spec.Images))
			tagStatuses := make([]AppsV1beta1.ImageTagStatus, len(spec.Tags))
			nodeImage.Status.ImageStatuses[image] = AppsV1beta1.ImageStatus{Tags: tagStatuses}
			for i, tag := range spec.Tags {
				nodeImage.Status.ImageStatuses[image].Tags[i] = AppsV1beta1.ImageTagStatus{
					Tag:            tag.Tag,
					Phase:          AppsV1beta1.ImagePhaseFailed,
					CompletionTime: &metav1.Time{Time: time.Now()},
					Version:        tag.Version,
					Message:        "node has not responded for a long time",
				}
			}
		}
		nodeImage.Status.Failed = desired
		nodeImage.Status.Desired = desired
		_, err = tester.kc.AppsV1beta1().NodeImages().UpdateStatus(context.TODO(), nodeImage, metav1.UpdateOptions{})
		return err
	})
}
