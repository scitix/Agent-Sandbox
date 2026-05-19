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
 * patch_e2b — Agent Sandbox patch for e2b JS SDK (v2.19.0)
 *
 * Monkey-patches the E2B SDK to redirect API calls and sandbox connections
 * to an Agent Sandbox deployment.
 *
 * Architecture:
 *   - Control plane (E2B-compatible REST API): Agent Sandbox exposes standard E2B
 *     routes at :8090. Set E2B_API_URL=http(s)://<host> (no path suffix).
 *   - Data plane (envd gRPC / connect-proto): Traffic goes through the Envoy
 *     ExtProc gateway using the path format:
 *       <gateway>/sandboxes/<sandboxID>/<port>/...
 *     E2B_DOMAIN should be the gateway host (no scheme).
 *
 * In-cluster defaults (call patchE2b() with no arguments):
 *   - Data plane gateway:
 *     agent-sandbox-data-plane.agentbox-system.svc.cluster.local
 *   - Control plane API:
 *     http://agent-sandbox-e2b-api.agentbox-system.svc.cluster.local
 *   - HTTPS: false
 *
 * Usage (ESM):
 *   import { patchE2b } from './patch_e2b.js';
 *   await patchE2b();
 *   const { Sandbox } = await import('e2b');
 *   const sandbox = await Sandbox.create('my-pool//docker.io/library/ubuntu:22.04');
 *
 * Usage (CJS):
 *   const { patchE2b } = require('./patch_e2b.js');
 *   await patchE2b();
 *   const { Sandbox } = require('e2b');
 *
 * IMPORTANT: patchE2b() must be called BEFORE the first use of any Sandbox class.
 * In ESM, dynamic import() ensures ordering; in CJS, call it before require('e2b').
 *
 * @compatible e2b 2.19.0
 */

// Compatible e2b version this patch was written and tested against.
const COMPATIBLE_E2B_VERSION = '2.19.0';

const DEFAULT_DOMAIN = 'agent-sandbox-data-plane.agentbox-system.svc.cluster.local';
const DEFAULT_API_URL = 'http://agent-sandbox-e2b-api.agentbox-system.svc.cluster.local';

/**
 * Strip http:// or https:// scheme prefix from a URL string.
 * @param {string} url
 * @returns {string} host[:port] without scheme
 */
function stripScheme(url) {
  return url.replace(/^https?:\/\//, '').replace(/\/$/, '');
}

/**
 * Patch the E2B SDK to route API calls and sandbox connections to Agent Sandbox.
 *
 * @param {object}  [opts]
 * @param {boolean} [opts.https=false]    Use HTTPS for envd data-plane connections.
 * @param {string}  [opts.domain]         Data-plane Envoy gateway host (no scheme).
 *                                        Priority: param > E2B_DOMAIN env > cluster default.
 * @param {string}  [opts.apiUrl]         Control-plane API URL (with scheme).
 *                                        Priority: param > E2B_API_URL env > cluster default.
 */
async function patchE2b({ https = false, domain, apiUrl } = {}) {
  // Resolve domain (data-plane gateway): param > env > cluster default
  const resolvedDomain = domain
    || (process.env.E2B_DOMAIN ? stripScheme(process.env.E2B_DOMAIN) : '')
    || DEFAULT_DOMAIN;

  // Resolve API URL (control-plane): param > env > cluster default
  const resolvedApiUrl = apiUrl
    || process.env.E2B_API_URL
    || DEFAULT_API_URL;

  // Set env vars so the static getters on ConnectionConfig pick them up.
  // E2B_DOMAIN must not have a scheme prefix — the static getter returns it raw.
  process.env.E2B_DOMAIN = resolvedDomain;
  process.env.E2B_API_URL = resolvedApiUrl;
  // E2B_API_KEY is expected to be set by the user (or already in the environment).

  // Resolve ConnectionConfig from the already-loaded e2b module.
  // In CJS: use require(); in ESM: use createRequire or rely on the caller
  // having already imported e2b (prototype patches apply to the shared module).
  let ConnectionConfig;
  try {
    if (typeof require !== 'undefined') {
      // CJS environment
      ConnectionConfig = require('e2b').ConnectionConfig;
    } else {
      // ESM environment — use createRequire to load the CJS bundle
      const { createRequire } = await import('module');
      const req = createRequire(import.meta.url);
      ConnectionConfig = req('e2b').ConnectionConfig;
    }
  } catch (err) {
    throw new Error(
      "The 'e2b' package is required. Install it with: npm install e2b\n" + err.message
    );
  }

  /**
   * Override getHost to use the Agent Sandbox path-based routing format:
   *   <gateway>/sandboxes/<sandboxId>/<port>
   *
   * The default E2B implementation uses subdomain routing:
   *   <port>-<sandboxId>.<domain>
   * which is incompatible with Agent Sandbox's Envoy ExtProc gateway.
   */
  ConnectionConfig.prototype.getHost = function (sandboxId, port, _sandboxDomain) {
    return `${resolvedDomain}/sandboxes/${sandboxId}/${port}`;
  };

  /**
   * Override getSandboxUrl to control the HTTP scheme.
   * The default SDK always uses https:// in non-debug mode, which breaks
   * cluster-internal setups where TLS is not available on the gateway.
   */
  ConnectionConfig.prototype.getSandboxUrl = function (sandboxId, opts) {
    if (this.sandboxUrl) return this.sandboxUrl;
    const scheme = https ? 'https' : 'http';
    return `${scheme}://${this.getHost(sandboxId, opts.envdPort, opts.sandboxDomain)}`;
  };
}

// CommonJS export
if (typeof module !== 'undefined' && module.exports) {
  module.exports = { patchE2b, COMPATIBLE_E2B_VERSION };
}

// Named ESM export (works when bundled via esbuild/rollup or when using .mjs)
export { patchE2b, COMPATIBLE_E2B_VERSION };
