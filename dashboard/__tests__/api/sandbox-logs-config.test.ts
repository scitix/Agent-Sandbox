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

import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { GET } from "@/app/api/sandbox-logs/config/route"

/**
 * The feature gate for a finished sandbox's logs.
 *
 * This endpoint is the only thing the viewer consults before deciding whether
 * to ask the central log service at all, and a false answer is completely
 * silent: the panel falls back to the live-stream endpoint, which has nothing
 * to say about a Pod that was recycled, and renders an empty view. No error,
 * no hint — just a sandbox that appears to have printed nothing.
 *
 * It said false on every Bearer deployment for as long as Bearer existed,
 * because it required LOG_APP_ID — the variable whose *absence* selects Bearer.
 */
describe("GET /api/sandbox-logs/config", () => {
  const saved = { ...process.env }

  beforeEach(() => {
    delete process.env.LOG_DOWNLOAD_URL
    delete process.env.LOG_TOKEN
    delete process.env.LOG_APP_ID
  })
  afterEach(() => {
    process.env = { ...saved }
  })

  it("reports configured for the Bearer scheme, which sets no app id", async () => {
    process.env.LOG_DOWNLOAD_URL = "https://logs.example.com/api/query/logs/download"
    process.env.LOG_TOKEN = "token"
    expect(await GET().json()).toEqual({ configured: true })
  })

  it("reports configured for the signed scheme too", async () => {
    process.env.LOG_DOWNLOAD_URL = "https://logs.example.com/api/query/logs/download"
    process.env.LOG_TOKEN = "token"
    process.env.LOG_APP_ID = "app-1"
    expect(await GET().json()).toEqual({ configured: true })
  })

  it("reports unconfigured when the endpoint is missing", async () => {
    process.env.LOG_TOKEN = "token"
    expect(await GET().json()).toEqual({ configured: false })
  })

  it("reports unconfigured when the credential is missing", async () => {
    process.env.LOG_DOWNLOAD_URL = "https://logs.example.com/api/query/logs/download"
    expect(await GET().json()).toEqual({ configured: false })
  })
})
