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
 * What a command actually prints, against a platform that answers.
 *
 * The registry tests above assert the shape of what the CLI knows; these run
 * the CLI against a stub console and read its output, because every bug this
 * file guards against was one where the CLI had the data and rendered
 * something else — a detail page of dashes, logs drawn as a sandbox table, an
 * approval link that never reached the caller.
 */

import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'bun:test'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { CliError } from '../src/context'
import { requestAt } from '../src/api'
import { run } from '../src/main'

const ENV_OVERRIDES = {
  envName: 'demo-env',
  templateName: 'demo-e2b-typescript',
  mode: 'WarmPool',
  // What the server actually renders: every fact about the cluster filled in,
  // and the API key left as the placeholder for the client to fill. A document
  // with a live credential in it could not be printed, echoed or handed to an
  // agent, which is exactly what this view is for.
  envDocs:
    '# demo-env\n\nE2B_API_URL=https://gw.example.test/agent-sandbox/api/e2b\n' +
    'AGENTBOX_API_KEY=${AGBX_API_KEY}\n',
}

const POOL = {
  name: 'demo-env-500mc1gi',
  namespace: 't-team-a-alice',
  owningEnv: 'demo-env',
  scalingGroup: '500mc1gi',
  cpu: '500m',
  memory: '1Gi',
  templateVersion: '2026.08.04',
  spec: { replicas: 1 },
  status: { phase: 'Ready', idleReplicas: 1, runningReplicas: 0 },
}

const TEMPLATE = {
  name: 'demo-e2b-typescript',
  version: '2026.08.04',
  description: 'TypeScript sandbox',
  // The same contract as the env's docs: endpoints rendered, key a placeholder.
  docs: '# docs\n\nuse ${AGBX_API_KEY}\n',
  crdYaml: 'apiVersion: agents.navix.sh/v1alpha1\nkind: SandboxTemplate\n',
  cpu: '500m',
  memory: '1Gi',
  syncSource: 'global',
}

let server: ReturnType<typeof Bun.serve>
let base = ''
let workdir = ''
const requests: string[] = []

/**
 * The env's sizing rule, as the stub reports it — and, when something is
 * refused locally, whether the write even left the process.
 */
let poolSizing: string | undefined
let poolWrites: Record<string, unknown>[] = []

