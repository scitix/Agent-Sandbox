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
  return proxyToAssistant(request, (await ctx.params).path, origin())
}
export async function POST(request: NextRequest, ctx: Ctx) {
  return proxyToAssistant(request, (await ctx.params).path, origin())
}
export async function DELETE(request: NextRequest, ctx: Ctx) {
  return proxyToAssistant(request, (await ctx.params).path, origin())
}
