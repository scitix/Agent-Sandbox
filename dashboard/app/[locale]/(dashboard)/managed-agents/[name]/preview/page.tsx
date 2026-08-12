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

"use client"

import { use } from "react"
import { useQuery } from "@tanstack/react-query"

import { AgentPreviewPanel } from "@/components/managed-agents/preview-panel"
import { managedAgentQueryOptions } from "@/lib/queries"

/**
 * Talk to this agent from the platform that deployed it.
 *
 * Readiness is read from the agent's own status rather than discovered by failing:
 * a Brain that is still pulling its image would otherwise answer every call with a
 * connection error, which reads as a broken feature rather than a pod starting up.
 */
export default function ManagedAgentPreviewPage({ params }: { params: Promise<{ name: string }> }) {
  const { name } = use(params)
  const { data: agent } = useQuery(managedAgentQueryOptions(name))
  const ready = agent?.status?.phase === "Ready"

  return <AgentPreviewPanel agent={name} ready={ready} />
}
