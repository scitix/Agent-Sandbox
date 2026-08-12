// Preflight gives up on a server that listens but never answers.
//
// The failure this guards against took the pod down for minutes at a time and
// left almost no evidence. `opencode serve` binds its port BEFORE it has
// bootstrapped its first instance; the old readiness check was a bare
// `connect()`, so it passed the moment the socket accepted. The gateway then
// issued its first real request into a server that could not yet reply — and
// because neither the SDK call nor the loop around it had a deadline, preflight
// simply never returned. No error, no log line: the gateway process sat in the
// event loop, :4099 was never bound, the startup probe got connection-refused
// for its full 180s budget, and the kubelet killed and restarted the container.
// On a fast node OpenCode bootstraps in a second and nobody sees it; on a slower
// one it is a restart loop.
//
// Two properties are asserted, because the fix is both of them:
//   * readiness is a REAL call, so "listening" alone does not satisfy it;
//   * it is BOUNDED, so an unready harness is reported unavailable (registry.ts
//     then serves the other one) instead of taking the pod with it.
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { describe, expect, it, vi } from 'vitest'

import { InteractionRegistry } from '../interactions.ts'
import { ThreadStore } from '../threads.ts'
import { OpenCodeBackend } from './opencode.ts'

const store = () =>
  new ThreadStore(join(mkdtempSync(join(tmpdir(), 'oc-pre-')), 'threads.json'))

function backend(readyTimeoutMs: number) {
  return new OpenCodeBackend(
    {
      baseUrl: 'http://127.0.0.1:1',
      readyTimeoutMs,
    } as unknown as ConstructorParameters<typeof OpenCodeBackend>[0],
    store(),
    new InteractionRegistry(50)
  )
}

/** Never settles — the shape of a request into a server that is bootstrapping. */
const forever = () => new Promise<never>(() => {})

describe('opencode preflight', () => {
  it('fails within its deadline when the server never answers', async () => {
    const oc = backend(7_000)
    // The server is up enough to accept connections; it just cannot serve. That
    // is exactly the window a `connect()` probe cannot see.
    const models = vi.spyOn(oc, 'models').mockImplementation(forever)

    const started = Date.now()
    await expect(oc.preflight()).rejects.toThrow(/was not ready within/)
    const elapsed = Date.now() - started

    // Bounded BY the deadline, not merely near it: a probe is capped at what is
    // left of the budget, so no single attempt can overrun it.
    expect(elapsed).toBeGreaterThanOrEqual(6_500)
    expect(elapsed).toBeLessThan(9_000)
    // Retried rather than waited out in one go, so a server that becomes ready
    // part-way through is picked up.
    expect(models.mock.calls.length).toBeGreaterThan(1)
  }, 20_000)

  it('proceeds as soon as the server actually answers', async () => {
    const oc = backend(10_000)
    let attempts = 0
    vi.spyOn(oc, 'models').mockImplementation(async () => {
      attempts++
      // Unready twice, then ready — the real cold-start sequence.
      if (attempts < 3) return forever()
      return [{ id: 'p/m', name: 'model' }]
    })

    // Past readiness it fails on the sandbox daemon, which is not running here.
    // That is the assertion: it got past readiness at all, and it did so by
    // waiting for a real answer rather than for a socket.
    await expect(oc.preflight()).rejects.toThrow(/sandbox daemon unreachable/)
    expect(attempts).toBe(3)
  }, 30_000)

  // The overseas-only failure: the server answers immediately, with nothing,
  // because it has not finished resolving its providers. Treating that first
  // answer as final withdrew OpenCode for the life of the pod — and, when the
  // other harness was also unconfigured, used to take the pod down with it.
  it('keeps waiting when the server answers with no models yet', async () => {
    const oc = backend(10_000)
    let attempts = 0
    vi.spyOn(oc, 'models').mockImplementation(async () => {
      attempts++
      // Empty twice — a successful call, just not a ready one — then populated.
      return attempts < 3 ? [] : [{ id: 'scitix/m', name: 'model' }]
    })

    // Past readiness, so the empty answers were retried rather than believed.
    await expect(oc.preflight()).rejects.toThrow(/sandbox daemon unreachable/)
    expect(attempts).toBe(3)
  }, 30_000)

  it('reports a provider misconfiguration once the budget is spent', async () => {
    const oc = backend(3_000)
    // Never populated: this is the real "no provider configured" case, and it
    // must still be reported — with a message about the provider, not about
    // readiness, since the server answered every single time.
    vi.spyOn(oc, 'models').mockResolvedValue([])

    await expect(oc.preflight()).rejects.toThrow(
      /still reports no models after 3000ms/
    )
  }, 20_000)
})
