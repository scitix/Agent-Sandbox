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
	"fmt"
	"log"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/api/protocol"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
)

// sendTemplateSnapshot sends all existing SandboxTemplates as a template_snapshot frame.
// Before sending, it attempts to migrate any legacy spec.reservation data to annotations.
func (m *SyncManager) sendTemplateSnapshot(ctx context.Context, sc *clusterSyncConn) error {
	if m.deps.TemplateClient == nil {
		return nil
	}
	list := &agentsv1alpha1.SandboxTemplateList{}
	if err := m.deps.TemplateClient.List(ctx, list); err != nil {
		return fmt.Errorf("list sandbox templates: %w", err)
	}
	items := make([]protocol.Frame, 0, len(list.Items))
	for i := range list.Items {
		tmpl := &list.Items[i]
		f, err := templateToFrame(tmpl)
		if err != nil {
			log.Printf("syncManager: skip template %s in snapshot: %v", tmpl.Name, err)
			continue
		}
		items = append(items, f)
	}
	return sc.send(protocol.Frame{Type: protocol.FrameTemplateSnapshot, Items: items})
}

// handleTemplateCreate creates a SandboxTemplate on master and broadcasts to all Workers.
func (m *SyncManager) handleTemplateCreate(ctx context.Context, sc *clusterSyncConn, frame protocol.Frame) {
	if m.deps.TemplateClient == nil {
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameTemplateCreateResp, OK: false,
			Error: "template sync not configured", HTTPStatus: 503})
		return
	}

	tmpl, err := frameToSandboxTemplate(frame)
	if err != nil {
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameTemplateCreateResp, OK: false,
			Error: err.Error(), HTTPStatus: 400})
		return
	}

	if createErr := m.deps.TemplateClient.Create(ctx, tmpl); createErr != nil {
		log.Printf("syncManager: create template %s error: %v", tmpl.Name, createErr)
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameTemplateCreateResp, OK: false,
			Error: "failed to create template", HTTPStatus: 500})
		return
	}

	sf, fErr := templateToFrame(tmpl)
	if fErr == nil {
		sf.Type = protocol.FrameTemplateSync
		m.broadcast(sf)
	}

	_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameTemplateCreateResp, OK: true,
		Name: tmpl.Name})
}

// handleTemplateUpdate updates a SandboxTemplate on master and broadcasts to all Workers.
func (m *SyncManager) handleTemplateUpdate(ctx context.Context, sc *clusterSyncConn, frame protocol.Frame) {
	if m.deps.TemplateClient == nil {
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameTemplateUpdateResp, OK: false,
			Error: "template sync not configured", HTTPStatus: 503})
		return
	}

	desired, err := frameToSandboxTemplate(frame)
	if err != nil {
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameTemplateUpdateResp, OK: false,
			Error: err.Error(), HTTPStatus: 400})
		return
	}

	existing := &agentsv1alpha1.SandboxTemplate{}
	if getErr := m.deps.TemplateClient.Get(ctx, client.ObjectKey{Name: desired.Name}, existing); getErr != nil {
		log.Printf("syncManager: get template %s error: %v", desired.Name, getErr)
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameTemplateUpdateResp, OK: false,
			Error: "template not found", HTTPStatus: 404})
		return
	}

	updated := existing.DeepCopy()
	updated.Spec = desired.Spec
	updated.Labels = desired.Labels
	updated.Annotations = desired.Annotations
	if updated.Labels == nil {
		updated.Labels = make(map[string]string)
	}
	updated.Labels["agentbox.io/sync-source"] = agentsv1alpha1.LabelSyncSourceGlobal

	if patchErr := m.deps.TemplateClient.Update(ctx, updated); patchErr != nil {
		log.Printf("syncManager: update template %s error: %v", desired.Name, patchErr)
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameTemplateUpdateResp, OK: false,
			Error: "failed to update template", HTTPStatus: 500})
		return
	}

	sf, fErr := templateToFrame(updated)
	if fErr == nil {
		sf.Type = protocol.FrameTemplateSync
		m.broadcast(sf)
	}

	_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameTemplateUpdateResp, OK: true,
		Name: updated.Name})
}

