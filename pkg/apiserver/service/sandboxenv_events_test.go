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

package service

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

const (
	eventsTestNS   = "default"
	eventsTestEnv  = "env-a"
	eventsTestPool = "env-a-pool"
)

func eventsTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("clientgo scheme: %v", err)
	}
	if err := agentsv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("agents scheme: %v", err)
	}
	return s
}

// One controller event, as stored, seen through both API views. This is what
// the cluster actually returns: events.k8s.io/v1 and core/v1 are two
// projections of one object, sharing metadata.name.
func bothViewsOfOneEvent(name, reason, message string, at time.Time) (*corev1.Event, *eventsv1.Event) {
	core := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: eventsTestNS},
		InvolvedObject: corev1.ObjectReference{
			Kind: "SandboxPool", Name: eventsTestPool, Namespace: eventsTestNS,
		},
		Reason:  reason,
		Message: message,
		Type:    corev1.EventTypeNormal,
		// The core/v1 view of an events.k8s.io record carries no Count and no
		// FirstTimestamp — which is exactly why it reads as a second, poorer
		// copy when both views are merged naively.
		EventTime: metav1.NewMicroTime(at),
	}
	modern := &eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: eventsTestNS},
		Regarding: corev1.ObjectReference{
			Kind: "SandboxPool", Name: eventsTestPool, Namespace: eventsTestNS,
		},
		Reason:              reason,
		Note:                message,
		Type:                corev1.EventTypeNormal,
		Action:              "ScaleUp",
		EventTime:           metav1.NewMicroTime(at),
		ReportingController: "sandboxpool-autoscaler",
	}
	return core, modern
}

// newEventsTestService seeds an Env with one member Pool plus the supplied
// Event objects, and returns a service reading from that fake cluster.
func newEventsTestService(t *testing.T, events ...client.Object) *k8sSandboxEnvService {
	t.Helper()
	env := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{Name: eventsTestEnv, Namespace: eventsTestNS},
	}
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      eventsTestPool,
			Namespace: eventsTestNS,
			Labels:    map[string]string{agentsv1alpha1.LabelEnv: eventsTestEnv},
		},
	}
	seed := append([]client.Object{env, pool}, events...)
	c := fake.NewClientBuilder().
		WithScheme(eventsTestScheme(t)).
		WithObjects(seed...).
		Build()
	return &k8sSandboxEnvService{client: c}
}

// A controller event must appear once, carrying the richer events.k8s.io
// fields — not twice, once per API view.
func TestListEvents_DeduplicatesAcrossBothAPIViews(t *testing.T) {
	at := time.Date(2026, 9, 15, 8, 55, 0, 0, time.UTC)
	core, modern := bothViewsOfOneEvent("evt-1", "AutoscalerScaleUp", "increased replicas from 1 to 6", at)

	svc := newEventsTestService(t, core, modern)
	got, appErr := svc.ListEvents(context.Background(), eventsTestNS, eventsTestEnv, 100)
	if appErr != nil {
		t.Fatalf("ListEvents: %v", appErr)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1 (the two API views are one stored object)", len(got))
	}
	if got[0].Action == nil || *got[0].Action != "ScaleUp" {
		t.Errorf("Action = %v, want ScaleUp — the events.k8s.io view should win", got[0].Action)
	}
	if got[0].Message != "increased replicas from 1 to 6" {
		t.Errorf("Message = %q", got[0].Message)
	}
}

// An event that exists only in core/v1 (kubelet, scheduler) has its own name
// and must still come through.
func TestListEvents_KeepsCoreOnlyEvents(t *testing.T) {
	at := time.Date(2026, 9, 15, 8, 55, 0, 0, time.UTC)
	core, modern := bothViewsOfOneEvent("evt-1", "AutoscalerScaleUp", "increased replicas from 1 to 6", at)
	kubeletOnly := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "evt-2", Namespace: eventsTestNS},
		InvolvedObject: corev1.ObjectReference{
			Kind: "SandboxPool", Name: eventsTestPool, Namespace: eventsTestNS,
		},
		Reason:         "FailedScheduling",
		Message:        "0/3 nodes are available",
		Type:           corev1.EventTypeWarning,
		FirstTimestamp: metav1.NewTime(at.Add(-time.Minute)),
		LastTimestamp:  metav1.NewTime(at.Add(-time.Minute)),
		Count:          3,
	}

	svc := newEventsTestService(t, core, modern, kubeletOnly)
	got, appErr := svc.ListEvents(context.Background(), eventsTestNS, eventsTestEnv, 100)
	if appErr != nil {
		t.Fatalf("ListEvents: %v", appErr)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	// Newest first.
	if got[0].Reason != "AutoscalerScaleUp" || got[1].Reason != "FailedScheduling" {
		t.Errorf("order = [%s %s], want [AutoscalerScaleUp FailedScheduling]", got[0].Reason, got[1].Reason)
	}
	if got[1].Count != 3 {
		t.Errorf("core-only event Count = %d, want 3", got[1].Count)
	}
}

// Events about other objects in the namespace must not leak into an Env's
// timeline.
func TestListEvents_FiltersForeignObjects(t *testing.T) {
	at := time.Date(2026, 9, 15, 8, 55, 0, 0, time.UTC)
	foreign := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "evt-x", Namespace: eventsTestNS},
		InvolvedObject: corev1.ObjectReference{
			Kind: "SandboxPool", Name: "someone-elses-pool", Namespace: eventsTestNS,
		},
		Reason:        "ScalingUp",
		Type:          corev1.EventTypeNormal,
		LastTimestamp: metav1.NewTime(at),
	}

	svc := newEventsTestService(t, foreign)
	got, appErr := svc.ListEvents(context.Background(), eventsTestNS, eventsTestEnv, 100)
	if appErr != nil {
		t.Fatalf("ListEvents: %v", appErr)
	}
	if len(got) != 0 {
		t.Fatalf("got %d events, want 0", len(got))
	}
}
