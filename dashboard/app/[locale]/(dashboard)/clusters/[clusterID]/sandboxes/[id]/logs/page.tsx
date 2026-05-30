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

import { SandboxLogsPanel } from "@/components/sandboxes/logs-sheet"

interface PageProps {
  params: Promise<{ clusterID: string; id: string; locale: string }>
}

/** Logs tab — full-height streaming log panel. */
export default function SandboxLogsPage({ params }: PageProps) {
  const { id } = use(params)

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <SandboxLogsPanel sandboxId={id} />
    </div>
  )
}
