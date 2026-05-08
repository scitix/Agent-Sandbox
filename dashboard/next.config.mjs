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

const nextConfig = {
  output: "standalone",
  basePath,
  // assetPrefix must match basePath so _next/static/* assets are served correctly.
  assetPrefix: basePath || undefined,
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
