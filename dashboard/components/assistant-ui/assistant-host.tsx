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