beforeAll(() => {
  server = Bun.serve({
    port: 0,
    async fetch(req) {
      const url = new URL(req.url)
      const p = url.pathname
      requests.push(`${req.method} ${p}`)

      const json = (body: unknown, status = 200) =>
        new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

      if (p === '/api/clusters') {
        return json({ clusters: [{ id: 'prod-foo', name: 'manager' }, { id: 'prod-bar', name: 'prod-bar' }] })
      }
      // Direct mode asks the cluster itself, and this is where it learns which
      // one it is talking to. The field is `local`; the CLI used to read
      // `isLocal`, which no version of the API has ever sent.
      if (p === '/v1/clusters') {
        return json({
          clusters: [
            { id: 'prod-foo', local: true, name: 'manager' },
            { id: 'prod-bar', local: false, name: 'prod-bar' },
          ],
        })
      }
      if (p.endsWith('/v1/auth/whoami')) {
        return json({ role: 'tenant', mode: 'agent', user: 'alice', team: 'k8s', namespace: 't-team-a-alice' })
      }
      if (p.endsWith('/v1/envs/demo-env')) {
        return json({
          env: {
            name: ENV_OVERRIDES.envName,
            namespace: 't-team-a-alice',
            team: 'k8s',
            user: 'alice',
            createdAt: '2026-08-04T06:37:18Z',
            envDocs: ENV_OVERRIDES.envDocs,
            // Absent unless a test sets it: an unstamped env is exactly what
            // the CLI has to keep working against.
            ...(poolSizing ? { poolSizing } : {}),
            spec: {
              templateRef: { name: ENV_OVERRIDES.templateName },
              mode: ENV_OVERRIDES.mode,
            },
            status: { memberCount: 1, desiredReplicas: 1, runningReplicas: 0, idleReplicas: 1 },
          },
          // What a write takes, as opposed to what a read returns. Same schema
          // as the PUT body, so this file can go straight back to `apply -f`.
          editable: {
            templateRef: { name: ENV_OVERRIDES.templateName },
            mode: ENV_OVERRIDES.mode,
            overrides: { gateway: { enabled: true } },
          },
        })
      }
      if (p.endsWith('/v1/envs/demo-env/sandboxpools')) {
        if (req.method === 'POST') {
          poolWrites.push((await req.json()) as Record<string, unknown>)
          return json({ pool: POOL }, 201)
        }
        return json({ items: [POOL] })
      }
      if (p.endsWith(`/v1/envs/demo-env/sandboxpools/${POOL.name}`)) {
        if (req.method === 'PUT') poolWrites.push((await req.json()) as Record<string, unknown>)
        return json({ pool: POOL })
      }
      if (p.endsWith('/v1/envs/demo-env/autoscaling/groups')) {
        return json({
          groups: [{ name: '500mc1gi', enabled: true, minReplicas: 1, maxReplicas: 64, scaleUpPolicy: { mode: 'Default' } }],
        })
      }
      if (p.endsWith('/v1/sandboxes/abc-123/logs')) {
        return json({
          sandboxId: 'abc-123',
          namespace: 't-team-a-alice',
          podName: 'demo-env-500mc1gi-abcde',
          capturedAt: '2026-09-15T12:00:00Z',
          source: 'live',
          truncated: false,
          totalBytes: 42,
          containers: ['sandbox', 'istio-proxy'],
          entries: [
            { timestamp: '2026-09-15T11:59:59Z', container: 'sandbox', log: 'hello from the sandbox' },
            { timestamp: '2026-09-15T12:00:00Z', container: 'istio-proxy', log: 'egress ok' },
          ],
        })
      }
      if (p.endsWith('/v1/envs/doesnotexist-xyz') && req.method === 'DELETE') {
        return json(
          {
            error: 'approval required: Delete an environment (doesnotexist-xyz)',
            errorCode: 'APPROVAL_REQUIRED',
            detail: {
              approvalId: 'apr_32fa2771a9169143',
              operation: 'env.delete',
              summary: 'Delete an environment (doesnotexist-xyz)',
              onceOnly: true,
              url: 'https://console.example/agentbox/clusters/prod-foo/approvals?id=apr_32fa2771a9169143',
              pollUrl: '/v1/approvals/apr_32fa2771a9169143',
              expiresAt: '2026-09-15T12:20:57Z',
            },
          },
          428,
        )
      }
      if (p.endsWith('/v1/api-keys') && req.method === 'POST') {
        return json(
          {
            error: 'this credential may not create an API key: issuing a credential would let it act without the approval gate.',
            errorCode: 'FORBIDDEN_FOR_AGENT',
            detail: { operation: 'apikey.create', summary: 'Create an API key', url: 'https://console.example/agentbox/api-keys' },
          },
          403,
        )
      }
      if (p.endsWith('/v1/sandbox-templates')) {
        return json({
          items: [
            TEMPLATE,
          ],
        })
      }
      if (p.endsWith('/v1/sandbox-templates/demo-e2b-typescript')) {
        return json({ template: TEMPLATE })
      }
      return json({ error: `nothing here: ${p}` }, 404)
    },
  })
  base = `http://127.0.0.1:${server.port}`
})

afterAll(() => server.stop(true))

/** Run the CLI as if from a shell, capturing both streams. */
async function cli(argv: string[]): Promise<{ code: number; out: string; err: string }> {
  const out: string[] = []
  const err: string[] = []
  const log = console.log
  const error = console.error
  console.log = (...a: unknown[]) => void out.push(a.join(' '))
  console.error = (...a: unknown[]) => void err.push(a.join(' '))
  try {
    const code = await run(argv)
    return { code, out: out.join('\n'), err: err.join('\n') }
  } catch (e) {
    // index.ts does this in production; tests want the same text.
    if (e instanceof CliError) {
      err.push(`error: ${e.message}`)
      if (e.hint) err.push(e.hint)
    } else {
      err.push(`error: ${e instanceof Error ? e.message : String(e)}`)
    }
    return { code: 1, out: out.join('\n'), err: err.join('\n') }
  } finally {
    console.log = log
    console.error = error
  }
}

beforeEach(() => {
  // A fresh config directory per test: the role cache is written to disk and
  // would otherwise carry a decision from one case into the next.
  workdir = mkdtempSync(join(tmpdir(), 'abx-ux-'))
  process.env.XDG_CONFIG_HOME = workdir
  process.env.AGENTBOX_ENDPOINT = base
  process.env.AGENTBOX_API_KEY = 'k'
  delete process.env.AGENTBOX_CLUSTER
  delete process.env.AGENTBOX_CLUSTER_API
})

