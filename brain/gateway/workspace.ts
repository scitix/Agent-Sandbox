// Where the agent's per-user workspace lives.
//
// One fixed path, because it has to be the same string in five places at once:
// the gateway hands it to whichever harness runs the turn, the browser posts it
// with every attachment and workspace listing, the workspace-fs server accepts
// paths only under it, the sandbox creates it as the agent's cwd, and the image
// pre-creates it so an unprivileged process can mkdir a user's dir inside it.
//
// It is deliberately NOT configurable. It was a variable once, and that was worse
// than a constant: most consumers ignored it, so setting it moved the gateway while
// the file API went on rejecting the new path with a 400 and the browser went on
// posting the old one. A knob only half the consumers honour is a trap, and nothing
// about a deployment wants this path to differ.
//
// The name mentions no harness and no vendor: it is visible to the model as its cwd,
// and the agent has no business reading which harness is running it off a directory
// name.
//
// Consumers that cannot import this module carry the literal and have to change with
// it — nothing at runtime catches the drift:
//   sdk/hands/python/agentbox_hands/fs.py    ROOT (ALLOWED derives from it)
//   brain/entrypoint.sh                      mkdir of the root
//   the Brain image                          mkdir + chown of the root
export const USER_DIR_ROOT = '/home/agents/u'

/**
 * The per-user identity directory: the agent's cwd, the harness's session
 * namespace, and the key attachment staging is filed under. All three must agree
 * or a staged file flushes into a directory the agent never looks at.
 */
export function userDirectory(userKey: string): string {
  return `${USER_DIR_ROOT}/${userKey}`
}
