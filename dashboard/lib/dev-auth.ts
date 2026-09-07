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

// Sign in automatically, for local work and for automated UI checks.
//
// Double-gated, and both gates matter. NODE_ENV is checked so this cannot ship
// in a production bundle whatever the environment says, and the flag is checked
// so a normal `pnpm dev` still shows the login page — a developer who wants to
// exercise the real sign-in must not have it skipped underneath them.
//
// It goes through the ordinary mock-login endpoint rather than fabricating a
// session, so the token is real and every downstream check — including the
// assistant proxy's, which is the whole reason the assistant needs a verified
// session — runs exactly as it would for a person.

import type { AuthState } from "@/lib/atoms"

export function devAutoLoginEnabled(): boolean {
  return (
    process.env.NODE_ENV !== "production" &&
    process.env.NEXT_PUBLIC_DEV_AUTOLOGIN === "1"
  )
}

/** Who to sign in as. Defaults are a tenant, not an admin: the more restricted
 *  identity is the one worth developing against. */
function devIdentity(): { username: string; team: string } {
  return {
    username: process.env.NEXT_PUBLIC_DEV_USER || "ylli",
    team: process.env.NEXT_PUBLIC_DEV_TEAM || "ylli",
  }
}

export async function devAutoLogin(): Promise<AuthState | null> {
  if (!devAutoLoginEnabled()) return null
  try {
    const res = await fetch("/api/auth/mock/login", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(devIdentity()),
    })
    if (!res.ok) return null
    return (await res.json()) as AuthState
  } catch {
    return null
  }
}