describe('a get is the object, not a row of the list', () => {
  it('renders the fields the list never shows', async () => {
    const { out } = await cli(['envs', 'demo-env', '--cluster', 'prod-foo'])
    // Every one of these used to print as `—`, with the value in the response.
    expect(out).toMatch(/^template\s+demo-e2b-typescript$/m)
    expect(out).toMatch(/^mode\s+WarmPool$/m)
    expect(out).toMatch(/^idleReplicas\s+1$/m)
    // The child lists come with it, which is what makes it a detail page.
    expect(out).toContain('demo-env-500mc1gi')
    expect(out).toContain('500mc1gi')
    expect(out).toContain('hint:')
  })

  it('keeps envDocs out of the detail page, where it would read as a scalar', async () => {
    const { out } = await cli(['envs', 'demo-env', '--cluster', 'prod-foo'])
    expect(out).not.toContain('E2B_API_URL')
    expect(out).not.toContain('envDocs')
  })

  it('hands envDocs back under --json, whole, because there is no secret in it', async () => {
    const { out } = await cli(['envs', 'demo-env', '--cluster', 'prod-foo', '--json'])
    const parsed = JSON.parse(out) as Record<string, unknown>
    expect(parsed.name).toBe('demo-env')
    expect(parsed.envDocs).toContain('E2B_API_URL=')
    // The key is a placeholder here for the same reason it is on the server:
    // this output ends up in transcripts and CI logs.
    expect(parsed.envDocs).toContain('${AGBX_API_KEY}')
    expect((parsed.spec as Record<string, unknown>).mode).toBe('WarmPool')
  })

  it('prints the document, not a table, for `abx envs <env> docs`', async () => {
    const { out } = await cli(['envs', 'demo-env', 'docs', '--cluster', 'prod-foo'])
    // The endpoints an agent cannot guess are the whole point of the view.
    expect(out).toContain('E2B_API_URL=https://gw.example.test/agent-sandbox/api/e2b')
    expect(out).toContain('${AGBX_API_KEY}')
    // The heading survives as a heading rather than being folded into a row.
    expect(out.split('\n')[0]).toBe('# demo-env')
  })

  it('renders a template full text, docs included', async () => {
    const { out } = await cli(['templates', 'demo-e2b-typescript', '--cluster', 'prod-foo'])
    expect(out).toContain('docs (')
    expect(out).toContain('use ${AGBX_API_KEY}')
    expect(out).toContain('crdYaml (')
  })
})

describe('logs are logs', () => {
  it('prints the lines and where they came from, not a sandbox table', async () => {
    const { out } = await cli(['sandboxes', 'abc-123', 'logs', '--cluster', 'prod-foo'])
    expect(out).toContain('hello from the sandbox')
    expect(out).toContain('egress ok')
    expect(out).toContain('source')
    expect(out).toContain('sandbox, istio-proxy')
    // The old rendering: one "sandbox" row whose every column was `—`.
    expect(out).not.toContain('sandboxId  —')
  })
})

describe('a held write says where to release it', () => {
  it('prints the console link, the approval id and the expiry', async () => {
    const { err } = await cli(['delete', 'envs', 'doesnotexist-xyz', '--cluster', 'prod-foo'])
    expect(err).toContain('this write is waiting for a person: Delete an environment (doesnotexist-xyz)')
    expect(err).toContain('https://console.example/agentbox/clusters/prod-foo/approvals?id=apr_32fa2771a9169143')
    expect(err).toContain('apr_32fa2771a9169143')
    expect(err).toContain('env.delete')
    expect(err).toContain('single-use')
    expect(err).toContain('2026-09-15T12:20:57Z')
    // The doubled prefix the old code printed by concatenating both messages.
    expect(err).not.toContain('held for approval: approval required')
  })

  it('a write that will never be approved does not read as something to wait for', async () => {
    // Straight at the transport: the CLI's own pre-flight refusal covers the
    // common case, and this is the server's half of the same contract.
    const ctx = { endpoint: base, apiKey: 'k', cluster: 'prod-foo', format: 'json' as const }
    let message = ''
    let hint = ''
    try {
      await requestAt(`${base}/api/clusters/prod-foo/v1/api-keys`, ctx, 'POST', {})
      throw new Error('should have been refused')
    } catch (e) {
      expect(e).toBeInstanceOf(CliError)
      message = (e as CliError).message
      hint = (e as CliError).hint ?? ''
    }
    expect(message).toContain('this credential cannot do that at all: Create an API key')
    expect(hint).toContain('https://console.example/agentbox/api-keys')
    expect(`${message} ${hint}`.toLowerCase()).not.toContain('retry')
    expect(`${message} ${hint}`.toLowerCase()).not.toContain('waiting')
  })
})

