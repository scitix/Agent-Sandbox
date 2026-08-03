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
 * Hand-written types for the ManagedAgent control-plane resource.
 *
 * ManagedAgent is served by the hub (wsproxy internal API) rather than the
 * per-cluster AgentBox API, so it has no entry in `pkg/openapi/native/openapi.yaml`
 * and therefore no generated counterpart in `lib/api/schema.d.ts`. The shapes here
 * mirror `api/v1alpha1/managedagent_types.go` and its generated CRD schema
 * (`config/crd/bases/agents.navix.sh_managedagents.yaml`), which stay the
 * authority on field names and validation.
 *
 * Pod-level escape hatches (`brain.extraEnv`, tolerations, affinity, resources,
 * scenario `sandboxEnv`) are typed loosely: the console never composes them, it
 * only round-trips whatever the API returns.
 */

// ─── Shared selectors ─────────────────────────────────────────────────────────

export interface ManagedAgentSecretKeySelector {
  name: string
  key: string
}

export interface ManagedAgentConfigMapKeySelector {
  name: string
  key: string
}

/** The harnesses a ManagedAgent can serve. */
export type ManagedAgentRuntimeID = "claude-code" | "opencode"

export const MANAGED_AGENT_RUNTIME_IDS: ManagedAgentRuntimeID[] = ["claude-code", "opencode"]

/** Reasoning effort accepted by the Claude Agent SDK harness. */
export type ManagedAgentEffort = "low" | "medium" | "high" | "xhigh" | "max"

export const MANAGED_AGENT_EFFORTS: ManagedAgentEffort[] = ["low", "medium", "high", "xhigh", "max"]

// ─── Spec ─────────────────────────────────────────────────────────────────────

/**
 * Owner is stamped by the server from the authenticated caller. The console
 * displays it read-only and never sends it.
 */
export interface ManagedAgentOwner {
  team?: string
  user?: string
}

export interface ManagedAgentImage {
  repository: string
  tag?: string
  pullPolicy?: "Always" | "IfNotPresent" | "Never"
  pullSecrets?: { name?: string }[]
}

export interface ManagedAgentModel {
  id: string
  name?: string
  /** Marks a model eligible to back the topic classifier. */
  nonReasoning?: boolean
}

export interface ClaudeCodeRuntime {
  baseURL?: string
  /**
   * Materialised by the server from the `credentials` block of a create/update
   * request. It is present on every response and echoed back unchanged when the
   * console edits an agent without supplying a new key.
   */
  credentialsRef?: ManagedAgentSecretKeySelector
  models?: ManagedAgentModel[]
  defaultModel?: string
  smallModel?: string
  effort?: ManagedAgentEffort
  pluginPaths?: string[]
}

export interface OpenCodeRuntime {
  enabled?: boolean
  port?: number
  baseURL?: string
  credentialsRef?: ManagedAgentSecretKeySelector
  models?: ManagedAgentModel[]
  defaultModel?: string
  /** Bring-your-own opencode.json; bypasses the generated provider allow-list. */
  configSecretRef?: ManagedAgentSecretKeySelector
}

export interface ManagedAgentRuntime {
  default: ManagedAgentRuntimeID
  claudeCode?: ClaudeCodeRuntime
  opencode?: OpenCodeRuntime
}

export interface ManagedAgentClassifier {
  enabled?: boolean
  wire?: "anthropic-messages" | "openai-chat"
  baseURL?: string
  credentialsRef?: ManagedAgentSecretKeySelector
  model?: string
  maxTokens?: number
  maxContextChars?: number
  timeoutSeconds?: number
}

export interface ManagedAgentPrompt {
  inline?: string
  from?: ManagedAgentConfigMapKeySelector
  append?: string
}

export interface ManagedAgentScenario {
  name: string
  displayName?: string
  default?: boolean
  prompt?: ManagedAgentPrompt
  /** Pins the scenario to one harness; empty means the agent-level default. */
  runtime?: ManagedAgentRuntimeID | ""
  model?: string
  /** Tool allow-list. Visibility is deny-by-default: unlisted tools are absent. */
  allow?: string[]
  disable?: string[]
  interactive?: boolean
  sandboxEnv?: { name: string; value?: string }[]
  scalingGroup?: string
  image?: string
  exposed?: boolean
}

export interface HandsEnvRef {
  clusterID?: string
  name: string
  namespace?: string
  scalingGroup?: string
  image?: string
}

export interface HandsInstanceType {
  name: string
  replicas?: number
  minReplicas?: number
  maxReplicas?: number
  default?: boolean
}

export interface HandsAutoSpec {
  clusterID: string
  templateRef: string
  image?: string
  instanceTypes: HandsInstanceType[]
  idleTimeoutSeconds?: number
  startupTimeoutSeconds?: number
}

export interface HandsBinding {
  scope?: "thread" | "user"
  timeoutSeconds?: number
  readyTimeoutSeconds?: number
  attachmentRoot?: string
  maxAttachmentBytes?: number
  workspace?: string
  skipSeed?: boolean
  seedRepo?: string
}

