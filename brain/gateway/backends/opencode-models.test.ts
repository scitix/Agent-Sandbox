// Which models the OpenCode harness offers in the composer's picker.
//
// Worth its own test because the failure mode is a DATA LEAK that looks like a
// feature: OpenCode reports its own hosted "opencode" (Zen) free models as
// connected — they need no credentials — so filtering on `connected` alone puts
// a third-party endpoint in the picker, and every prompt sent to it carries this
// deployment's cluster data off-site. What opencode.json declares is the filter.
import { describe, expect, it } from 'vitest'

import { pickModels } from './opencode.ts'

const ZEN = {
  id: 'opencode',
  name: 'OpenCode Zen',
  models: { 'big-pickle': { id: 'big-pickle', name: 'Big Pickle' } },
}

const SCITIX = {
  id: 'scitix',
  name: 'ScitiX',
  models: { 'glm-5.2': { id: 'glm-5.2', name: 'GLM 5.2' } },
}

describe('pickModels', () => {
  it('drops a connected provider the config never declared', () => {
    expect(
      pickModels([ZEN, SCITIX], ['opencode', 'scitix'], ['scitix'])
    ).toEqual([{ id: 'scitix/glm-5.2', name: 'GLM 5.2 (ScitiX)' }])
  })

  it('falls back to connected when the config declares nothing', () => {
    expect(pickModels([ZEN, SCITIX], ['scitix'], null).map(m => m.id)).toEqual([
      'scitix/glm-5.2',
    ])
  })

  it('leaves the list unfiltered when the server reports neither', () => {
    expect(pickModels([ZEN, SCITIX], [], null)).toHaveLength(2)
  })

  it('ignores a declared provider the server does not know', () => {
    expect(pickModels([ZEN], ['opencode'], ['scitix'])).toEqual([])
  })
})
