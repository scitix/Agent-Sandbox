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
 * Which pod a sandbox chart is actually about.
 *
 * This is asserted rather than eyeballed because the failure is invisible: the
 * join metric is exported by the controller, so joining on `pod` matches the
 * CONTROLLER's container and plots its numbers under the sandbox's id. Nothing
 * errors, no series is empty, and the chart is simply about something else — a
 * sandbox burning 8 cores read 0.007, and it took a direct backend query to
 * see it.
 */

import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { sandboxPodJoin } from '@/app/api/prometheus/_shared'

const ROUTES = join(__dirname, '..', '..', 'app', 'api', 'prometheus')

describe('per-sandbox charts join on the sandbox pod', () => {
  it('rewrites pod from exported_pod', () => {
    const expr = sandboxPodJoin('cluster="c",sandbox_id="s"')
    expect(expr).toContain('label_replace(')
    expect(expr).toContain('"pod"')
    expect(expr).toContain('"exported_pod"')
  })

  it('leaves pod alone when exported_pod is absent', () => {
    // `(.+)` not `(.*)`: on a scrape config that does NOT collide, the metric
    // keeps its own `pod` and there is nothing to rewrite. An empty match would
    // overwrite it with "" and the join would return nothing at all.
    expect(sandboxPodJoin('x="y"')).toContain('"(.+)"')
    expect(sandboxPodJoin('x="y"')).not.toContain('"(.*)"')
  })

  it('no route joins on the raw metric', () => {
    // The bug, stated as an assertion. Any route that writes the join by hand
    // is a route that can drift back to matching the controller.
    for (const dir of readdirSync(ROUTES, { withFileTypes: true })) {
      if (!dir.isDirectory()) continue
      const file = join(ROUTES, dir.name, 'route.ts')
      let src: string
      try {
        src = readFileSync(file, 'utf8')
      } catch {
        continue
      }
      if (!src.includes('group_left(sandbox_id)')) continue
      expect(src, `${dir.name} builds the join by hand`).toContain('sandboxPodJoin(')
      expect(
        src.replace(/sandboxPodJoin\(/g, ''),
        `${dir.name} still references agentbox_sandbox_running_info directly`,
      ).not.toContain('agentbox_sandbox_running_info{')
    }
  })
})