export interface HandsE2B {
  credentialsSecret?: string
  apiURL?: string
  domain?: string
  https?: boolean
}

/**
 * An E2B-compatible sandbox service this control plane does not own. Nothing is
 * reconciled for it: readiness is whatever the remote reports at call time.
 */
export interface HandsExternal {
  apiURL: string
  domain: string
  https?: boolean
  envName: string
  image?: string
  scalingGroup?: string
  credentialsRef?: ManagedAgentSecretKeySelector
}

export interface ManagedAgentHands {
  envRef?: HandsEnvRef
  auto?: HandsAutoSpec
  external?: HandsExternal
  binding?: HandsBinding
  e2b?: HandsE2B
}

/** The three mutually exclusive shapes of sandbox supply. */
export type HandsMode = "auto" | "envRef" | "external"

export interface SessionPersistence {
  enabled?: boolean
  existingClaim?: string
  size?: string
  storageClass?: string
}

export interface ManagedAgentSession {
  persistence?: SessionPersistence
  retentionDays?: number
}

export interface LangfuseSpec {
  enabled?: boolean
  baseURL?: string
  publicKeyRef?: ManagedAgentSecretKeySelector
  secretKeyRef?: ManagedAgentSecretKeySelector
  environment?: string
}

export interface ManagedAgentObservability {
  langfuse?: LangfuseSpec
}

export interface ManagedAgentBrain {
  gatewayPort?: number
  workspaceFSPort?: number
  serviceAccountName?: string
  nodeSelector?: Record<string, string>
  resources?: Record<string, unknown>
  tolerations?: Record<string, unknown>[]
  affinity?: Record<string, unknown>
  extraEnv?: { name: string; value?: string; valueFrom?: Record<string, unknown> }[]
  extraEnvFrom?: Record<string, unknown>[]
  extraVolumes?: Record<string, unknown>[]
  extraVolumeMounts?: Record<string, unknown>[]
  extraPorts?: { name?: string; containerPort: number; protocol?: string }[]
}

export interface ManagedAgentSpec {
  displayName?: string
  description?: string
  owner?: ManagedAgentOwner
  /**
   * Deployment-supplied integration guide in Markdown, rendered on the agent's
   * Docs tab. Empty falls back to the console's built-in guide.
   */
  docs?: string
  image: ManagedAgentImage
  runtime: ManagedAgentRuntime
  classifier?: ManagedAgentClassifier
  prompt?: ManagedAgentPrompt
  scenarios?: ManagedAgentScenario[]
  hands: ManagedAgentHands
  session?: ManagedAgentSession
  observability?: ManagedAgentObservability
  brain?: ManagedAgentBrain
}

// ─── Status ───────────────────────────────────────────────────────────────────

export interface ResolvedHands {
  clusterID?: string
  envName?: string
  pools?: string[]
  ready?: boolean
}

export interface BackendStatus {
  id: string
  available?: boolean
  reason?: string
}

export interface ManagedAgentCondition {
  type: string
  status: "True" | "False" | "Unknown"
  reason?: string
  message?: string
  lastTransitionTime?: string
  observedGeneration?: number
}

export interface ManagedAgentStatus {
  observedGeneration?: number
  phase?: string
  endpoint?: string
  hands?: ResolvedHands
  backends?: BackendStatus[]
  scenarios?: string[]
  conditions?: ManagedAgentCondition[]
}

/** Condition types reported by the ManagedAgent controller, in display order. */
export const MANAGED_AGENT_CONDITION_TYPES = [
  "BrainReady",
  "BackendsAvailable",
  "HandsReady",
  "SandboxReachable",
] as const

export type ManagedAgentConditionType = (typeof MANAGED_AGENT_CONDITION_TYPES)[number]

// ─── Resource ─────────────────────────────────────────────────────────────────

export interface ManagedAgent {
  name: string
  namespace?: string
  creationTimestamp?: string
  spec: ManagedAgentSpec
  status?: ManagedAgentStatus
  /** Server-rendered CRD YAML; the detail page falls back to the JSON body. */
  crdYaml?: string
}

export interface ManagedAgentListResult {
  items?: ManagedAgent[]
}

/**
 * Plain-text secrets collected by the console. The server writes each into a
 * Secret and back-fills the matching `credentialsRef` on the stored spec, so the
 * console never handles Secret names. On update an omitted field keeps the key
 * already stored; the console omits whatever the user left blank.
 */
export interface ManagedAgentCredentials {
  claudeCodeApiKey?: string
  openCodeApiKey?: string
  classifierApiKey?: string
  /** Sandbox API key for `hands.external` / `hands.binding`. */
  sandboxApiKey?: string
}

export interface CreateManagedAgentRequest {
  name: string
  namespace?: string
  spec: ManagedAgentSpec
  credentials?: ManagedAgentCredentials
}

export interface UpdateManagedAgentRequest {
  spec: ManagedAgentSpec
  credentials?: ManagedAgentCredentials
}
