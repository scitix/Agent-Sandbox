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

package syncmgr

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

const (
	// templateWatchRetryMin / Max bound the backoff between watch attempts. The
	// upper bound is short because a stalled watch is invisible: templates keep
	// serving, they just stop propagating.
	templateWatchRetryMin = 2 * time.Second
	templateWatchRetryMax = 30 * time.Second
)

// WatchTemplateCRs mirrors SandboxTemplate changes on the hub's own API server
// into the per-cluster broadcast channels.
//
// Without it the only writer that reaches a worker is the internal HTTP API,
// because that is the one place broadcastTemplateUpsert is called. A template
// edited any other way — kubectl, a restore, another operator — stays on the
// hub until a worker happens to reconnect and pulls the snapshot, which can be
// hours and looks exactly like the edit having no effect.
//
// The API path still broadcasts. Keeping both is deliberate: that path is
// synchronous, so a caller's write is already on the wire when the response
// returns, while this is the net under every other writer. A worker applies an
// upsert with CreateOrUpdate, so the duplicate costs one message and changes
// nothing.
func (m *SyncManager) WatchTemplateCRs(ctx context.Context, w client.WithWatch) {
	if w == nil {
		return
	}
	backoff := templateWatchRetryMin
	for {
		if err := m.watchTemplatesOnce(ctx, w); err != nil {
			log.Printf("syncmgr/template-watch: %v (retrying in %s)", err, backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > templateWatchRetryMax {
			backoff = templateWatchRetryMax
		}
	}
}

// watchTemplatesOnce runs one watch until it ends, and returns the reason.
func (m *SyncManager) watchTemplatesOnce(ctx context.Context, w client.WithWatch) error {
	list := &agentsv1alpha1.SandboxTemplateList{}
	wi, err := w.Watch(ctx, list)
	if err != nil {
		return err
	}
	defer wi.Stop()

	// Resync before serving events. A watch established now reports only what
	// happens from now on, so anything edited while the previous one was down
	// would otherwise be skipped — and the gap is precisely when an edit is most
	// likely to be missed.
	m.resyncTemplates(ctx, w)

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-wi.ResultChan():
			if !ok {
				return errWatchClosed
			}
			m.handleTemplateEvent(ev)
		}
	}
}

// errWatchClosed marks the ordinary end of a watch (the API server closes them
// periodically), so the retry loop can log it as routine rather than as failure.
var errWatchClosed = watchClosedError{}

type watchClosedError struct{}

func (watchClosedError) Error() string { return "watch channel closed" }

func (m *SyncManager) resyncTemplates(ctx context.Context, w client.WithWatch) {
	list := &agentsv1alpha1.SandboxTemplateList{}
	if err := w.List(ctx, list); err != nil {
		log.Printf("syncmgr/template-watch: resync list failed: %v", err)
		return
	}
	for i := range list.Items {
		m.broadcastTemplateJSON(&list.Items[i])
	}
}

func (m *SyncManager) handleTemplateEvent(ev watch.Event) {
	tmpl, ok := ev.Object.(*agentsv1alpha1.SandboxTemplate)
	if !ok {
		// Error events carry a Status, not a template; the closed channel that
		// follows is what restarts the watch.
		return
	}
	switch ev.Type {
	case watch.Added, watch.Modified:
		m.broadcastTemplateJSON(tmpl)
	case watch.Deleted:
		m.broadcastTemplateDelete(tmpl.Name)
	}
}

func (m *SyncManager) broadcastTemplateJSON(tmpl *agentsv1alpha1.SandboxTemplate) {
	raw, err := json.Marshal(tmpl)
	if err != nil {
		log.Printf("syncmgr/template-watch: marshal %s: %v", tmpl.Name, err)
		return
	}
	m.broadcastTemplateUpsert(raw)
}
