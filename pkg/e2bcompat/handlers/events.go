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

package handlers

import (
	"context"

	e2bgen "github.com/scitix/agent-sandbox/pkg/e2bcompat/gen"
)

// Sandbox lifecycle events and their webhooks. AgentBox keeps no event store
// to serve them from; sandbox state is observable by listing.

func (s *Server) GetEventsSandboxes(_ context.Context, _ e2bgen.GetEventsSandboxesRequestObject) (e2bgen.GetEventsSandboxesResponseObject, error) {
	return unsupportedOp("GetEventsSandboxes", catUnimplemented, msgEvents), nil
}

func (s *Server) GetEventsSandboxesSandboxID(_ context.Context, _ e2bgen.GetEventsSandboxesSandboxIDRequestObject) (e2bgen.GetEventsSandboxesSandboxIDResponseObject, error) {
	return unsupportedOp("GetEventsSandboxesSandboxID", catUnimplemented, msgEvents), nil
}

func (s *Server) GetEventsWebhooks(_ context.Context, _ e2bgen.GetEventsWebhooksRequestObject) (e2bgen.GetEventsWebhooksResponseObject, error) {
	return unsupportedOp("GetEventsWebhooks", catUnimplemented, msgEvents), nil
}

func (s *Server) PostEventsWebhooks(_ context.Context, _ e2bgen.PostEventsWebhooksRequestObject) (e2bgen.PostEventsWebhooksResponseObject, error) {
	return unsupportedOp("PostEventsWebhooks", catUnimplemented, msgEvents), nil
}

func (s *Server) GetEventsWebhooksWebhookID(_ context.Context, _ e2bgen.GetEventsWebhooksWebhookIDRequestObject) (e2bgen.GetEventsWebhooksWebhookIDResponseObject, error) {
	return unsupportedOp("GetEventsWebhooksWebhookID", catUnimplemented, msgEvents), nil
}

func (s *Server) PatchEventsWebhooksWebhookID(_ context.Context, _ e2bgen.PatchEventsWebhooksWebhookIDRequestObject) (e2bgen.PatchEventsWebhooksWebhookIDResponseObject, error) {
	return unsupportedOp("PatchEventsWebhooksWebhookID", catUnimplemented, msgEvents), nil
}

func (s *Server) DeleteEventsWebhooksWebhookID(_ context.Context, _ e2bgen.DeleteEventsWebhooksWebhookIDRequestObject) (e2bgen.DeleteEventsWebhooksWebhookIDResponseObject, error) {
	return unsupportedOp("DeleteEventsWebhooksWebhookID", catUnimplemented, msgEvents), nil
}

func (s *Server) GetEventsWebhooksWebhookIDDeliveries(_ context.Context, _ e2bgen.GetEventsWebhooksWebhookIDDeliveriesRequestObject) (e2bgen.GetEventsWebhooksWebhookIDDeliveriesResponseObject, error) {
	return unsupportedOp("GetEventsWebhooksWebhookIDDeliveries", catUnimplemented, msgEvents), nil
}

func (s *Server) GetEventsWebhooksWebhookIDStats(_ context.Context, _ e2bgen.GetEventsWebhooksWebhookIDStatsRequestObject) (e2bgen.GetEventsWebhooksWebhookIDStatsResponseObject, error) {
	return unsupportedOp("GetEventsWebhooksWebhookIDStats", catUnimplemented, msgEvents), nil
}