// handleTemplateDelete deletes a SandboxTemplate on master and broadcasts to all Workers.
func (m *SyncManager) handleTemplateDelete(ctx context.Context, sc *clusterSyncConn, frame protocol.Frame) {
	if m.deps.TemplateClient == nil {
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameTemplateDeleteResp, OK: false,
			Error: "template sync not configured", HTTPStatus: 503})
		return
	}
	if frame.Name == "" {
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameTemplateDeleteResp, OK: false,
			Error: "name is required", HTTPStatus: 400})
		return
	}

	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if getErr := m.deps.TemplateClient.Get(ctx, client.ObjectKey{Name: frame.Name}, tmpl); getErr != nil {
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameTemplateDeleteResp, OK: false,
			Error: "template not found", HTTPStatus: 404})
		return
	}
	if deleteErr := m.deps.TemplateClient.Delete(ctx, tmpl); deleteErr != nil {
		log.Printf("syncManager: delete template %s error: %v", frame.Name, deleteErr)
		_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameTemplateDeleteResp, OK: false,
			Error: "failed to delete template", HTTPStatus: 500})
		return
	}

	m.broadcast(protocol.Frame{Type: protocol.FrameTemplateDeleteSync, Name: frame.Name})
	_ = sc.send(protocol.Frame{ID: frame.ID, Type: protocol.FrameTemplateDeleteResp, OK: true,
		Name: frame.Name})
}

// templateToFrame serialises a SandboxTemplate into a protocol.Frame.
func templateToFrame(tmpl *agentsv1alpha1.SandboxTemplate) (protocol.Frame, error) {
	fullRaw, err := json.Marshal(tmpl)
	if err != nil {
		return protocol.Frame{}, fmt.Errorf("marshal full SandboxTemplate for %s: %w", tmpl.Name, err)
	}
	return protocol.Frame{
		Type:         protocol.FrameTemplateSync,
		TemplateFull: fullRaw,
	}, nil
}

// templateDomainToFrame constructs a broadcast protocol.Frame from a domain.SandboxTemplate.
// It YAML→JSON converts CrdYaml so that Workers can unmarshal it as a full CRD object.
func templateDomainToFrame(dt *domain.SandboxTemplate) (protocol.Frame, error) {
	if dt.CrdYaml == "" {
		return protocol.Frame{}, fmt.Errorf("CrdYaml is empty for template %q", dt.Name)
	}
	var obj any
	if err := yaml.Unmarshal([]byte(dt.CrdYaml), &obj); err != nil {
		return protocol.Frame{}, fmt.Errorf("unmarshal CrdYaml for %q: %w", dt.Name, err)
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return protocol.Frame{}, fmt.Errorf("marshal CrdYaml JSON for %q: %w", dt.Name, err)
	}
	return protocol.Frame{Type: protocol.FrameTemplateSync, TemplateFull: raw}, nil
}

// frameToSandboxTemplate deserialises a Frame into a SandboxTemplate ready for K8s ops.
func frameToSandboxTemplate(frame protocol.Frame) (*agentsv1alpha1.SandboxTemplate, error) {
	if len(frame.TemplateFull) == 0 {
		return nil, fmt.Errorf("templateFull is required")
	}
	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if err := json.Unmarshal(frame.TemplateFull, tmpl); err != nil {
		return nil, fmt.Errorf("unmarshal SandboxTemplate: %w", err)
	}
	if tmpl.Labels == nil {
		tmpl.Labels = make(map[string]string)
	}
	tmpl.Labels["agentbox.io/sync-source"] = agentsv1alpha1.LabelSyncSourceGlobal
	return tmpl, nil
}
