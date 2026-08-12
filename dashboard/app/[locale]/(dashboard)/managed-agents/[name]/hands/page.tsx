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

import { HandsSection } from "@/components/managed-agents/detail-sections"
import { managedAgentQueryOptions } from "@/lib/queries"

interface PageProps {
  params: Promise<{ name: string; locale: string }>
}

/** Hands tab — declared and resolved sandbox supply. */
export default function AgentHandsPage({ params }: PageProps) {
  const { name } = use(params)
  const { data: agent } = useQuery(managedAgentQueryOptions(name))

  if (!agent) return null

  return (
    <div className="min-h-0 flex-1 overflow-y-auto px-6 py-4">
      <HandsSection agent={agent} />
    </div>
  )
}