describe('the address is validated before the deployment is', () => {
  it('a mistyped resource is an unknown resource, not a question about clusters', async () => {
    const { err } = await cli(['badtoken'])
    expect(err).toContain('unknown resource "badtoken"')
    // The old behaviour: every mistyped token was answered with the cluster
    // refusal, because the cluster was resolved before the address was read.
    expect(err).not.toContain('needs one')
    expect(err).not.toContain('--cluster')
  })

  it('only the plural is a resource', async () => {
    const { err } = await cli(['cluster'])
    expect(err).toContain('unknown resource "cluster"')
    expect(err).toContain('clusters')
  })

  it('and it is answered even with no deployment configured at all', async () => {
    // The old order resolved the deployment first, so a typo in the resource
    // was answered with "no endpoint configured" and the typo never surfaced.
    delete process.env.AGENTBOX_ENDPOINT
    delete process.env.AGENTBOX_API_KEY
    const { err } = await cli(['badtoken'])
    expect(err).toContain('unknown resource "badtoken"')
    expect(err).not.toContain('no endpoint configured')
  })

  it('an unknown flag names the ones that exist', async () => {
    const { err } = await cli(['envs', '--nope'])
    expect(err).toContain('unknown flag "--nope"')
    expect(err).toContain('--cluster')
  })

  it('--limit takes a number, and --cluster needs its value', async () => {
    expect((await cli(['envs', '--limit', 'abc'])).err).toContain('--limit takes a number')
    expect((await cli(['envs', '--cluster'])).err).toContain('--cluster needs a value')
  })

  it('--version prints the version rather than a usage page', async () => {
    const { out } = await cli(['--version'])
    expect(out).not.toContain('Usage:')
    expect(out.trim().length).toBeGreaterThan(0)
  })
})

describe('whoami does not need a cluster', () => {
  it('answers with two clusters reachable and none named', async () => {
    const { out, err } = await cli(['whoami'])
    expect(err).toBe('')
    expect(JSON.parse(out).role).toBe('tenant')
  })

  it('and what it learned decides what the help page advertises', async () => {
    await cli(['whoami'])
    const { out } = await cli(['--help'])
    // A tenant key reaches no admin resource, and saying otherwise costs a
    // round trip per attempt.
    expect(out).not.toContain('admin-teams')
    expect(out).not.toContain('admin-namespaces')
    expect(out).not.toContain('admin-api-keys')
    // But the template catalog is not admin-only, and hiding it would tell a
    // tenant that templates cannot be read at all.
    expect(out).toContain('templates')
    const doc = JSON.parse((await cli(['agent-context'])).out) as { roots: string[] }
    expect(doc.roots).toContain('templates')
    expect(doc.roots.some((r) => r.startsWith('admin-'))).toBe(false)
  })
})

describe('a console link is only offered where a page exists', () => {
  it('the cluster list has no view link, because the console has no such page', async () => {
    const { out } = await cli(['clusters'])
    expect(out).toContain('prod-foo')
    expect(out).not.toContain('view:')
    expect(out).not.toContain('/clusters\n')
  })
})

describe('--editable is the body a write takes, not the object', () => {
  it('prints the projection, and only the projection', async () => {
    const { out } = await cli(['envs', 'demo-env', '--cluster', 'prod-foo', '--json', '--editable'])
    const body = JSON.parse(out) as Record<string, unknown>
    // The fields a create/update carries…
    expect((body.templateRef as Record<string, unknown>).name).toBe('demo-e2b-typescript')
    expect(body.overrides).toEqual({ gateway: { enabled: true } })
    // …and none of the fields that only exist on the read side, because a file
    // carrying those is not one `apply -f` takes.
    expect('name' in body).toBe(false)
    expect('status' in body).toBe(false)
    expect('envDocs' in body).toBe(false)
  })

  it('refuses when the deployment has no such projection yet', async () => {
    // A template's get has no editable body: the catalog is read-only.
    const { code, err } = await cli([
      'templates',
      'demo-e2b-typescript',
      '--cluster',
      'prod-foo',
      '--json',
      '--editable',
    ])
    expect(code).toBe(1)
    expect(err).toContain('no editable body')
    // And says what NOT to do, because the wrong file is destructive.
    expect(err).toContain('NOT')
  })

  it('refuses on a collection address, where there is no one object', async () => {
    const { code, err } = await cli(['envs', '--cluster', 'prod-foo', '--editable'])
    expect(code).toBe(1)
    expect(err).toContain('ONE object')
  })
})

