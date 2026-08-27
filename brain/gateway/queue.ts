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

// A single-consumer async queue.
//
// Needed because a backend produces events from two places at once: the harness's
// message iterator, and callbacks the harness invokes (a permission prompt, a
// hook). Both have to land in one ordered stream, and the callback cannot `yield`.

export class AsyncQueue<T> {
  private readonly items: T[] = []
  private waiting: ((v: IteratorResult<T>) => void) | null = null
  private closed = false

  push(item: T): void {
    if (this.closed) return
    const w = this.waiting
    if (w) {
      this.waiting = null
      w({ value: item, done: false })
      return
    }
    this.items.push(item)
  }

  /**
   * Would a `push` be discarded?
   *
   * `push` on a closed queue is deliberately silent — a producer racing its own
   * shutdown should not throw. That makes this the only way a CALLER can tell that
   * what it handed over went nowhere, which matters for anything a user can see: a
   * mid-turn message pushed into a turn that has just ended would otherwise leave a
   * pending chip that never resolves.
   */
  get isClosed(): boolean {
    return this.closed
  }

  /** No more items will be pushed; the consumer drains what is left and ends. */
  close(): void {
    if (this.closed) return
    this.closed = true
    const w = this.waiting
    if (w) {
      this.waiting = null
      w({ value: undefined as unknown as T, done: true })
    }
  }

  async *[Symbol.asyncIterator](): AsyncGenerator<T> {
    for (;;) {
      if (this.items.length) {
        yield this.items.shift() as T
        continue
      }
      if (this.closed) return
      const next = await new Promise<IteratorResult<T>>(resolve => {
        this.waiting = resolve
      })
      if (next.done) return
      yield next.value
    }
  }
}
