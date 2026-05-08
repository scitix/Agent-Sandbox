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

import { SignJWT, jwtVerify, type JWTPayload } from "jose"

const JWT_EXPIRY = "7d"

// Cached secret encoding to avoid repeated TextEncoder calls
let cachedSecret: Uint8Array | null = null

export interface AuthJWTPayload extends JWTPayload {
  apiKey?: string // Present for API-key logins; absent for Mock/OIDC logins
  role: "admin" | "tenant"
  user?: string
  team?: string
  // Auth-method-specific fields
  authMethod?: "apikey" | "mock" | "oidc"
  name?: string
  email?: string
}

function getSecret(): Uint8Array {
  if (cachedSecret) return cachedSecret

  const secret = process.env.JWT_SECRET
  if (!secret) {
    throw new Error("JWT_SECRET environment variable is not set")
  }
  cachedSecret = new TextEncoder().encode(secret)
  return cachedSecret
}

export async function signJWT(payload: Omit<AuthJWTPayload, keyof JWTPayload>): Promise<string> {
  const secret = getSecret()
  return new SignJWT(payload as JWTPayload)
    .setProtectedHeader({ alg: "HS256" })
    .setIssuedAt()
    .setExpirationTime(JWT_EXPIRY)
    .sign(secret)
}

export async function verifyJWT(token: string): Promise<AuthJWTPayload> {
  const secret = getSecret()
  const { payload } = await jwtVerify(token, secret)
  return payload as AuthJWTPayload
}
