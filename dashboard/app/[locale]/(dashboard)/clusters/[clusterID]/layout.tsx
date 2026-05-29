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

// Cluster-scoped layout. Auth guard and shell (Sidebar, etc.) come from the
// parent (dashboard)/layout.tsx; this layer renders the single, route-driven
// PageHeader once so every cluster page inherits it without rendering its own.

"use client"

import { PageHeader } from "@/components/page-header"

export default function ClusterLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-full w-full flex-col overflow-hidden">
      <PageHeader />
      <div className="flex min-h-0 flex-1 flex-col">{children}</div>
    </div>
  )
}
