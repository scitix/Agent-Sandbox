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

// Sign in automatically when the dashboard is being served to its own machine,
// so local work and automated UI checks do not have to drive a login form to
// reach the pages they are checking.
//
// Gated on the ORIGIN rather than on a build-time flag, deliberately. A
// NEXT_PUBLIC_* flag has to be present when the client bundle is compiled, and
// getting that wrong fails silently — the page simply sits on the login screen
// with nothing saying why. A loopback host cannot be wrong: a deployment is
// never reached at 127.0.0.1 by the person using it.
//
// The second gate is the endpoint's own: /api/auth/mock/login refuses outright
// when OIDC is configured, which every real deployment has. So this cannot
// hand out a session anywhere it matters, and it goes through that ordinary
// endpoint rather than fabricating one — the token is real, and every
// downstream check runs exactly as it would for a person.

import type { AuthState } from "@/lib/atoms"

const LOOPBACK = new Set(["localhost", "127.0.0.1", "[::1]", "::1"])

export function devAutoLoginEnabled(): boolean {
  if (typeof window === "undefined") return false
  return LOOPBACK.has(window.location.hostname)
}

/** Who to sign in as. A tenant rather than an admin: the more restricted
 *  identity is the one worth developing against. */
function devIdentity(): { username: string; team: string } {
  const q = new URLSearchParams(window.location.search)
  return {
    username: q.get("devUser") || "ylli",
    team: q.get("devTeam") || "ylli",
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
