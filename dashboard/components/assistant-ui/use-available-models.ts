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

import { useAssistantBackendId } from '@/components/assistant-ui/backend-port'
import { gateway } from '@/components/assistant-ui/gw/client'
import type { AssistantModelRef } from '@/lib/assistant/store'
import { useQuery } from '@tanstack/react-query'

/** One selectable entry in the model dropdown. `modelID` is what the run sends
 *  back to the gateway; `label` is the human-readable name. */
export interface ModelOption extends AssistantModelRef {
  label: string
}

export interface AvailableModels {
  options: ModelOption[]
  /** The first model the backend reports — the initial pick when the user has
   *  not chosen one. `undefined` when the backend reports none. */
  serverDefault: AssistantModelRef | undefined
  isLoading: boolean
}

/**
 * The models this deployment actually has, from the gateway's `/models`.
 *
 * There used to be a second path here that reached into OpenCode's own config
 * API, because OpenCode was not behind the gateway. It is now, and every backend
 * answers `/models` — OpenCode by enumerating its providers, Claude Code from
 * deployment config, because the SDK's bundled table does not reflect a
 * third-party endpoint. The browser does not need to know which of those
 * happened, which is why the gateway no longer reports it as a capability: the
 * one thing this file does with the backend id is scope the cache.
 *
 * The provider dimension is gone with it: a model is one opaque id, and for
 * OpenCode the backend composes it as `providerID/modelID` and splits it again
 * on the way in. The set is static per deployment, so it is cached indefinitely.
 */
export function useAvailableModels(): AvailableModels {
  const backendId = useAssistantBackendId()

  const { data, isLoading } = useQuery({
    queryKey: ['assistant-models', backendId],
    staleTime: Infinity,
    gcTime: Infinity,
    retry: 1,
    queryFn: async (): Promise<AvailableModels> => {
      // Scoped to the harness running the open conversation: the two do not
      // offer the same models, and listing the other one's would produce a
      // dropdown whose picks the backend rejects.
      const models = await gateway.models(backendId)
      return {
        options: models.map(m => ({
          providerID: 'gateway',
          modelID: m.id,
          label: m.name || m.id,
        })),
        serverDefault: models[0]
          ? { providerID: 'gateway', modelID: models[0].id }
          : undefined,
        isLoading: false,
      }
    },
  })

  return data ?? { options: [], serverDefault: undefined, isLoading }
}
