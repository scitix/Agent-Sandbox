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

import { describe, it, expect, beforeEach, afterEach } from "vitest"
import { isOIDCAdmin } from "@/lib/server/oidc-admins"

describe("isOIDCAdmin", () => {
  const origEnv = process.env.DEX_OIDC_ADMINS

  afterEach(() => {
    process.env.DEX_OIDC_ADMINS = origEnv
  })

  // ── Empty / unset ────────────────────────────────────────────────────────────

  it("returns false when DEX_OIDC_ADMINS is not set", () => {
    delete process.env.DEX_OIDC_ADMINS
    expect(isOIDCAdmin("org1", "bob")).toBe(false)
  })

  it("returns false when DEX_OIDC_ADMINS is empty string", () => {
    process.env.DEX_OIDC_ADMINS = ""
    expect(isOIDCAdmin("org1", "bob")).toBe(false)
  })

  it("returns false when DEX_OIDC_ADMINS is only whitespace", () => {
    process.env.DEX_OIDC_ADMINS = "   "
    expect(isOIDCAdmin("org1", "bob")).toBe(false)
  })

  // ── Org-only (no colon) — entire org is admin ────────────────────────────────

  it("grants admin to any user in an org when no colon is present", () => {
    process.env.DEX_OIDC_ADMINS = "org1"
    expect(isOIDCAdmin("org1", "bob")).toBe(true)
    expect(isOIDCAdmin("org1", "anyone")).toBe(true)
  })

  it("does NOT grant admin to users in a different org (org-only entry)", () => {
    process.env.DEX_OIDC_ADMINS = "org1"
    expect(isOIDCAdmin("org2", "bob")).toBe(false)
  })

  // ── "org:" with empty user list — entire org is admin ────────────────────────

  it("grants admin to any user when org ends with colon but no users listed", () => {
    process.env.DEX_OIDC_ADMINS = "org1:"
    expect(isOIDCAdmin("org1", "bob")).toBe(true)
    expect(isOIDCAdmin("org1", "someone-else")).toBe(true)
  })

  it("does NOT grant admin to other orgs via empty-user-list entry", () => {
    process.env.DEX_OIDC_ADMINS = "org1:"
    expect(isOIDCAdmin("org2", "bob")).toBe(false)
  })

  // ── Specific users ────────────────────────────────────────────────────────────

  it("grants admin only to listed users in an org", () => {
    process.env.DEX_OIDC_ADMINS = "org1:bob,alice,ted"
    expect(isOIDCAdmin("org1", "bob")).toBe(true)
    expect(isOIDCAdmin("org1", "alice")).toBe(true)
    expect(isOIDCAdmin("org1", "ted")).toBe(true)
  })

  it("denies admin to unlisted users in the matched org", () => {
    process.env.DEX_OIDC_ADMINS = "org1:bob,alice"
    expect(isOIDCAdmin("org1", "stranger")).toBe(false)
  })

  it("denies admin to listed users in a different org", () => {
    process.env.DEX_OIDC_ADMINS = "org1:bob"
    expect(isOIDCAdmin("org2", "bob")).toBe(false)
  })

  // ── Multiple entries (semicolon-separated) ────────────────────────────────────

  it("supports multiple entries separated by semicolons", () => {
    process.env.DEX_OIDC_ADMINS = "org1:bob,alice,ted;org2:carol"
    expect(isOIDCAdmin("org1", "bob")).toBe(true)
    expect(isOIDCAdmin("org1", "alice")).toBe(true)
    expect(isOIDCAdmin("org2", "carol")).toBe(true)
    expect(isOIDCAdmin("org2", "bob")).toBe(false)
    expect(isOIDCAdmin("other-org", "carol")).toBe(false)
  })

  it("handles a mix of org-only and specific-user entries", () => {
    process.env.DEX_OIDC_ADMINS = "org1;org2:carol"
    expect(isOIDCAdmin("org1", "anyone")).toBe(true)
    expect(isOIDCAdmin("org2", "carol")).toBe(true)
    expect(isOIDCAdmin("org2", "other")).toBe(false)
  })

  // ── Case-insensitivity ────────────────────────────────────────────────────────

  it("matches org case-insensitively", () => {
    process.env.DEX_OIDC_ADMINS = "org1:bob"
    expect(isOIDCAdmin("org1", "bob")).toBe(true)
    expect(isOIDCAdmin("org1", "bob")).toBe(true)
  })

  it("matches username case-insensitively", () => {
    process.env.DEX_OIDC_ADMINS = "org1:bob"
    expect(isOIDCAdmin("org1", "bob")).toBe(true)
    expect(isOIDCAdmin("org1", "bob")).toBe(true)
  })

  it("matches org-only entry case-insensitively", () => {
    process.env.DEX_OIDC_ADMINS = "org1"
    expect(isOIDCAdmin("org1", "anyone")).toBe(true)
  })

  // ── Whitespace tolerance ──────────────────────────────────────────────────────

  it("trims whitespace around entries and usernames", () => {
    process.env.DEX_OIDC_ADMINS = " org1 : bob , alice ; org2 : carol "
    expect(isOIDCAdmin("org1", "bob")).toBe(true)
    expect(isOIDCAdmin("org1", "alice")).toBe(true)
    expect(isOIDCAdmin("org2", "carol")).toBe(true)
  })

  it("ignores empty semicolon-separated entries gracefully", () => {
    process.env.DEX_OIDC_ADMINS = ";org1:bob;;"
    expect(isOIDCAdmin("org1", "bob")).toBe(true)
    expect(isOIDCAdmin("org1", "other")).toBe(false)
  })
})
