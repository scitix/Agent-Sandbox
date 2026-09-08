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

// The workspace file API — attachment staging, the file browser, and reading a
// sent attachment back. A different PORT on the assistant pod from the
// conversation gateway, and the one other surface a browser reaches directly.
//
// See lib/server/assistant-proxy.ts for why this route exists at all.

import type { NextRequest } from "next/server"
import { proxyToAssistant } from "@/lib/server/assistant-proxy"

function origin(): string {
  return (
    process.env.ASSISTANT_FS_URL ??
    "http://agentbox-dashboard-assistant:8766"
  )
}

type Ctx = { params: Promise<{ path: string[] }> }

export async function GET(request: NextRequest, ctx: Ctx) {
  return proxyToAssistant(request, (await ctx.params).path, origin(), false)
}
export async function POST(request: NextRequest, ctx: Ctx) {
  return proxyToAssistant(request, (await ctx.params).path, origin(), false)
}
export async function DELETE(request: NextRequest, ctx: Ctx) {
  return proxyToAssistant(request, (await ctx.params).path, origin(), false)
}