describe('-f takes JSON, and says which format it wanted', () => {
  it('refuses a YAML file by name, not with a parser error', async () => {
    const file = join(workdir, 'env.yaml')
    await Bun.write(file, 'name: demo-env\ntemplateRef:\n  name: tmpl\n')
    const { code, err } = await cli(['create', 'envs', '-f', file, '--cluster', 'prod-foo'])
    expect(code).toBe(1)
    expect(err).toContain('takes JSON, not YAML')
    // The message has to name the way out, not just the mistake — and the way
    // out is the file a write takes, which `--editable` prints.
    expect(err).toContain('--editable')
  })
})

describe('--filter belongs to a list', () => {
  it('a list takes a filter', async () => {
    const { code, out } = await cli(['envs', '--cluster', 'prod-foo', '--filter', 'mode=WarmPool'])
    // The list itself 404s in this stub unless envs are stubbed; what matters
    // here is that the FILTER was not what refused it.
    expect(out + '').not.toContain('narrows a list')
    expect([0, 1]).toContain(code)
  })

  it('one object does not', async () => {
    const { code, err } = await cli([
      'envs',
      'demo-env',
      '--cluster',
      'prod-foo',
      '--filter',
      'mode=WarmPool',
    ])
    expect(code).toBe(1)
    expect(err).toContain('narrows a list')
  })
})

describe('direct mode answers for one cluster and says so', () => {
  it('refuses a cluster this address does not serve', async () => {
    process.env.AGENTBOX_CLUSTER_API = base
    const { code, err } = await cli(['envs', '--cluster', 'prod-bar'])
    expect(code).toBe(1)
    // Reading `isLocal` instead of `local` made this guard inert: with two
    // clusters in the routing table the command succeeded, and answered with
    // THIS cluster's rows under the other one's name.
    expect(err).toContain('serves cluster')
   expect(err).toContain('prod-foo')
  })
})

describe('the write page is about the file, not the resource', () => {
  it('a create page carries the address, the example and the fields', async () => {
    const { out } = await cli(['create', 'envs', '--help'])
    expect(out).toContain('abx create envs -f FILE')
    expect(out).toContain('abx update envs <env> -f FILE')
    // The example and the field table are generated from the API schema, so a
    // field the API has cannot be missing from the page a caller writes from.
    expect(out).toContain('"templateRef"')
    expect(out).toContain('name  string  (required)')
    expect(out).toContain('fixed at create')
    // …and the trap this page exists to close, said out loud.
    expect(out).toContain('is NOT a file a write takes')
  })

  it('a nested address documents the child, not the parent', async () => {
    const { out } = await cli(['create', 'envs', 'demo', 'pools', '--help'])
    expect(out).toContain('writes to pools')
    expect(out).toContain('abx create envs <env> pools -f FILE')
    expect(out).toContain('instanceType')
    // The env's file is not the pool's file, and a page that answered with the
    // env's fields would be the same failure it is meant to fix.
    expect(out).not.toContain('WarmPool | OnDemandJob')
  })

  it('a missing -f answers with the file, before any question about clusters', async () => {
    const { code, err } = await cli(['create', 'envs'])
    expect(code).toBe(1)
    expect(err).toContain('create needs -f FILE')
    expect(err).toContain('fields: name, templateRef')
    expect(err).not.toContain('cluster')
  })

  it('an update says how to get a file that will be accepted', async () => {
    const { err } = await cli(['update', 'envs', 'demo'])
    expect(err).toContain('update needs -f FILE')
    // The three commands that produce one, because "send the whole object and
    // a field you omit is deleted" is a rule nobody follows from prose alone.
    expect(err).toContain('abx envs <env> --editable > FILE')
    expect(err).toContain('abx update envs <env> -f FILE')
  })

  it('an update of something that is not there points at create', async () => {
    await Bun.write(join(workdir, 'missing.json'), '{"templateRef":{"name":"t"}}')
    const { code, err } = await cli([
      'update', 'envs', 'does-not-exist', '-f', join(workdir, 'missing.json'), '--cluster', 'prod-foo',
    ])
    expect(code).toBe(1)
    expect(err).toContain('does-not-exist')
    // Not "check the name" — the name is right, the verb assumed an upsert.
    expect(err).toContain('update changes an object that exists')
    expect(err).toContain('abx create envs -f FILE')
  })
})

