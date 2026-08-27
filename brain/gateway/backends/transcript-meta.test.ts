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

// Per-message metadata on an exported transcript.
//
// The defect this guards is silent by construction: an export that omits the
// timestamp / model / usage still renders a perfectly good-looking conversation
// after a reload — just with the send time, the model name and the token+cost
// figures quietly gone, on a thread the user was reading a second earlier. So
// what is asserted here is mostly ABSENCE handling: a missing field must stay
// missing rather than become 0, "undefined" or a plausible-but-wrong value.
import { describe, expect, it } from 'vitest'

import { transcriptMeta } from './claude-code.ts'

describe('transcriptMeta', () => {
  it('reads the model and usage out of the raw Anthropic message', () => {
    expect(
      transcriptMeta({
        timestamp: '2026-07-31T04:12:00.000Z',
        message: {
          model: 'claude-sonnet-4-5',
          usage: {
            input_tokens: 120,
            output_tokens: 34,
            cache_read_input_tokens: 8,
            cache_creation_input_tokens: 2,
          },
        },
      })
    ).toEqual({
      timestamp: '2026-07-31T04:12:00.000Z',
      model: 'claude-sonnet-4-5',
      usage: {
        inputTokens: 120,
        outputTokens: 34,
        cacheReadTokens: 8,
        cacheCreationTokens: 2,
      },
    })
  })

  it('omits what is not there rather than inventing a zero', () => {
    // A user turn has no model and no usage; emitting `model: undefined` or a
    // zero-filled usage block would render as a real (wrong) stats row.
    expect(transcriptMeta({ message: { content: 'hi' } })).toEqual({})
  })

  it('keeps a partial usage block partial', () => {
    const out = transcriptMeta({ message: { usage: { input_tokens: 5 } } })
    expect(out.usage).toEqual({
      inputTokens: 5,
      outputTokens: undefined,
      cacheReadTokens: undefined,
      cacheCreationTokens: undefined,
    })
    expect(out.model).toBeUndefined()
  })

  it('ignores a non-string timestamp and a non-string model', () => {
    // The SDK's SessionMessage type does not declare `timestamp`, so it is read
    // defensively off the raw entry — a surprise shape must not reach the UI.
    expect(
      transcriptMeta({ timestamp: 1730000000, message: { model: 42 } })
    ).toEqual({})
  })

  it('survives a message that is missing entirely', () => {
    expect(transcriptMeta({})).toEqual({})
  })
})
