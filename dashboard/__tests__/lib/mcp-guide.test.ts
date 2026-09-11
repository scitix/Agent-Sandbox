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
 * The setup guide is generated, and the reason is the assertion below: this
 * repository is public and one organisation runs several deployments from it,
 * so no address may be written into the source. Every address in the document
 * has to have come from the live deployment that rendered it.
 */

import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { mcpGuide } from '@/components/assistant-ui/mcp-guide'

const LIVE = {
  e2bURL: 'https://gw.example.test/agent-sandbox/api/e2b',
  dataURL: 'https://gw.example.test/agent-sandbox/api/data',
  consoleBase: 'https://console.acme.example/agentbox',
  clusterID: 'cluster-a',
}

describe('the setup guide carries this deployment and no other', () => {
  it('hard-codes no host of its own', () => {
    // The download bucket is the one address that is genuinely universal —
    // every deployment installs the same binary from it. Anything else naming
    // a host would be one deployment's address shipped to all of them.
    const src = readFileSync(
      join(__dirname, '..', '..', 'components', 'assistant-ui', 'mcp-guide.ts'),
      'utf8',
    )
    const hosts = [...src.matchAll(/https?:\/\/([a-z0-9.-]+\.[a-z]{2,})/gi)].map(m => m[1])
    for (const h of hosts) {
      expect(h, `mcp-guide.ts names the host ${h}`).toMatch(
        /^(www\.apache\.org|oss-ap-southeast\.scitix\.ai|e2b\.dev)$/,
      )
    }
  })

  for (const locale of ['en', 'zh-Hans'] as const) {
    it(`builds the abx context line from the live console (${locale})`, () => {
      const doc = mcpGuide(LIVE, locale)
      // Named after the deployment, not after the front-door word.
      expect(doc).toContain('abx context set acme')
      // One endpoint for every cluster: the placeholder is what makes
      // --cluster a substitution rather than an unanswerable claim.
      expect(doc).toContain('https://console.acme.example/agentbox/api/clusters/{cluster}')
      expect(doc).toContain('--cluster cluster-a')
      // abx leads; the E2B SDK is still there for the sandboxes themselves.
      expect(doc.indexOf('abx context set')).toBeLessThan(doc.indexOf('patch_e2b'))
      expect(doc).toContain(LIVE.e2bURL)
    })
  }

  it('degrades to placeholders rather than to somebody else’s address', () => {
    const doc = mcpGuide({}, 'en')
    expect(doc).toContain('https://<console>/agentbox/api/clusters/{cluster}')
    expect(doc).not.toMatch(/https:\/\/console\.[a-z]/)
  })
})
