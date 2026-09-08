/**
 * Copyright 2026 ScitiX
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// The assembly point.
//
// There is now exactly ONE host: every harness is an AgentBackend behind the
// gateway, so the browser speaks one wire (AG-UI) and mounts one runtime. What
// differs between backends is capability, not transport, and the UI reads that
// from `GET /capabilities` through the neutral port in `backend-port.tsx` —
// nothing here branches on a backend name any more.
//
// Kept as its own module because the enterprise build overrides it, and because
// a host that imports the thing that chooses it would be a cycle.
import { GatewayAssistantHost } from '@/components/assistant-ui/gw/gateway-host'
import type { AssistantHostProps } from '@/components/assistant-ui/runtime-provider'

export function AssistantHost(props: AssistantHostProps) {
  return <GatewayAssistantHost {...props} />
}

export type { AssistantHostProps }
