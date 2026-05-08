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

import { NextResponse } from "next/server"
import { isOIDCEnabled } from "@/lib/server/oidc"

/**
 * GET /api/auth/oidc/config
 *
 * Returns whether OIDC (Dex) mode is enabled on the server.
 * Safe to call from the browser — never exposes secrets.
 */
export async function GET() {
  return NextResponse.json({ enabled: isOIDCEnabled() })
}
