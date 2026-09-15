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
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	gen "github.com/scitix/agent-sandbox/pkg/apiserver/gen"
)

const defaultEventListLimit = 100

// ListEvents fans out across the corev1 and eventsv1 APIs because the
// controllers in this repo emit through k8s.io/client-go/tools/events
// (events.k8s.io/v1), while kubectl-style status events (e.g. pod failures)
// still land in corev1. Merging both lets the timeline surface every
// signal the operator would expect to see, regardless of which API path
// the emitter took.
//
// The two APIs are two *views* of one set of stored objects, not two sets:
// an event written through events.k8s.io/v1 is readable through core/v1 as
// well. A plain merge therefore returns every controller event twice — once
// richly (with Action and the Series count) and once flattened (no Action,
// Count 0, no FirstTimestamp), which reads as a duplicated timeline rather
// than as one event seen two ways. Results are keyed by the Event object's
// own name, with the events.k8s.io view winning: it is the same record with
// more of its fields populated. Events that exist only in core/v1 carry
// distinct names and survive untouched.
func (s *k8sSandboxEnvService) ListEvents(ctx context.Context, namespace, name string, limit int) ([]gen.EnvEvent, *domain.AppError) {
	if limit <= 0 || limit > 500 {
		limit = defaultEventListLimit
	}

	env := &agentsv1alpha1.SandboxEnv{}
	if err := s.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, env); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, domain.NewNotFound(fmt.Sprintf("sandbox env %s/%s not found", namespace, name))
		}
		return nil, domain.NewInternal(fmt.Sprintf("get env: %v", err), err)
	}

	// Collect the set of member Pool names so we can filter Events by
	// involvedObject.name. Use LabelEnv rather than walking Status because the
	// label is the authoritative index and survives transient status drift.
	memberPools := map[string]struct{}{}
	poolList := &agentsv1alpha1.SandboxPoolList{}
	if err := s.client.List(ctx, poolList,
		client.InNamespace(namespace),
		client.MatchingLabels{agentsv1alpha1.LabelEnv: name},
	); err != nil {
		return nil, domain.NewInternal(fmt.Sprintf("list member pools: %v", err), err)
	}
	for i := range poolList.Items {
		memberPools[poolList.Items[i].Name] = struct{}{}
	}

	matches := func(kind, n string) bool {
		switch kind {
		case agentsv1alpha1.SandboxEnvOwnerKind:
			return n == name
		case "SandboxPool":
			_, ok := memberPools[n]
			return ok
		}
		return false
	}

	// Keyed by the Event object's own metadata.name so the two API views of
	// one stored object collapse into a single entry. Insertion order is kept
	// separately because the final sort is by timestamp and Go map iteration
	// would otherwise reshuffle same-timestamp events between calls.
	byName := make(map[string]gen.EnvEvent, limit)
	var order []string
	put := func(name string, item gen.EnvEvent) {
		if _, seen := byName[name]; !seen {
			order = append(order, name)
		}
		byName[name] = item
	}

	// Legacy corev1 events. Some controllers (and built-in kubelet/scheduler
	// signals) still write here; ignore List errors so a missing API or RBAC
	// hole on one source doesn't blank out the timeline.
	cevs := &corev1.EventList{}
	if err := s.client.List(ctx, cevs, client.InNamespace(namespace)); err == nil {
		for i := range cevs.Items {
			ev := &cevs.Items[i]
			if !matches(ev.InvolvedObject.Kind, ev.InvolvedObject.Name) {
				continue
			}
			item := gen.EnvEvent{
				InvolvedKind: ev.InvolvedObject.Kind,
				InvolvedName: ev.InvolvedObject.Name,
				Reason:       ev.Reason,
				Message:      ev.Message,
				Type:         ev.Type,
				Count:        int(ev.Count),
			}
			if !ev.FirstTimestamp.IsZero() {
				t := ev.FirstTimestamp.Time
				item.FirstTimestamp = &t
			}
			if !ev.LastTimestamp.IsZero() {
				t := ev.LastTimestamp.Time
				item.LastTimestamp = &t
			} else if !ev.EventTime.IsZero() {
				t := ev.EventTime.Time
				item.LastTimestamp = &t
			}
			put(ev.Name, item)
		}
	}

	// Modern events.k8s.io/v1 events — what k8s.io/client-go/tools/events
	// recorders write to. `Regarding` carries the involved object reference
	// and `Note` carries the message; the controllers here also set Action.
	eevs := &eventsv1.EventList{}
	if err := s.client.List(ctx, eevs, client.InNamespace(namespace)); err == nil {
		for i := range eevs.Items {
			ev := &eevs.Items[i]
			if !matches(ev.Regarding.Kind, ev.Regarding.Name) {
				continue
			}
			item := gen.EnvEvent{
				InvolvedKind: ev.Regarding.Kind,
				InvolvedName: ev.Regarding.Name,
				Reason:       ev.Reason,
				Message:      ev.Note,
				Type:         ev.Type,
				Count:        1,
			}
			if ev.Action != "" {
				a := ev.Action
				item.Action = &a
			}
			if ev.Series != nil {
				item.Count = int(ev.Series.Count)
				t := ev.Series.LastObservedTime.Time
				item.LastTimestamp = &t
			} else if !ev.EventTime.IsZero() {
				t := ev.EventTime.Time
				item.LastTimestamp = &t
			} else if !ev.DeprecatedLastTimestamp.IsZero() {
				t := ev.DeprecatedLastTimestamp.Time
				item.LastTimestamp = &t
			}
			if !ev.DeprecatedFirstTimestamp.IsZero() {
				t := ev.DeprecatedFirstTimestamp.Time
				item.FirstTimestamp = &t
			} else if !ev.EventTime.IsZero() {
				t := ev.EventTime.Time
				item.FirstTimestamp = &t
			}
			if ev.DeprecatedCount > 0 {
				item.Count = int(ev.DeprecatedCount)
			}
			// Overwrites the core/v1 view of the same object deliberately:
			// same record, more fields populated.
			put(ev.Name, item)
		}
	}

	out := make([]gen.EnvEvent, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}

	// Newest first; events with no timestamp sort last.
	sort.SliceStable(out, func(i, j int) bool {
		var ti, tj int64
		if out[i].LastTimestamp != nil {
			ti = out[i].LastTimestamp.UnixNano()
		}
		if out[j].LastTimestamp != nil {
			tj = out[j].LastTimestamp.UnixNano()
		}
		return ti > tj
	})

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
