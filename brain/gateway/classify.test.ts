// Classifier semantics.
//
// This feature is invisible when it breaks: a wrong verdict just means the "move
// this to a new session" prompt does not appear, or appears when it should not.
// So the two rules that make it correct are pinned here — the verdict is DERIVED
// from the two axes rather than trusted from the model, and everything fails safe
// to "continuation".
import { describe, expect, it } from 'vitest'

import {
  CLASSIFIER_SYSTEM_PROMPT,
  classifierUserMessage,
  createClassifier,
  parseVerdict,
  stripPageMarkers,
} from './classify.ts'
import type { OneShotModel, OneShotResult } from './model/one-shot.ts'

function stub(result: Partial<OneShotResult>): OneShotModel {
  return {
    model: 'stub',
    async complete() {
      return {
        content: '',
        model: 'stub',
        latencyMs: 1,
        ...result,
      }
    },
  }
}

describe('verdict derivation', () => {
  it('needs BOTH axes to change to call it a new topic', () => {
    expect(
      parseVerdict(
        '{"objectCarriesOver":false,"goalCarriesOver":false,"confidence":0.9}'
      ).isNewTopic
    ).toBe(true)
    expect(
      parseVerdict(
        '{"objectCarriesOver":true,"goalCarriesOver":false,"confidence":0.9}'
      ).isNewTopic
    ).toBe(false)
    expect(
      parseVerdict(
        '{"objectCarriesOver":false,"goalCarriesOver":true,"confidence":0.9}'
      ).isNewTopic
    ).toBe(false)
  })

  // Models do contradict themselves on the conjunction: they report that the
  // object carried over and still set isNewTopic. The axes win.
  it('overrides the model when it contradicts its own axes', () => {
    const v = parseVerdict(
      '{"objectCarriesOver":true,"goalCarriesOver":true,"isNewTopic":true,"confidence":1}'
    )
    expect(v.isNewTopic).toBe(false)
  })

  it('falls back to isNewTopic only when no axis is reported', () => {
    expect(
      parseVerdict('{"isNewTopic":true,"confidence":0.8}').isNewTopic
    ).toBe(true)
  })

  it('tolerates code fences and surrounding prose', () => {
    const v = parseVerdict(
      'Sure!\n```json\n{"objectCarriesOver":false,"goalCarriesOver":false,' +
        '"confidence":0.7,"rationale":"different node, different question"}\n```\n'
    )
    expect(v.isNewTopic).toBe(true)
    expect(v.rationale).toBe('different node, different question')
  })

  it('clamps confidence and survives junk', () => {
    expect(parseVerdict('{"confidence":5}').confidence).toBe(1)
    expect(parseVerdict('{"confidence":-2}').confidence).toBe(0)
    expect(parseVerdict('not json at all').isNewTopic).toBe(false)
    expect(parseVerdict('').isNewTopic).toBe(false)
  })
})

describe('page markers', () => {
  // Walking from a node list to a pod detail is not a change of subject. Leaving
  // the marker in makes BOTH axes look like they moved when only the browser did.
  it('are stripped from context and new input alike', () => {
    const withMarker =
      'why is this pending?\n\n<page key="node_detail" cluster="foo" />'
    expect(stripPageMarkers(withMarker)).toBe('why is this pending?')
    const msg = classifierUserMessage(withMarker, withMarker)
    expect(msg).not.toContain('<page')
  })

  it('collapses the blank lines the removal leaves behind', () => {
    expect(stripPageMarkers('a\n\n<page x="1" />\n\nb')).toBe('a\n\nb')
  })
})

describe('fail-safe behaviour', () => {
  it('reports the feature off when no model is configured', async () => {
    const verdict = await createClassifier(null).classify({ newInput: 'hi' })
    // `enabled:false` is distinct from "on, and this is a continuation": the UI
    // hides the affordance entirely rather than showing one that never fires.
    expect(verdict).toEqual({ enabled: false })
  })

  it('treats a transport error as a continuation, never an exception', async () => {
    const verdict = await createClassifier(
      stub({ error: 'AbortError: timed out' })
    ).classify({ newInput: 'and the node?' })
    expect(verdict.enabled).toBe(true)
    expect(verdict.isNewTopic).toBe(false)
    expect(verdict.confidence).toBe(0)
  })

  it('recovers a verdict a reasoning model left in its chain of thought', async () => {
    // OneShotModel already prefers reasoning_content when content is empty; this
    // asserts the parse still finds the JSON inside prose.
    const verdict = await createClassifier(
      stub({
        content:
          'Let me think. The object changed and the goal changed. ' +
          '{"objectCarriesOver":false,"goalCarriesOver":false,"confidence":0.8}',
      })
    ).classify({ newInput: 'how much quota does team-b have?' })
    expect(verdict.isNewTopic).toBe(true)
  })
})

describe('prompt asset', () => {
  // The prompt is tuned product text; an accidental edit changes verdicts with no
  // test failure anywhere else.
  it('keeps the two-axis contract and the reply shape', () => {
    expect(CLASSIFIER_SYSTEM_PROMPT).toContain('OBJECT --')
    expect(CLASSIFIER_SYSTEM_PROMPT).toContain('GOAL --')
    expect(CLASSIFIER_SYSTEM_PROMPT).toContain(
      'A new topic requires BOTH axes to change'
    )
    expect(CLASSIFIER_SYSTEM_PROMPT).toContain('"objectCarriesOver":boolean')
    expect(CLASSIFIER_SYSTEM_PROMPT).toMatchSnapshot()
  })
})
