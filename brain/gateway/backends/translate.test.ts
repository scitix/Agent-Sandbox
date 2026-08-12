// The SDK-message → wire-event translation.
//
// Worth its own test because two of its properties are invisible when wrong:
//   * the MODEL is named only on assistant messages while the turn ends on the
//     result message, so it has to be carried across messages. Losing it blanks
//     the per-message model row in the UI and leaves Langfuse unable to attribute
//     cost to a model — nothing errors, the numbers just have no label.
//   * a failed turn must emit BOTH turn-end (so the UI stops spinning) and error
//     (so the user sees why). Emitting only one of them looks fine in the happy
//     path and hangs or goes silent in the unhappy one.
import { describe, expect, it } from 'vitest'

import { translate } from './claude-code.ts'
import { isAgentPart } from './opencode.ts'

// Minimal stand-ins for the SDK's message shapes; only the fields translate reads.
function assistantMessage(model: string, content: unknown[] = []) {
  return { type: 'assistant', message: { model, content } } as never
}

function resultMessage(subtype: string, cost = 0.01) {
  return {
    type: 'result',
    subtype,
    total_cost_usd: cost,
    usage: {
      input_tokens: 10,
      output_tokens: 3,
      cache_read_input_tokens: 4,
      cache_creation_input_tokens: 5,
    },
  } as never
}

describe('translate', () => {
  it('carries the model from an assistant message onto turn-end', () => {
    const seen: { model?: string } = {}
    translate(assistantMessage('claude-sonnet-5'), seen)
    const [end] = translate(resultMessage('success'), seen)

    expect(end).toMatchObject({
      t: 'turn-end',
      model: 'claude-sonnet-5',
      costUsd: 0.01,
      usage: {
        inputTokens: 10,
        outputTokens: 3,
        cacheReadTokens: 4,
        cacheCreationTokens: 5,
      },
    })
  })

  it('does not invent a model when no assistant message named one', () => {
    const [end] = translate(resultMessage('success'), {})
    expect(end).toMatchObject({ t: 'turn-end' })
    expect((end as { model?: string }).model).toBeUndefined()
  })

  it('reports a failed turn as both an end and an error', () => {
    const events = translate(resultMessage('error_during_execution'), {})
    expect(events.map(e => e.t)).toEqual(['turn-end', 'error'])
  })

  it('emits tool-end with whole args, since streamed arg deltas carry no id', () => {
    const events = translate(
      assistantMessage('claude-sonnet-5', [
        { type: 'tool_use', id: 'tu_1', input: { command: 'ls' } },
      ]),
      {}
    )
    expect(events).toEqual([
      { t: 'tool-end', id: 'tu_1', args: { command: 'ls' } },
    ])
  })

  it('turns a tool_result block into a tool-result, stringifying rich content', () => {
    const events = translate(
      {
        type: 'user',
        message: {
          content: [
            {
              type: 'tool_result',
              tool_use_id: 'tu_1',
              content: [{ type: 'text', text: 'file.txt' }],
              is_error: false,
            },
          ],
        },
      } as never,
      {}
    )
    expect(events[0]).toMatchObject({ t: 'tool-result', id: 'tu_1' })
    expect((events[0] as { content: string }).content).toContain('file.txt')
  })

  it('streams text and thinking deltas as separate event kinds', () => {
    const text = translate(
      {
        type: 'stream_event',
        event: {
          type: 'content_block_delta',
          delta: { type: 'text_delta', text: 'hi' },
        },
      } as never,
      {}
    )
    const thinking = translate(
      {
        type: 'stream_event',
        event: {
          type: 'content_block_delta',
          delta: { type: 'thinking_delta', text: 'hmm' },
        },
      } as never,
      {}
    )
    expect(text).toEqual([{ t: 'text', delta: 'hi' }])
    expect(thinking).toEqual([{ t: 'thinking', delta: 'hmm' }])
  })
})

// OpenCode's event stream is a server-wide firehose and its part updates cover the
// USER message as well as the agent's. Emitting those replayed the prompt back as
// assistant text — visibly, because the `<page …/>` marker the gateway appends for
// the model is stripped from the browser's own bubble — and mis-anchored the tool
// cards, since the wire attaches a tool call to whichever text message is open.
// A reload hid both, because the transcript is read per role.
describe('isAgentPart', () => {
  const userMsg = 'msg_user'
  const agentMsg = 'msg_agent'
  const roles = new Map([
    [userMsg, 'user'],
    [agentMsg, 'assistant'],
  ])

  it('drops a part belonging to the prompt', () => {
    expect(isAgentPart({ messageID: userMsg }, roles)).toBe(false)
  })

  it('keeps a part belonging to the agent', () => {
    expect(isAgentPart({ messageID: agentMsg }, roles)).toBe(true)
  })

  it('keeps a part whose message it has not seen announced', () => {
    // Fails OPEN on purpose: roles arrive first in practice, so an unknown id
    // means that assumption broke — and echoing beats swallowing the answer.
    expect(isAgentPart({ messageID: 'msg_unannounced' }, roles)).toBe(true)
    expect(isAgentPart({}, roles)).toBe(true)
  })
})
