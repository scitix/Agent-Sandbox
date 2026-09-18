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
import { cliGuide } from '@/components/assistant-ui/cli-guide'

const LIVE = {
  e2bURL: 'https://gw.example.test/agent-sandbox/api/e2b',
  dataURL: 'https://gw.example.test/agent-sandbox/api/data',
  consoleBase: 'https://console.acme.example/agentbox',
  cluster: 'acme-manager',
}

describe('the setup guide carries this deployment and no other', () => {
  it('hard-codes no host of its own', () => {
    // The download bucket is the one address that is genuinely universal —
    // every deployment installs the same binary from it. Anything else naming
    // a host would be one deployment's address shipped to all of them.
    const src = readFileSync(
      join(__dirname, '..', '..', 'components', 'assistant-ui', 'cli-guide.ts'),
      'utf8',
    )
    const hosts = [...src.matchAll(/https?:\/\/([a-z0-9.-]+\.[a-z]{2,})/gi)].map(m => m[1])
    for (const h of hosts) {
      expect(h, `cli-guide.ts names the host ${h}`).toMatch(
        /^(www\.apache\.org|oss-ap-southeast\.scitix\.ai|scitix\.github\.io|e2b\.dev)$/,
      )
    }
  })

  for (const locale of ['en', 'zh-Hans'] as const) {
    it(`builds the abx context line from the live console (${locale})`, () => {
      const doc = cliGuide(LIVE, locale)
      // Named after the deployment, not after the front-door word.
      expect(doc).toContain('abx context set acme')
      // The console's own address, exactly as it appears in the browser — one
      // address for every cluster, so `--cluster` selects among them rather
      // than being a claim the address cannot honour. It is also the only
      // setting given: the auth header follows from it.
      expect(doc).toContain(`--endpoint '${LIVE.consoleBase}'`)
      expect(doc).not.toContain('--auth-scheme')
      expect(doc).not.toContain('--web-base')
      // The mount and the routing placeholder are the CLI's business, not
      // something a reader should be asked to paste.
      expect(doc).not.toContain('{cluster}')
      expect(doc).not.toContain('/api/clusters')
      // Direct mode is for a sandbox with no route to a console. Nobody
      // reading this page is in that position — they have the console open.
      expect(doc).not.toContain('--cluster-api')
      // abx leads; the E2B SDK is still there for the sandboxes themselves.
      expect(doc.indexOf('abx context set')).toBeLessThan(doc.indexOf('patch_e2b'))
      expect(doc).toContain(LIVE.e2bURL)
      // The first command after saving the context is the one that needs no
      // `--cluster` — a platform can reach several clusters, and `abx envs`
      // refused outright without one. The commands that do address an Env
      // carry the id of the cluster this page is about.
      expect(doc).toContain('abx clusters')
      expect(doc).not.toMatch(/^abx envs$/m)
      expect(doc).toContain(`abx envs YOUR_ENV pools --cluster ${LIVE.cluster}`)
      expect(doc).toContain(`abx envs YOUR_ENV docs --cluster ${LIVE.cluster}`)
      // The key is a placeholder for the console to fill in, never a value the
      // server or this module could have known. A document that arrives with a
      // live credential in it cannot be copied into a chat or handed to an
      // agent, which is the one thing this document is for.
      expect(doc).toContain('${AGBX_API_KEY}')
      expect(doc).not.toMatch(/agbx_[a-z0-9]/)
      // …and it is written only inside a code block. In prose the same token is
      // a sentence a reader skims past; filled in, it is a credential in a
      // paragraph somebody quotes. The check strips fenced blocks itself rather
      // than leaning on `fillApiKey`, so the two cannot agree by construction.
      expect(proseOf(doc)).not.toContain('${AGBX_API_KEY}')
      // The CLI's own docs sub-resource is how the reader — or their agent —
      // gets the endpoints this deployment actually answers on.
      expect(doc).toContain('docs')
      // A document meant to be read by an agent says so, and names the tool
      // that hands it the whole CLI shape.
      expect(doc).toContain('abx agent-context')
    })
  }

  it('degrades to placeholders rather than to somebody else’s address', () => {
    const doc = cliGuide({}, 'en')
    expect(doc).toContain('https://<console>/agentbox')
    expect(doc).not.toMatch(/https:\/\/console\.[a-z]/)
    // A cluster-less page says where the id goes rather than naming one.
    expect(doc).toContain('abx envs YOUR_ENV pools --cluster YOUR_CLUSTER')
  })
})

/** Everything in `doc` that is not inside a fenced code block. Deliberately a
 *  second implementation: the point is to catch prose carrying the placeholder,
 *  and reusing the renderer's own helper would make that unfalsifiable. */
function proseOf(doc: string): string {
  const out: string[] = []
  let inFence = false
  for (const line of doc.split('\n')) {
    if (/^\s*(`{3,}|~{3,})/.test(line)) {
      inFence = !inFence
      continue
    }
    if (!inFence) out.push(line)
  }
  return out.join('\n')
}
