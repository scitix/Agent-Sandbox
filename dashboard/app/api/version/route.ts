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

// GET /api/version — which build of the Dashboard is serving this page.
//
// This exists because `NEXT_PUBLIC_APP_VERSION` cannot answer that. Next.js
// inlines NEXT_PUBLIC_* into the client bundle at image-build time, so it is
// fixed before the image has a tag and says the same thing for every deployment
// built from that commit. Reading an ordinary (non-public) variable here, on the
// server, at request time, lets the chart pass the image tag it deployed.
//
// Returns: { dashboardVersion: string }

import { NextResponse } from "next/server"

/** Set by the hub chart from `.Values.image.tag`. */
const RUNTIME_VERSION = process.env.APP_VERSION

/** Build-time fallback, so `pnpm dev` and unchanged deployments still show something. */
const BUILD_VERSION = process.env.NEXT_PUBLIC_APP_VERSION

export function resolveDashboardVersion(runtime = RUNTIME_VERSION, build = BUILD_VERSION): string {
  // A Helm value left empty renders as an empty variable rather than an absent
  // one, which would otherwise blank the About panel.
  return runtime?.trim() || build?.trim() || "0.0.0"
}

export function GET() {
  return NextResponse.json({ dashboardVersion: resolveDashboardVersion() })
}
