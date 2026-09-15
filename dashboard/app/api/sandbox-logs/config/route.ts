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

/**
 * GET /api/sandbox-logs/config
 *
 * Returns whether the external log service is configured on this BFF instance.
 * The response is not sensitive — no auth required.
 *
 * The gate is URL + token, matching getLogConfig() in the sibling route that
 * actually serves the query. LOG_APP_ID is NOT part of it: its absence selects
 * Bearer auth rather than the signed scheme, so requiring it here reported
 * "not configured" on every Bearer deployment — and a false answer is silent,
 * because the viewer's only reaction is to keep asking the live-stream endpoint,
 * which has nothing to say about a Pod that no longer exists.
 *
 * Response: { configured: boolean }
 */

import { NextResponse } from "next/server"

export function GET(): NextResponse {
  const configured = !!(process.env.LOG_DOWNLOAD_URL && process.env.LOG_TOKEN)
  return NextResponse.json({ configured })
}
