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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func makeStartingPod(name string, containerStatuses []corev1.ContainerStatus, podPhase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseStarting,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "myrepo/myimage:v1"}},
		},
		Status: corev1.PodStatus{
			Phase:             podPhase,
			ContainerStatuses: containerStatuses,
		},
	}
}

func makeFailedPod(name string, containerStatuses []corev1.ContainerStatus, podReason, podMessage string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseFailed,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "myrepo/myimage:v1"}},
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodFailed,
			Reason:            podReason,
			Message:           podMessage,
			ContainerStatuses: containerStatuses,
		},
	}
}

func TestBuildPodStatusDetail_NoPodStatus_Pulling(t *testing.T) {
	pod := makeStartingPod("pod1", nil, "")

	detail := BuildSandboxStatusDetailFromPod(pod)
	if detail == nil {
		t.Fatal("expected detail to be set")
	}
	if detail.Reason != "Pulling" {
		t.Errorf("reason = %q, want %q", detail.Reason, "Pulling")
	}
	if detail.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestBuildPodStatusDetail_ImagePullBackOff(t *testing.T) {
	cs := []corev1.ContainerStatus{
		{
			Name:  "sandbox",
			Image: "myrepo/myimage:v1",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason:  "ImagePullBackOff",
					Message: "Back-off pulling image",
				},
			},
		},
	}
	pod := makeStartingPod("pod2", cs, "")

	detail := BuildSandboxStatusDetailFromPod(pod)
	if detail == nil {
		t.Fatal("expected detail to be set")
	}
	if detail.Reason != "ImagePullBackOff" {
		t.Errorf("reason = %q, want %q", detail.Reason, "ImagePullBackOff")
	}
}

func TestBuildPodStatusDetail_ErrImagePull(t *testing.T) {
	cs := []corev1.ContainerStatus{
		{
			Name:  "sandbox",
			Image: "myrepo/myimage:v1",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason:  "ErrImagePull",
					Message: "no such image",
				},
			},
		},
	}
	pod := makeStartingPod("pod3", cs, "")

	detail := BuildSandboxStatusDetailFromPod(pod)
	if detail == nil {
		t.Fatal("expected detail to be set")
	}
	if detail.Reason != "ErrImagePull" {
		t.Errorf("reason = %q, want %q", detail.Reason, "ErrImagePull")
	}
	if detail.Message != "no such image" {
		t.Errorf("message = %q, want %q", detail.Message, "no such image")
	}
}

func TestBuildPodStatusDetail_CrashLoopBackOff(t *testing.T) {
	cs := []corev1.ContainerStatus{
		{
			Name:  "sandbox",
			Image: "myrepo/myimage:v1",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason:  "CrashLoopBackOff",
					Message: "exit code 1",
				},
			},
		},
	}
	pod := makeStartingPod("pod4", cs, "")

	detail := BuildSandboxStatusDetailFromPod(pod)
	if detail == nil {
		t.Fatal("expected detail to be set")
	}
	if detail.Reason != "CrashLoopBackOff" {
		t.Errorf("reason = %q, want %q", detail.Reason, "CrashLoopBackOff")
	}
}

func TestBuildPodStatusDetail_PodFailed(t *testing.T) {
	pod := makeFailedPod("pod5", nil, "OOMKilled", "pod was OOM killed")

	detail := BuildSandboxStatusDetailFromPod(pod)
	if detail == nil {
		t.Fatal("expected detail to be set")
	}
	if detail.Reason != "OOMKilled" {
		t.Errorf("reason = %q, want %q", detail.Reason, "OOMKilled")
	}
}

func TestBuildPodStatusDetail_SkipsRunningPods(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod7",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "myrepo/myimage:v1"}},
		},
	}

	detail := BuildSandboxStatusDetailFromPod(pod)
	if detail != nil {
		t.Errorf("expected no detail for running pod, got: %+v", detail)
	}
}

func TestBuildPodStatusDetail_SkipsIdlePods(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod8",
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "busybox:1.36"}},
		},
	}

	detail := BuildSandboxStatusDetailFromPod(pod)
	if detail != nil {
		t.Errorf("expected no detail for idle pod, got: %+v", detail)
	}
}

func TestBuildPodStatusDetail_PodNameAndPhaseSet(t *testing.T) {
	pod := makeStartingPod("pod9", nil, "")

	detail := BuildSandboxStatusDetailFromPod(pod)
	if detail == nil {
		t.Fatal("expected detail to be set")
	}
	if detail.PodName != "pod9" {
		t.Errorf("PodName = %q, want %q", detail.PodName, "pod9")
	}
	if detail.Phase != agentsv1alpha1.SandboxPhaseStarting {
		t.Errorf("Phase = %q, want %q", detail.Phase, agentsv1alpha1.SandboxPhaseStarting)
	}
}
