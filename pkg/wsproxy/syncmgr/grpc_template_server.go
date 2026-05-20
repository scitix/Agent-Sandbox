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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	syncv1 "github.com/scitix/agent-sandbox/pkg/proto/sandbox/sync/v1"
)

// templateServer implements the Hub side of TemplateService for one Worker.
//
// Templates travel as JSON bytes; this keeps the proto schema independent of
// the K8s API surface. The Worker side json.Unmarshal()s straight into
// *v1alpha1.SandboxTemplate, exactly mirroring how it reads from K8s.
type templateServer struct {
	syncv1.UnimplementedTemplateServiceServer
	m  *SyncManager
	sc *clusterSyncConn
}

func newTemplateServer(m *SyncManager, sc *clusterSyncConn) *templateServer {
	return &templateServer{m: m, sc: sc}
}

func (s *templateServer) CreateTemplate(ctx context.Context, req *syncv1.CreateTemplateRequest) (*syncv1.CreateTemplateResponse, error) {
	if s.m.deps.TemplateClient == nil {
		return nil, status.Error(codes.Unavailable, "template sync not configured")
	}
	tmpl, err := decodeTemplate(req.TemplateJson)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode template: %v", err)
	}
	if createErr := s.m.deps.TemplateClient.Create(ctx, tmpl); createErr != nil {
		log.Printf("syncmgr/grpc: CreateTemplate %s error: %v", tmpl.Name, createErr)
		return nil, status.Errorf(codes.Internal, "failed to create template: %v", createErr)
	}
	if raw, mErr := json.Marshal(tmpl); mErr == nil {
		s.m.broadcastTemplateUpsert(raw)
	}
	return &syncv1.CreateTemplateResponse{Name: tmpl.Name}, nil
}

func (s *templateServer) UpdateTemplate(ctx context.Context, req *syncv1.UpdateTemplateRequest) (*syncv1.UpdateTemplateResponse, error) {
	if s.m.deps.TemplateClient == nil {
		return nil, status.Error(codes.Unavailable, "template sync not configured")
	}
	desired, err := decodeTemplate(req.TemplateJson)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode template: %v", err)
	}
	existing := &agentsv1alpha1.SandboxTemplate{}
	if getErr := s.m.deps.TemplateClient.Get(ctx, client.ObjectKey{Name: desired.Name}, existing); getErr != nil {
		return nil, status.Errorf(codes.NotFound, "template not found: %v", getErr)
	}
	updated := existing.DeepCopy()
	updated.Spec = desired.Spec
	updated.Labels = desired.Labels
	updated.Annotations = desired.Annotations
	if updated.Labels == nil {
		updated.Labels = make(map[string]string)
	}
	updated.Labels["agentbox.io/sync-source"] = agentsv1alpha1.LabelSyncSourceGlobal
	if patchErr := s.m.deps.TemplateClient.Update(ctx, updated); patchErr != nil {
		log.Printf("syncmgr/grpc: UpdateTemplate %s error: %v", desired.Name, patchErr)
		return nil, status.Errorf(codes.Internal, "failed to update template: %v", patchErr)
	}
	if raw, mErr := json.Marshal(updated); mErr == nil {
		s.m.broadcastTemplateUpsert(raw)
	}
	return &syncv1.UpdateTemplateResponse{Name: updated.Name}, nil
}

func (s *templateServer) DeleteTemplate(ctx context.Context, req *syncv1.DeleteTemplateRequest) (*syncv1.DeleteTemplateResponse, error) {
	if s.m.deps.TemplateClient == nil {
		return nil, status.Error(codes.Unavailable, "template sync not configured")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if getErr := s.m.deps.TemplateClient.Get(ctx, client.ObjectKey{Name: req.Name}, tmpl); getErr != nil {
		return nil, status.Errorf(codes.NotFound, "template not found: %v", getErr)
	}
	if delErr := s.m.deps.TemplateClient.Delete(ctx, tmpl); delErr != nil {
		log.Printf("syncmgr/grpc: DeleteTemplate %s error: %v", req.Name, delErr)
		return nil, status.Errorf(codes.Internal, "failed to delete template: %v", delErr)
	}
	s.m.broadcastTemplateDelete(req.Name)
	return &syncv1.DeleteTemplateResponse{Name: req.Name}, nil
}

func (s *templateServer) WatchTemplates(_ *syncv1.WatchTemplatesRequest, stream syncv1.TemplateService_WatchTemplatesServer) error {
	ctx := stream.Context()

	// Initial snapshot.
	if s.m.deps.TemplateClient != nil {
		list := &agentsv1alpha1.SandboxTemplateList{}
		if err := s.m.deps.TemplateClient.List(ctx, list); err != nil {
			return status.Errorf(codes.Internal, "list templates: %v", err)
		}
		snap := &syncv1.TemplateSnapshot{TemplateJsons: make([][]byte, 0, len(list.Items))}
		for i := range list.Items {
			raw, err := json.Marshal(&list.Items[i])
			if err != nil {
				log.Printf("syncmgr/grpc: skip template %s in snapshot: %v", list.Items[i].Name, err)
				continue
			}
			snap.TemplateJsons = append(snap.TemplateJsons, raw)
		}
		if err := stream.Send(&syncv1.TemplateEvent{Kind: &syncv1.TemplateEvent_Snapshot{Snapshot: snap}}); err != nil {
			return err
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-s.sc.tmplCh:
			if !ok {
				return nil
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

// decodeTemplate unmarshals a JSON-encoded SandboxTemplate and applies the
// "sync-source=global" label that flags it as authoritative on Worker.
func decodeTemplate(raw []byte) (*agentsv1alpha1.SandboxTemplate, error) {
	if len(raw) == 0 {
		return nil, errEmptyTemplate
	}
	tmpl := &agentsv1alpha1.SandboxTemplate{}
	if err := json.Unmarshal(raw, tmpl); err != nil {
		return nil, err
	}
	if tmpl.Labels == nil {
		tmpl.Labels = make(map[string]string)
	}
	tmpl.Labels["agentbox.io/sync-source"] = agentsv1alpha1.LabelSyncSourceGlobal
	return tmpl, nil
}

// errEmptyTemplate is returned when CreateTemplate / UpdateTemplate is called
// with an empty body; surfaced as InvalidArgument upstream.
var errEmptyTemplate = errEmptyTemplateValue{}

type errEmptyTemplateValue struct{}

func (errEmptyTemplateValue) Error() string { return "template_json is required" }