/**
 * What a pool body has to contain is the env's to say, and an agent writing one
 * has to be told before it sends it. The stub reports the env's `poolSizing`,
 * so these run the real command against a real (stub) server.
 */
describe('a pool body is checked against the env that will receive it', () => {
  const write = async (body: Record<string, unknown>, verb = 'create') => {
    const file = join(workdir, `pool-${Math.random().toString(36).slice(2)}.json`)
    await Bun.write(file, JSON.stringify(body))
    // An update names the one pool it changes; a create writes the collection.
    const address = verb === 'update' ? ['envs', 'demo-env', 'pools', POOL.name] : ['envs', 'demo-env', 'pools']
    return await cli([verb, ...address, '-f', file, '--cluster', 'prod-foo'])
  }

  const inline = { inlineResources: { requests: { cpu: '1', memory: '2Gi' } } }
  const quota = { labels: { 'quota.scitix.ai/url': 'alice.42.team-a.ondemand' } }

  beforeEach(() => {
    poolSizing = undefined
    poolWrites = []
    requests.length = 0
  })

  it('tells a reader what the pools here need, before they write one', async () => {
    poolSizing = 'billed'
    const { out } = await cli(['envs', 'demo-env', 'pools', '--cluster', 'prod-foo'])
    expect(out).toContain('pools here are billed')
    expect(out).toContain('quota.scitix.ai/url')
  })

  it('says nothing when the deployment states no rule', async () => {
    poolSizing = 'either'
    const { out } = await cli(['envs', 'demo-env', 'pools', '--cluster', 'prod-foo'])
    expect(out).not.toContain('pools here are')
  })

  it('shows the rule on the env detail page, where the choice is made', async () => {
    poolSizing = 'free-form'
    const { out } = await cli(['envs', 'demo-env', '--cluster', 'prod-foo'])
    expect(out).toMatch(/^poolSizing\s+free-form$/m)
  })

  it('refuses a billed env pool that names no quota, without sending it', async () => {
    poolSizing = 'billed'
    const { code, err } = await write({ instanceType: 'sci.c23-2' })
    expect(code).toBe(1)
    expect(err).toContain('bills its pools')
    expect(err).toContain('quota.scitix.ai/url')
    // Refused before the wire: a 400 would not name the field that is missing.
    expect(requests.filter((r) => r.startsWith('POST'))).toHaveLength(0)
    expect(poolWrites).toHaveLength(0)
  })

  it('refuses a billed env pool that names no instance type', async () => {
    poolSizing = 'billed'
    const { code, err } = await write({ ...quota, ...inline })
    expect(code).toBe(1)
    expect(err).toContain('per instance type')
    expect(requests.filter((r) => r.startsWith('POST'))).toHaveLength(0)
  })

  it('sends the body a billed env does accept', async () => {
    poolSizing = 'billed'
    const { code } = await write({ instanceType: 'sci.c23-2', multiplier: 1, ...quota })
    expect(code).toBe(0)
    expect(poolWrites).toHaveLength(1)
  })

  it('refuses an instance type on an env that is not billed', async () => {
    poolSizing = 'free-form'
    const { code, err } = await write({ ...inline, instanceType: 'sci.c23-2' })
    expect(code).toBe(1)
    expect(err).toContain('take no instance type')
    expect(requests.filter((r) => r.startsWith('POST'))).toHaveLength(0)
  })

  it('refuses a quota label on an env that is not billed', async () => {
    poolSizing = 'free-form'
    const { code, err } = await write({ ...inline, ...quota })
    expect(code).toBe(1)
    expect(err).toContain('declare no quota')
  })

  it('sends the free-form body an unbilled env accepts', async () => {
    poolSizing = 'free-form'
    const { code } = await write(inline)
    expect(code).toBe(0)
    expect(poolWrites).toHaveLength(1)
  })

  it('stays out of the way on a server too old to state a rule', async () => {
    // No poolSizing at all: the server decides, and the CLI must not invent a
    // rule of its own — this is the shape an unstamped deployment answers.
    const { code } = await write({ ...inline, instanceType: 'sci.c23-2' })
    expect(code).toBe(0)
    expect(poolWrites).toHaveLength(1)
  })

  it('does not pre-check an update, where the shape cannot change', async () => {
    // A pool created before its template was billed: re-checking on update
    // would make it impossible to resize.
    poolSizing = 'billed'
    const { code, err } = await write({ ...inline, replicas: 3 }, 'update')
    expect(code).toBe(0)
    expect(err).toBe('')
  })
})
