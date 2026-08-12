// The per-user workspace path is written out in five languages and only one of
// them can be type-checked against the source: TypeScript imports the constant,
// while Python, bash, Dockerfile and Go each carry the literal. Nothing at
// runtime notices when one of them drifts — a mismatch shows up as a failed turn
// (the gateway mkdir'ing one path while the fs server 400s the other), which is
// how this last went wrong.
//
// So the drift check is here: pin the value, and assert every copy in this
// package agrees with it and no longer mentions the path it replaced.
import {
  USER_DIR_ROOT,
  userDirectory,
} from './workspace.ts'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const PACKAGE_ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
const PREVIOUS_ROOT = '/home/opencode/u'

const read = (rel: string) => readFileSync(join(PACKAGE_ROOT, rel), 'utf8')

describe('the per-user workspace path', () => {
  it('is a fixed path that names neither the harness nor the product', () => {
    expect(USER_DIR_ROOT).toBe('/home/agents/u')
    expect(userDirectory('alice')).toBe('/home/agents/u/alice')
  })

  it('is not configurable, so nothing can honour it only halfway', () => {
    // It WAS an env var that most consumers ignored: setting it moved the
    // gateway and left the fs server rejecting the new path. Keep it inert.
    process.env.AGENTBOX_USER_DIR_ROOT = '/somewhere/else'
    try {
      expect(userDirectory('alice')).toBe('/home/agents/u/alice')
    } finally {
      delete process.env.AGENTBOX_USER_DIR_ROOT
    }
  })

  // Each of these creates or accepts the very same directory, and none of them can
  // import the constant, so a partial rename breaks a user's turn rather than any
  // one component. The list is this repository's consumers: it shrank when the
  // gateway was ported here and the origin deployment's own consumers stayed behind,
  // which is the whole reason the check is a list and not a comment.
  it.each([
    [
      '../sdk/hands/python/agentbox_hands/fs.py',
      'the workspace file API accepts only this root',
    ],
    ['entrypoint.sh', 'the root is mkdir-ed at container start'],
  ])('is the same path in %s (%s)', rel => {
    const content = read(rel)
    expect(content).toContain(USER_DIR_ROOT)
    expect(content).not.toContain(PREVIOUS_ROOT)
  })

  // The Brain image has to pre-create the root, owned by the runtime user, because
  // every process that makes a user's directory inside it is unprivileged. There is
  // no image here yet, so this records the requirement rather than pretending to
  // check it.
  it.skip('is the same path in the Brain image (pre-created and chowned)', () => {})
})
