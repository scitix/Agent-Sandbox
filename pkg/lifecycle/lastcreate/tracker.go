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

// Package lastcreate hosts the in-process Sandbox.Create timestamp
// tracker that the Pool autoscaler's quiet-window gate consumes.
//
// Motivation: every Sandbox.Create request needs to bump "this Pool was
// recently asked for a sandbox" so the autoscaler can decide whether
// proactive idleZero scale-up is justified. Writing that timestamp to
// the SandboxPool annotation on every Create would put high-QPS write
// pressure on the K8s API server. The Tracker accumulates Bumps in
// memory and flushes at most once every `flushInterval` per Pool.
//
// Because cmd/sandbox runs the API server and the controller manager in
// one process (see CLAUDE.md §Three Binaries), the in-memory value is
// directly readable by the Pool autoscaler's Snapshot loader through
// the LastCreateTracker interface in
// pkg/controllers/sandboxpool/autoscalingstate/. The persisted
// annotation only matters across process restarts.
package lastcreate

import (
	"context"
	"fmt"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// defaultFlushInterval is the period at which the Tracker scans the
// dirty set and patches Pool annotations. Five seconds is a deliberate
// balance: short enough that the persisted mirror lags by at most one
// human-perceptible heartbeat (the autoscaler's reconcile cadence is
// ~30 s, so a 5 s lag is invisible there), long enough that 1000 active
// Pools produce 200 patches/sec at peak — well within K8s API rate
// limits.
const defaultFlushInterval = 5 * time.Second

// Tracker records the most recent Sandbox.Create timestamp per Pool and
// periodically flushes the dirty entries to the SandboxPool annotation
// (LastSandboxCreateTimeAnnotationKey).
//
// All methods are safe for concurrent use.
//
// Tracker is constructed with NewTracker and bound to a controller-runtime
// client; the flush loop is driven by Start, which satisfies the
// manager.Runnable interface so the controller manager owns its
// lifecycle.
type Tracker struct {
	client        client.Client
	flushInterval time.Duration

	mu      sync.Mutex
	perPool map[string]*entry

	// now is the clock injection point for tests. Production wires
	// time.Now via NewTracker's default.
	now func() time.Time
}

// entry holds one Pool's last-seen Create timestamp and the last
// successfully flushed value. lastSeen advances on every Bump; the
// background loop sets lastFlushed once a patch lands.
type entry struct {
	lastSeen    time.Time
	lastFlushed time.Time
}

// NewTracker constructs a Tracker bound to c. flushInterval <= 0 uses
// the package default (5 s). Both arguments may be omitted only in
// unit tests via NewTrackerForTest.
func NewTracker(c client.Client, flushInterval time.Duration) *Tracker {
	if flushInterval <= 0 {
		flushInterval = defaultFlushInterval
	}
	return &Tracker{
		client:        c,
		flushInterval: flushInterval,
		perPool:       make(map[string]*entry),
		now:           time.Now,
	}
}

// Bump records the current time as the most-recent Sandbox.Create for
// the named Pool. Safe to call from any goroutine on the request hot
// path; takes a brief mutex.
//
// Nil receiver is a no-op so the call site can skip nil checks when
// the tracker has not been wired (e.g. unit-test SandboxService).
func (t *Tracker) Bump(namespace, name string) {
	if t == nil || namespace == "" || name == "" {
		return
	}
	key := namespace + "/" + name
	now := t.now()
	t.mu.Lock()
	e := t.perPool[key]
	if e == nil {
		e = &entry{}
		t.perPool[key] = e
	}
	e.lastSeen = now
	t.mu.Unlock()
}

// Get returns the most recent observed Create time for the Pool and a
// bool indicating whether any Bump has been recorded. Satisfies the
// LastCreateTracker interface consumed by
// pkg/controllers/sandboxpool/autoscalingstate.
//
// Nil receiver returns (zero, false) — the autoscaler then falls back
// to the persisted annotation on the Pool object.
func (t *Tracker) Get(namespace, name string) (time.Time, bool) {
	if t == nil || namespace == "" || name == "" {
		return time.Time{}, false
	}
	key := namespace + "/" + name
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.perPool[key]
	if e == nil || e.lastSeen.IsZero() {
		return time.Time{}, false
	}
	return e.lastSeen, true
}

// Start drives the periodic flush loop until ctx is cancelled.
// Satisfies sigs.k8s.io/controller-runtime/pkg/manager.Runnable so the
// controller manager can own its lifecycle via mgr.Add(tracker).
//
// Start blocks the calling goroutine; pass the manager's root context
// (controller-runtime guarantees cancellation on shutdown).
func (t *Tracker) Start(ctx context.Context) error {
	if t == nil {
		return fmt.Errorf("lastcreate: nil Tracker")
	}
	if t.client == nil {
		return fmt.Errorf("lastcreate: Tracker has no client; cannot flush")
	}
	ticker := time.NewTicker(t.flushInterval)
	defer ticker.Stop()
	klog.V(2).InfoS("LastCreateTracker starting", "flushInterval", t.flushInterval)
	for {
		select {
		case <-ctx.Done():
			klog.V(2).InfoS("LastCreateTracker stopped", "reason", ctx.Err())
			return nil
		case <-ticker.C:
			t.flushOnce(ctx)
		}
	}
}

// flushOnce scans the dirty set and patches each Pool's annotation in
// turn. Sequential by design — flushing 1000 Pools serially at a 5 s
// cadence comfortably fits the budget, and bounded sequential I/O is
// easier to reason about under back-pressure than a goroutine fanout.
func (t *Tracker) flushOnce(ctx context.Context) {
	type job struct {
		namespace string
		name      string
		key       string
		timestamp time.Time
	}

	t.mu.Lock()
	jobs := make([]job, 0)
	for k, e := range t.perPool {
		if !e.lastSeen.After(e.lastFlushed) {
			continue
		}
		ns, name, ok := splitKey(k)
		if !ok {
			continue
		}
		jobs = append(jobs, job{namespace: ns, name: name, key: k, timestamp: e.lastSeen})
	}
	t.mu.Unlock()

	for _, j := range jobs {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := patchAnnotation(ctx, t.client, j.namespace, j.name, j.timestamp); err != nil {
			if apierrors.IsNotFound(err) {
				// Pool was deleted; drop the entry so the map doesn't
				// accumulate dead keys.
				t.mu.Lock()
				delete(t.perPool, j.key)
				t.mu.Unlock()
				continue
			}
			klog.V(2).InfoS("LastCreateTracker flush failed (will retry next tick)",
				"pool", j.key, "err", err)
			continue
		}
		t.mu.Lock()
		if e := t.perPool[j.key]; e != nil && j.timestamp.After(e.lastFlushed) {
			e.lastFlushed = j.timestamp
		}
		t.mu.Unlock()
	}
}

// patchAnnotation writes the timestamp annotation onto the SandboxPool,
// using RFC3339 UTC. Skips the patch when the persisted value is
// already >= the supplied timestamp (another writer beat us, or this
// is a no-op retry).
func patchAnnotation(ctx context.Context, c client.Client, namespace, name string, ts time.Time) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pool := &agentsv1alpha1.SandboxPool{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pool); err != nil {
			return err
		}
		desired := ts.UTC().Format(time.RFC3339)
		if existing := pool.Annotations[agentsv1alpha1.LastSandboxCreateTimeAnnotationKey]; existing != "" {
			if t, err := time.Parse(time.RFC3339, existing); err == nil && !ts.After(t) {
				return nil // already current
			}
		}
		base := pool.DeepCopy()
		if pool.Annotations == nil {
			pool.Annotations = map[string]string{}
		}
		pool.Annotations[agentsv1alpha1.LastSandboxCreateTimeAnnotationKey] = desired
		return c.Patch(ctx, pool, client.MergeFrom(base))
	})
}

// splitKey splits "namespace/name" into its parts. Returns ok=false
// when the key is malformed — should not happen in production since
// Bump validates inputs, but we still defend the flush path.
func splitKey(key string) (namespace, name string, ok bool) {
	for i, c := range key {
		if c == '/' {
			return key[:i], key[i+1:], i > 0 && i < len(key)-1
		}
	}
	return "", "", false
}
