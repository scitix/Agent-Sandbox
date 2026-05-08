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

package extproc

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// ActivityTracker is a lightweight in-memory store that records the last time
// a proxied HTTP request was observed for each sandbox. It is intentionally
// stateless with respect to Kubernetes — all K8s reads/writes are handled by
// IdleTimeoutReconciler in the agentbox controller manager.
//
// Lifecycle:
//  1. On startup, InitFromAnnotations is called for each Running pod that has a
//     last-active annotation so that ExtProc restart does not reset history.
//  2. On every proxied request, Touch is called (non-blocking, O(1)).
//  3. IdleTimeoutReconciler polls LastActiveHandler to snapshot the map, writes
//     the values back as pod annotations, and decides whether to release pods.
type ActivityTracker struct {
	mu         sync.RWMutex
	lastActive map[string]time.Time // sandboxID → last seen

	// client is injected for GC; nil when GC is disabled.
	client     client.Client
	gcInterval time.Duration
}

// NewActivityTracker creates an ActivityTracker with an empty state and no
// background GC. This is the zero-config constructor.
func NewActivityTracker() *ActivityTracker {
	return &ActivityTracker{
		lastActive: make(map[string]time.Time),
	}
}

// NewActivityTrackerWithGC creates an ActivityTracker with a background GC
// goroutine that periodically removes entries for sandboxes that are no longer
// in the Running state. gcInterval controls the GC frequency; 5–10 minutes is
// recommended for production use.
func NewActivityTrackerWithGC(c client.Client, gcInterval time.Duration) *ActivityTracker {
	return &ActivityTracker{
		lastActive: make(map[string]time.Time),
		client:     c,
		gcInterval: gcInterval,
	}
}

// StartGC launches the background GC goroutine. It is a no-op when no client
// or gcInterval was provided. Call this only after the K8s cache is warm so
// that the first GC cycle does not spuriously evict entries.
func (t *ActivityTracker) StartGC(ctx context.Context) {
	if t.client == nil || t.gcInterval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(t.gcInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.gc(ctx)
			}
		}
	}()
}

// gc removes entries from the lastActive map for sandboxes that no longer have
// a Running pod. It is called periodically by the GC goroutine.
func (t *ActivityTracker) gc(ctx context.Context) {
	snap := t.snapshot()
	if len(snap) == 0 {
		return
	}

	podList := &corev1.PodList{}
	if err := t.client.List(ctx, podList,
		client.MatchingFields{indexer.IndexFieldSandboxPhase: agentsv1alpha1.SandboxPhaseRunning},
	); err != nil {
		klog.ErrorS(err, "ActivityTracker GC: failed to list Running pods")
		return
	}

	runningSet := make(map[string]bool, len(podList.Items))
	for i := range podList.Items {
		if id := podList.Items[i].Labels[agentsv1alpha1.SandboxIDLabelKey]; id != "" {
			runningSet[id] = true
		}
	}

	removed := 0
	for sandboxID := range snap {
		if !runningSet[sandboxID] {
			t.Remove(sandboxID)
			klog.V(4).InfoS("ActivityTracker GC: removed stale entry for sandbox", "sandboxID", sandboxID)
			removed++
		}
	}
	klog.V(4).InfoS("ActivityTracker GC done", "checked", len(snap), "removed", removed)
}

// InitFromAnnotations seeds the in-memory map with a timestamp from a pod
// annotation. Call this once per Running pod at startup before serving traffic.
// If ts is the zero value it is ignored.
func (t *ActivityTracker) InitFromAnnotations(sandboxID string, ts time.Time) {
	if sandboxID == "" || ts.IsZero() {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// Only seed if we don't already have a newer value.
	if existing, ok := t.lastActive[sandboxID]; !ok || ts.After(existing) {
		t.lastActive[sandboxID] = ts
	}
}

// Touch records the current time as the last-active timestamp for sandboxID.
// It is safe to call from multiple goroutines and returns immediately.
func (t *ActivityTracker) Touch(sandboxID string) {
	if sandboxID == "" {
		return
	}
	now := time.Now().UTC()
	t.mu.Lock()
	t.lastActive[sandboxID] = now
	t.mu.Unlock()
}

// Remove deletes a sandbox entry from the tracker. Called when a pod is no
// longer Running so its entry does not accumulate indefinitely.
func (t *ActivityTracker) Remove(sandboxID string) {
	if sandboxID == "" {
		return
	}
	t.mu.Lock()
	delete(t.lastActive, sandboxID)
	t.mu.Unlock()
}

// snapshot returns a copy of the current map under a read lock.
func (t *ActivityTracker) snapshot() map[string]time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]time.Time, len(t.lastActive))
	maps.Copy(out, t.lastActive)
	return out
}

// LastActiveHandler returns an HTTP handler that serves a JSON object mapping
// sandboxID → RFC3339 timestamp for the IdleTimeoutReconciler to poll.
//
// Response format:
//
//	{ "sandboxId1": "2026-03-17T10:00:00Z", ... }
func (t *ActivityTracker) LastActiveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snap := t.snapshot()
		out := make(map[string]string, len(snap))
		for id, ts := range snap {
			out[id] = ts.UTC().Format(time.RFC3339)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(out); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	}
}
