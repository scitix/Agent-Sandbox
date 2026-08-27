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

// Gateway entrypoint. It is always the pod's public entry and health authority.
// Every backend goes through the same path now — OpenCode's transparent proxy is
// gone, so there is exactly one wire contract and the browser needs one runtime.
//
// Startup order matters: preflight BEFORE listening, so a harness's dialect or
// credential problem is known by the time the first request arrives rather than
// discovered inside a turn.
//
// It does NOT gate binding. Every harness failing is a configuration error, and
// the way to surface one is to answer with it — the picker shows each harness
// greyed out with its reason, and the pod's other services (workspace-fs, the
// workspace-fs server) keeps serving, and it is not something a
// model credential should be able to take down. See registry.ts.
import { startBackends } from './registry.ts'
import { listenGateway } from './server.ts'

async function main(): Promise<void> {
  // Every configured harness is built and verified; the pod serves all of them
  // so a tester can switch from the UI. A backend that fails preflight is
  // reported unavailable, not fatal — see registry.ts.
  const deps = await startBackends()
  console.error(
    deps.backends.size
      ? `[gateway] serving ${[...deps.backends.keys()].join(', ')} ` +
          `(default ${deps.defaultBackendId})`
      : '[gateway] serving no harness; the API is up and reports why'
  )

  // deps carries the classifier and the Langfuse target as well as the backend,
  // so both are wired by construction rather than by a caller remembering to.
  const server = await listenGateway(deps)
  const addr = server.address()
  const port = typeof addr === 'object' && addr ? addr.port : '?'
  console.error(`[gateway] listening on 0.0.0.0:${port}`)

  const shutdown = (signal: string) => {
    console.error(`[gateway] ${signal} received, closing`)
    server.close(() => process.exit(0))
    // Do not wait forever on a long-lived SSE stream.
    setTimeout(() => process.exit(0), 5_000).unref?.()
  }
  process.on('SIGTERM', () => shutdown('SIGTERM'))
  process.on('SIGINT', () => shutdown('SIGINT'))
}

main().catch(e => {
  // Reserved for the gateway itself failing to come up (a bad
  // ASSISTANT_BACKEND value, a port already taken). A harness that cannot serve
  // never lands here — it is reported through /backends instead.
  console.error(
    `[gateway] refusing to start: ${e instanceof Error ? e.message : e}`
  )
  process.exit(1)
})
