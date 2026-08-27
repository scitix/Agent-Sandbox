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

// Bounding calls that have no timeout of their own.
//
// The gateway talks to three things it does not control — a harness SDK, a model
// endpoint, and the sandbox daemon — and none of their clients time out by
// default. A `.catch()` only covers the case where they FAIL; the case that
// actually hurts is the one where they never answer at all, because every caller
// above is then stuck with no error to report and no state to move on from. Two
// production incidents have had that shape (`GET /threads` fanning out to a hung
// harness; a preflight that never returned, so the pod never became ready), so
// the rule is now explicit: an outbound call on a startup or request path gets a
// deadline.

/**
 * `work`, or `fallback` if it has not settled within `ms`.
 *
 * The loser is abandoned rather than cancelled — there is usually nothing to
 * cancel an SDK call with, and a late answer is simply discarded. Use it where
 * a missing answer has a sane substitute; where it does not, use
 * {@link attempt} and act on the failure.
 */
export async function withTimeout<T>(
  work: Promise<T>,
  ms: number,
  fallback: T
): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined
  try {
    return await Promise.race([
      work,
      new Promise<T>(resolve => {
        timer = setTimeout(() => resolve(fallback), ms)
      }),
    ])
  } finally {
    if (timer) clearTimeout(timer)
  }
}

/** The outcome of a bounded call: the value, or why there isn't one. A timeout
 *  and a rejection are different facts and a retry loop wants to report both. */
export type Attempt<T> =
  | { ok: true; value: T }
  | { ok: false; timedOut: boolean; reason: string }

/** Run `work` with a deadline, reporting failure instead of throwing. */
export async function attempt<T>(
  work: () => Promise<T>,
  ms: number
): Promise<Attempt<T>> {
  const TIMED_OUT = Symbol('timed-out')
  try {
    const result = await withTimeout(
      work().then(value => ({ value })),
      ms,
      TIMED_OUT as unknown as { value: T }
    )
    if ((result as unknown) === TIMED_OUT) {
      return { ok: false, timedOut: true, reason: `no answer within ${ms}ms` }
    }
    return { ok: true, value: result.value }
  } catch (e) {
    return {
      ok: false,
      timedOut: false,
      reason: e instanceof Error ? e.message : String(e),
    }
  }
}
