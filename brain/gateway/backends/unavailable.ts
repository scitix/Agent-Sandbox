// The stand-in for "this pod can serve no harness at all".
//
// WHY THE POD STAYS UP. The gateway used to refuse to bind when every harness
// failed preflight, on the reasoning that a pod which accepts traffic and then
// fails every turn is worse than one that never becomes ready. That is true of
// the TURNS and false of everything else in the pod: the entrypoint kills its
// siblings when any child exits, so one missing model credential also took down
// the workspace-fs server, which serves attachment upload and the file browser —
// of which touch a model. And the diagnosis it produced was the worst kind: a
// CrashLoopBackOff whose reason is only in a log nobody is tailing.
//
// So the rule is now the same at every scale: a harness that cannot serve is
// reported unavailable WITH its reason, whether it is one of them or all of them.
// Read paths degrade to empty (no models, no history — there is genuinely none),
// and anything that would need a harness fails immediately with the collected
// reasons, which is what puts the misconfiguration in front of the person who can
// fix it instead of in a restart loop.
import type {
  AgentBackend,
  AgentEvent,
  BackendCapabilities,
  BackendId,
  ModelInfo,
  SandboxBinding,
  ThreadInfo,
  TranscriptEntry,
} from '../backend.ts'

export class UnavailableBackend implements AgentBackend {
  /**
   * @param id     The harness the deployment ASKED for. Reported as-is so
   *               `/capabilities` names the intended harness rather than
   *               inventing a fourth id the browser has never heard of.
   * @param reason Every configured harness and why it is not serving.
   */
  constructor(
    readonly id: BackendId,
    private readonly reason: string
  ) {}

  /** Everything off: the browser hides each affordance instead of offering one
   *  that would fail. */
  readonly capabilities: BackendCapabilities = {
    interaction: false,
    threadList: false,
    fork: false,
    rename: false,
    compaction: false,
    transcriptExport: false,
    reasoningStream: false,
  }

  /** Moot — it never runs a turn, so no IO can land anywhere. Not 'none', which
   *  the registry reads as "this backend's file and shell IO may hit the pod"
   *  and which is a claim about a real harness. This object never passes through
   *  the registry's admission check at all. */
  readonly sandboxing: SandboxBinding = 'mcp'

  private fail(): never {
    throw new Error(`no agent harness is available: ${this.reason}`)
  }

  async preflight(): Promise<void> {
    this.fail()
  }

  /** Empty, not an error: the composer's model list is a read, and an empty
   *  dropdown next to a visible reason beats a failed request. */
  async models(): Promise<ModelInfo[]> {
    return []
  }

  async listThreads(): Promise<ThreadInfo[]> {
    return []
  }

  async createThread(): Promise<string> {
    this.fail()
  }

  async forkThread(): Promise<string> {
    this.fail()
  }

  async renameThread(): Promise<void> {
    this.fail()
  }

  async deleteThread(): Promise<void> {
    this.fail()
  }

  async exportThread(): Promise<TranscriptEntry[]> {
    this.fail()
  }

  run(): AsyncIterable<AgentEvent> {
    this.fail()
  }

  async interrupt(): Promise<void> {
    this.fail()
  }

  async answer(): Promise<void> {
    this.fail()
  }
}
