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

/** @type {import('next').NextConfig} */

// NEXT_BASE_PATH is a BUILD-TIME variable injected via Docker ARG.
// Set to "" for root deployments (e.g. xxx.com/) or "/agentbox" for
// sub-path deployments (e.g. xxx.com/agentbox).
const basePath = process.env.NEXT_BASE_PATH || ""

// NEXT_PUBLIC_APP_VERSION is a BUILD-TIME variable injected via Docker ARG.
// Defaults to "0.0.0" (suppresses the changelog dialog in dev builds).
// CI usage: docker build --build-arg NEXT_PUBLIC_APP_VERSION=1.3.0 ...
const appVersion = process.env.NEXT_PUBLIC_APP_VERSION || "0.0.0"

// Remote-backend dev mode: proxy every /api/* request to another deployment's
// BFF (e.g. a developer laptop running the frontend against a hub console it
// can reach over HTTPS, while the worker clusters and ws-proxy behind that BFF
// are unreachable locally). Auth runs entirely on the remote BFF — it signs the
// session JWT with its own secret, so no local JWT_SECRET/cluster config is
// needed; API-key login against the remote is the supported entry. Terminal
// WebSocket (/ws/*) is NOT covered: Next rewrites do not proxy WS upgrades.
//
// The rewrite targets a local forwarder (hack/dev-api-forwarder.mjs, spawned by
// hack/dev-dashboard.sh) instead of the console directly: loopback never drops
// connections, and the forwarder owns the TLS to the console with keep-alive +
// connect-phase retries — Next's internal proxy hangs a request forever when
// the direct TLS handshake is reset (common on VPN fake-IP routes).
const remoteApiBase = process.env.LOCAL_API_PROXY_BASE || ""
const forwarderUrl = `http://127.0.0.1:${process.env.LOCAL_API_FORWARD_PORT || "9999"}`

const nextConfig = {
  output: "standalone",
  basePath,
  // assetPrefix must match basePath so _next/static/* assets are served correctly.
  assetPrefix: basePath || undefined,
  async rewrites() {
    if (!remoteApiBase) return []
    return {
      // beforeFiles: takes precedence over the local app/api route handlers,
      // otherwise the filesystem routes would win and the proxy never fires.
      beforeFiles: [
        {
          source: "/api/:path*",
          destination: `${forwarderUrl}/api/:path*`,
        },
      ],
      afterFiles: [],
      fallback: [],
    }
  },
  env: {
    // Expose basePath to client-side code so fetch() calls can prefix it.
    // NEXT_PUBLIC_* vars are inlined at build time.
    NEXT_PUBLIC_BASE_PATH: basePath,
    // Expose app version to client-side code for changelog dialog.
    NEXT_PUBLIC_APP_VERSION: appVersion,
  },
  typescript: {
    // ignoreBuildErrors: true,
  },
  images: {
    unoptimized: true,
  },
}

export default nextConfig
