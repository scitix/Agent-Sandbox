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

"use client"

import { useMemo } from "react"
import { Check, Download, X } from "lucide-react"
import { stringify as yamlStringify } from "yaml"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { CopyableText } from "@/components/custom/copyable-text"
import { MarkdownRenderer } from "@/components/markdown-renderer"
import { buildFallbackDocs } from "@/components/managed-agents/docs-fallback"
import type {
  ManagedAgent,
  ManagedAgentCondition,
  ManagedAgentModel,
  ManagedAgentScenario,
  ManagedAgentSecretKeySelector,
} from "@/lib/api/managed-agent-types"
import { MANAGED_AGENT_CONDITION_TYPES } from "@/lib/api/managed-agent-types"
import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"

const DASH = "—"

// ─── Shared building blocks ───────────────────────────────────────────────────

function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <h3 className="text-muted-foreground mb-2 font-mono text-xs font-bold tracking-[0.12em] uppercase">
      {children}
    </h3>
  )
}

function KVGrid({ children }: { children: React.ReactNode }) {
  return (
    <dl className="border-border bg-muted/20 divide-border grid grid-cols-1 divide-x divide-y rounded border text-xs sm:grid-cols-2">
      {children}
    </dl>
  )
}

function KVRow({ label, value }: { label: string; value?: React.ReactNode }) {
  const isEmpty = value === undefined || value === null || value === ""
  return (
    <div className="flex items-start gap-3 px-3 py-2">
      <dt className="text-muted-foreground w-40 shrink-0 font-mono text-xs uppercase">{label}</dt>
      <dd className="min-w-0 font-mono text-xs break-all">
        {isEmpty ? <span className="text-muted-foreground">{DASH}</span> : value}
      </dd>
    </div>
  )
}

function BoolMark({ value }: { value?: boolean }) {
  const { t } = useTranslation()
  const Icon = value ? Check : X
  return (
    <Icon
      className={cn("h-3.5 w-3.5", value ? "text-green-500" : "text-muted-foreground")}
      aria-label={value ? t("common.yes") : t("common.no")}
    />
  )
}

function EmptyNote({ children }: { children: React.ReactNode }) {
  return <p className="text-muted-foreground text-xs">{children}</p>
}

/**
 * Credentials are never echoed back by the API, so the console reports only
 * whether one is in place and which Secret holds it.
 */
function CredentialValue({ selector }: { selector?: ManagedAgentSecretKeySelector }) {
  const { t } = useTranslation()
  if (!selector?.name) {
    return (
      <span className="text-muted-foreground">{t("managedAgents.detail.credentialUnset")}</span>
    )
  }
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      <Badge
        variant="outline"
        className="border-green-500/30 bg-green-500/15 font-mono text-[10px] text-green-700 uppercase dark:text-green-400"
      >
        {t("managedAgents.detail.credentialSet")}
      </Badge>
      <span className="text-muted-foreground">
        {selector.key ? `${selector.name}/${selector.key}` : selector.name}
      </span>
    </span>
  )
}

function ModelList({
  models,
  defaultModel,
}: {
  models?: ManagedAgentModel[]
  defaultModel?: string
}) {
  const { t } = useTranslation()
  if (!models?.length) return <EmptyNote>{t("managedAgents.detail.noModels")}</EmptyNote>
  return (
    <ul className="border-border bg-muted/20 divide-border divide-y rounded border">
      {models.map((model) => (
        <li key={model.id} className="flex items-center gap-2 px-3 py-1.5 font-mono text-xs">
          <span>{model.id}</span>
          {model.name && <span className="text-muted-foreground">{model.name}</span>}
          {model.id === defaultModel && (
            <Badge variant="outline" className="ml-auto font-mono text-[10px]">
              {t("managedAgents.detail.defaultBadge")}
            </Badge>
          )}
          {model.nonReasoning && (
            <Badge variant="outline" className="font-mono text-[10px]">
              {t("managedAgents.detail.nonReasoningBadge")}
            </Badge>
          )}
        </li>
      ))}
    </ul>
  )
}

// ─── Overview ─────────────────────────────────────────────────────────────────

function ConditionRow({ type, condition }: { type: string; condition?: ManagedAgentCondition }) {
  const { t } = useTranslation()
  const ok = condition?.status === "True"
  const unknown = !condition || condition.status === "Unknown"
  return (
    <div className="flex items-start gap-3 px-3 py-2">
      <dt className="w-44 shrink-0 font-mono text-xs">{type}</dt>
      <dd className="flex min-w-0 flex-col gap-1">
        <Badge
          variant="outline"
          className={cn(
            "w-fit font-mono text-[10px] uppercase",
            unknown
              ? "border-gray-500/30 bg-gray-500/15 text-gray-500 dark:text-gray-400"
              : ok
                ? "border-green-500/30 bg-green-500/15 text-green-700 dark:text-green-400"
                : "border-red-500/30 bg-red-500/15 text-red-700 dark:text-red-400",
          )}
        >
          {condition?.status ?? t("managedAgents.detail.conditionUnknown")}
        </Badge>
        {condition?.reason && (
          <span className="text-muted-foreground font-mono text-[11px]">{condition.reason}</span>
        )}
        {condition?.message && (
          <span className="text-muted-foreground text-[11px] break-all">{condition.message}</span>
        )}
      </dd>
    </div>
  )
}

export function OverviewSection({ agent }: { agent: ManagedAgent }) {
  const { t } = useTranslation()
  const conditions = agent.status?.conditions ?? []
  const backends = agent.status?.backends ?? []

  return (
    <div className="flex flex-col gap-6">
      <section>
        <SectionTitle>{t("managedAgents.detail.section.summary")}</SectionTitle>
        <KVGrid>
          <KVRow
            label={t("managedAgents.detail.field.displayName")}
            value={agent.spec.displayName}
          />
          <KVRow label={t("managedAgents.detail.field.team")} value={agent.spec.owner?.team} />
          <KVRow label={t("managedAgents.detail.field.user")} value={agent.spec.owner?.user} />
          <KVRow
            label={t("managedAgents.detail.field.image")}
            value={
              agent.spec.image?.tag
                ? `${agent.spec.image.repository}:${agent.spec.image.tag}`
                : agent.spec.image?.repository
            }
          />
          <KVRow
            label={t("managedAgents.detail.field.defaultRuntime")}
            value={agent.spec.runtime?.default}
          />
          <KVRow
            label={t("managedAgents.detail.field.observedGeneration")}
            value={agent.status?.observedGeneration}
          />
          <KVRow
            label={t("managedAgents.detail.field.description")}
            value={agent.spec.description}
          />
        </KVGrid>
        <p className="text-muted-foreground mt-2 text-[11px]">
          {t("managedAgents.detail.ownerHint")}
        </p>
      </section>

      <section>
        <SectionTitle>{t("managedAgents.detail.section.endpoint")}</SectionTitle>
        {agent.status?.endpoint ? (
          <div className="border-border bg-muted/20 rounded border px-3 py-2">
            <CopyableText value={agent.status.endpoint} />
            <p className="text-muted-foreground mt-1 text-[11px]">
              {t("managedAgents.detail.endpointHint")}
            </p>
          </div>
        ) : (
          <EmptyNote>{t("managedAgents.detail.noEndpoint")}</EmptyNote>
        )}
      </section>

      <section>
        <SectionTitle>{t("managedAgents.detail.section.conditions")}</SectionTitle>
        <dl className="border-border bg-muted/20 divide-border divide-y rounded border">
          {MANAGED_AGENT_CONDITION_TYPES.map((type) => (
            <ConditionRow
              key={type}
              type={type}
              condition={conditions.find((c) => c.type === type)}
            />
          ))}
        </dl>
      </section>

      <section>
        <SectionTitle>{t("managedAgents.detail.section.backends")}</SectionTitle>
        {backends.length === 0 ? (
          <EmptyNote>{t("managedAgents.detail.noBackends")}</EmptyNote>
        ) : (
          <dl className="border-border bg-muted/20 divide-border divide-y rounded border">
            {backends.map((backend) => (
              <div key={backend.id} className="flex items-start gap-3 px-3 py-2">
                <dt className="w-44 shrink-0 font-mono text-xs">{backend.id}</dt>
                <dd className="flex min-w-0 flex-col gap-1">
                  <span className="flex items-center gap-1.5 font-mono text-xs">
                    <BoolMark value={backend.available} />
                    {backend.available
                      ? t("managedAgents.detail.backendAvailable")
                      : t("managedAgents.detail.backendUnavailable")}
                  </span>
                  {!backend.available && backend.reason && (
                    <span className="text-muted-foreground text-[11px] break-all">
                      {backend.reason}
                    </span>
                  )}
                </dd>
              </div>
            ))}
          </dl>
        )}
      </section>
    </div>
  )
}

// ─── Docs ─────────────────────────────────────────────────────────────────────

/**
 * Integration guide. `spec.docs` is the deployment's own document and always
 * wins; an agent without one gets the console's built-in guide, filled in with
 * this agent's endpoint and scenarios.
 */
export function DocsSection({ agent }: { agent: ManagedAgent }) {
  const { t } = useTranslation()
  const provided = agent.spec.docs?.trim()
  const content = useMemo(() => provided || buildFallbackDocs(agent), [provided, agent])

  return (
    <section>
      <div className="mb-3 flex items-center justify-between gap-3">
        <SectionTitle>{t("managedAgents.detail.section.docs")}</SectionTitle>
        {!provided && (
          <span className="text-muted-foreground font-mono text-[10px] tracking-wider uppercase">
            {t("managedAgents.detail.docsFallbackBadge")}
          </span>
        )}
      </div>
      <MarkdownRenderer content={content} />
    </section>
  )
}

// ─── Runtime & models ─────────────────────────────────────────────────────────

export function RuntimeSection({ agent }: { agent: ManagedAgent }) {
  const { t } = useTranslation()
  const claude = agent.spec.runtime?.claudeCode
  const opencode = agent.spec.runtime?.opencode
  const classifier = agent.spec.classifier
  const prompt = agent.spec.prompt

  return (
    <div className="flex flex-col gap-6">
      <section>
        <SectionTitle>claude-code</SectionTitle>
        {!claude ? (
          <EmptyNote>{t("managedAgents.detail.notConfigured")}</EmptyNote>
        ) : (
          <>
            <KVGrid>
              <KVRow label={t("managedAgents.detail.field.baseURL")} value={claude.baseURL} />
              <KVRow
                label={t("managedAgents.detail.field.credentials")}
                value={<CredentialValue selector={claude.credentialsRef} />}
              />
              <KVRow
                label={t("managedAgents.detail.field.defaultModel")}
                value={claude.defaultModel}
              />
              <KVRow label={t("managedAgents.detail.field.smallModel")} value={claude.smallModel} />
              <KVRow label={t("managedAgents.detail.field.effort")} value={claude.effort} />
              <KVRow
                label={t("managedAgents.detail.field.pluginPaths")}
                value={claude.pluginPaths?.join(", ")}
              />
            </KVGrid>
            <div className="mt-3">
              <SectionTitle>{t("managedAgents.detail.section.models")}</SectionTitle>
              <ModelList models={claude.models} defaultModel={claude.defaultModel} />
            </div>
          </>
        )}
      </section>

      <section>
        <SectionTitle>opencode</SectionTitle>
        {!opencode ? (
          <EmptyNote>{t("managedAgents.detail.notConfigured")}</EmptyNote>
        ) : (
          <>
            <KVGrid>
              <KVRow
                label={t("managedAgents.detail.field.enabled")}
                value={<BoolMark value={opencode.enabled !== false} />}
              />
              <KVRow label={t("managedAgents.detail.field.baseURL")} value={opencode.baseURL} />
              <KVRow
                label={t("managedAgents.detail.field.credentials")}
                value={<CredentialValue selector={opencode.credentialsRef} />}
              />
              <KVRow
                label={t("managedAgents.detail.field.defaultModel")}
                value={opencode.defaultModel}
              />
              <KVRow label={t("managedAgents.detail.field.port")} value={opencode.port} />
              <KVRow
                label={t("managedAgents.detail.field.configSecret")}
                value={
                  opencode.configSecretRef
                    ? `${opencode.configSecretRef.name}/${opencode.configSecretRef.key}`
                    : undefined
                }
              />
            </KVGrid>
            <div className="mt-3">
              <SectionTitle>{t("managedAgents.detail.section.models")}</SectionTitle>
              <ModelList models={opencode.models} defaultModel={opencode.defaultModel} />
            </div>
            {opencode.configSecretRef && (
              <p className="text-muted-foreground mt-2 text-[11px]">
                {t("managedAgents.detail.configSecretHint")}
              </p>
            )}
          </>
        )}
      </section>

      <section>
        <SectionTitle>{t("managedAgents.detail.section.classifier")}</SectionTitle>
        {!classifier ? (
          <EmptyNote>{t("managedAgents.detail.notConfigured")}</EmptyNote>
        ) : (
          <KVGrid>
            <KVRow
              label={t("managedAgents.detail.field.enabled")}
              value={<BoolMark value={classifier.enabled !== false} />}
            />
            <KVRow label={t("managedAgents.detail.field.wire")} value={classifier.wire} />
            <KVRow label={t("managedAgents.detail.field.baseURL")} value={classifier.baseURL} />
            <KVRow label={t("managedAgents.detail.field.model")} value={classifier.model} />
            <KVRow
              label={t("managedAgents.detail.field.credentials")}
              value={<CredentialValue selector={classifier.credentialsRef} />}
            />
            <KVRow label={t("managedAgents.detail.field.maxTokens")} value={classifier.maxTokens} />
            <KVRow
              label={t("managedAgents.detail.field.timeoutSeconds")}
              value={classifier.timeoutSeconds}
            />
          </KVGrid>
        )}
      </section>

      <section>
        <SectionTitle>{t("managedAgents.detail.section.basePrompt")}</SectionTitle>
        {!prompt?.inline && !prompt?.from && !prompt?.append ? (
          <EmptyNote>{t("managedAgents.detail.noPrompt")}</EmptyNote>
        ) : (
          <div className="flex flex-col gap-2">
            {prompt.from && (
              <KVGrid>
                <KVRow
                  label={t("managedAgents.detail.field.promptFrom")}
                  value={`${prompt.from.name}/${prompt.from.key}`}
                />
              </KVGrid>
            )}
            {prompt.inline && (
              <pre className="bg-secondary overflow-auto rounded border p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap">
                {prompt.inline}
              </pre>
            )}
            {prompt.append && (
              <pre className="bg-secondary overflow-auto rounded border p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap">
                {prompt.append}
              </pre>
            )}
          </div>
        )}
      </section>
    </div>
  )
}

// ─── Scenarios ────────────────────────────────────────────────────────────────

function ScenarioCard({ scenario }: { scenario: ManagedAgentScenario }) {
  const { t } = useTranslation()
  return (
    <div className="border-border rounded border">
      <div className="border-border flex flex-wrap items-center gap-2 border-b px-3 py-2">
        <span className="font-mono text-xs font-semibold">{scenario.name}</span>
        {scenario.displayName && (
          <span className="text-muted-foreground text-xs">{scenario.displayName}</span>
        )}
        {scenario.default && (
          <Badge variant="outline" className="font-mono text-[10px]">
            {t("managedAgents.detail.defaultBadge")}
          </Badge>
        )}
        {scenario.exposed === false && (
          <Badge variant="outline" className="font-mono text-[10px]">
            {t("managedAgents.detail.hiddenBadge")}
          </Badge>
        )}
      </div>
      <KVGrid>
        <KVRow
          label={t("managedAgents.detail.field.runtimePin")}
          value={scenario.runtime || undefined}
        />
        <KVRow label={t("managedAgents.detail.field.model")} value={scenario.model} />
        <KVRow
          label={t("managedAgents.detail.field.interactive")}
          value={<BoolMark value={scenario.interactive !== false} />}
        />
        <KVRow label={t("managedAgents.detail.field.scalingGroup")} value={scenario.scalingGroup} />
        <KVRow label={t("managedAgents.detail.field.sandboxImage")} value={scenario.image} />
        <KVRow
          label={t("managedAgents.detail.field.allow")}
          value={scenario.allow?.length ? scenario.allow.join(", ") : undefined}
        />
        <KVRow
          label={t("managedAgents.detail.field.disable")}
          value={scenario.disable?.length ? scenario.disable.join(", ") : undefined}
        />
      </KVGrid>
      {scenario.prompt?.inline && (
        <pre className="bg-secondary m-3 overflow-auto rounded border p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap">
          {scenario.prompt.inline}
        </pre>
      )}
    </div>
  )
}

export function ScenariosSection({ agent }: { agent: ManagedAgent }) {
  const { t } = useTranslation()
  const scenarios = agent.spec.scenarios ?? []
  const served = agent.status?.scenarios ?? []

  if (scenarios.length === 0) {
    return <EmptyNote>{t("managedAgents.detail.noScenarios")}</EmptyNote>
  }

  return (
    <div className="flex flex-col gap-4">
      {served.length > 0 && (
        <p className="text-muted-foreground text-xs">
          {t("managedAgents.detail.servedScenarios", { names: served.join(", ") })}
        </p>
      )}
      {scenarios.map((scenario) => (
        <ScenarioCard key={scenario.name} scenario={scenario} />
      ))}
    </div>
  )
}

// ─── Hands ────────────────────────────────────────────────────────────────────

export function HandsSection({ agent }: { agent: ManagedAgent }) {
  const { t } = useTranslation()
  const hands = agent.spec.hands ?? {}
  const resolved = agent.status?.hands
  const binding = hands.binding
  const e2b = hands.e2b

  return (
    <div className="flex flex-col gap-6">
      <section>
        <SectionTitle>{t("managedAgents.detail.section.handsResolved")}</SectionTitle>
        <KVGrid>
          <KVRow label={t("managedAgents.detail.field.envName")} value={resolved?.envName} />
          <KVRow label={t("managedAgents.detail.field.clusterID")} value={resolved?.clusterID} />
          <KVRow
            label={t("managedAgents.detail.field.pools")}
            value={resolved?.pools?.length ? resolved.pools.join(", ") : undefined}
          />
          <KVRow
            label={t("managedAgents.detail.field.handsReady")}
            value={<BoolMark value={resolved?.ready} />}
          />
        </KVGrid>
      </section>

      <section>
        <SectionTitle>{t("managedAgents.detail.section.handsSupply")}</SectionTitle>
        {hands.external ? (
          <KVGrid>
            <KVRow
              label={t("managedAgents.detail.field.handsMode")}
              value={t("managedAgents.handsMode.external")}
            />
            <KVRow
              label={t("managedAgents.detail.field.e2bApiUrl")}
              value={hands.external.apiURL}
            />
            <KVRow
              label={t("managedAgents.detail.field.e2bDomain")}
              value={hands.external.domain}
            />
            <KVRow
              label={t("managedAgents.detail.field.e2bHttps")}
              value={<BoolMark value={hands.external.https !== false} />}
            />
            <KVRow
              label={t("managedAgents.detail.field.externalEnvName")}
              value={hands.external.envName}
            />
            <KVRow
              label={t("managedAgents.detail.field.scalingGroup")}
              value={hands.external.scalingGroup}
            />
            <KVRow
              label={t("managedAgents.detail.field.sandboxImage")}
              value={hands.external.image}
            />
            <KVRow
              label={t("managedAgents.detail.field.credentials")}
              value={<CredentialValue selector={hands.external.credentialsRef} />}
            />
          </KVGrid>
        ) : hands.envRef ? (
          <KVGrid>
            <KVRow
              label={t("managedAgents.detail.field.handsMode")}
              value={t("managedAgents.handsMode.envRef")}
            />
            <KVRow label={t("managedAgents.detail.field.envName")} value={hands.envRef.name} />
            <KVRow
              label={t("managedAgents.detail.field.clusterID")}
              value={hands.envRef.clusterID}
            />
            <KVRow
              label={t("managedAgents.detail.field.namespace")}
              value={hands.envRef.namespace}
            />
            <KVRow
              label={t("managedAgents.detail.field.scalingGroup")}
              value={hands.envRef.scalingGroup}
            />
            <KVRow
              label={t("managedAgents.detail.field.sandboxImage")}
              value={hands.envRef.image}
            />
          </KVGrid>
        ) : hands.auto ? (
          <>
            <KVGrid>
              <KVRow
                label={t("managedAgents.detail.field.handsMode")}
                value={t("managedAgents.handsMode.auto")}
              />
              <KVRow
                label={t("managedAgents.detail.field.clusterID")}
                value={hands.auto.clusterID}
              />
              <KVRow
                label={t("managedAgents.detail.field.templateRef")}
                value={hands.auto.templateRef}
              />
              <KVRow
                label={t("managedAgents.detail.field.sandboxImage")}
                value={hands.auto.image}
              />
              <KVRow
                label={t("managedAgents.detail.field.idleTimeout")}
                value={hands.auto.idleTimeoutSeconds}
              />
              <KVRow
                label={t("managedAgents.detail.field.startupTimeout")}
                value={hands.auto.startupTimeoutSeconds}
              />
            </KVGrid>
            <div className="mt-3">
              <SectionTitle>{t("managedAgents.detail.field.instanceTypes")}</SectionTitle>
              <ul className="border-border bg-muted/20 divide-border divide-y rounded border">
                {hands.auto.instanceTypes?.map((it) => (
                  <li
                    key={it.name}
                    className="flex items-center gap-2 px-3 py-1.5 font-mono text-xs"
                  >
                    <span>{it.name}</span>
                    <span className="text-muted-foreground">
                      {t("managedAgents.detail.replicasLabel", { count: it.replicas ?? 0 })}
                    </span>
                    {it.default && (
                      <Badge variant="outline" className="ml-auto font-mono text-[10px]">
                        {t("managedAgents.detail.defaultBadge")}
                      </Badge>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          </>
        ) : (
          // No branch is not "unconfigured": the platform supplies one, and the
          // resolved values are the section above. Saying "not configured" here
          // would send someone looking for a field to fill in.
          <>
            <KVGrid>
              <KVRow
                label={t("managedAgents.detail.field.handsMode")}
                value={t("managedAgents.detail.handsSourcePlatformDefault")}
              />
              <KVRow
                label={t("managedAgents.detail.field.envName")}
                value={resolved?.envName}
              />
            </KVGrid>
            <p className="text-muted-foreground mt-2 text-xs">
              {t("managedAgents.detail.handsSourcePlatformDefaultHint")}
            </p>
          </>
        )}
      </section>

      <section>
        <SectionTitle>{t("managedAgents.detail.section.binding")}</SectionTitle>
        {!binding ? (
          <EmptyNote>{t("managedAgents.detail.notConfigured")}</EmptyNote>
        ) : (
          <KVGrid>
            <KVRow label={t("managedAgents.detail.field.bindingScope")} value={binding.scope} />
            <KVRow
              label={t("managedAgents.detail.field.idleTimeout")}
              value={binding.timeoutSeconds}
            />
            <KVRow
              label={t("managedAgents.detail.field.readyTimeout")}
              value={binding.readyTimeoutSeconds}
            />
            <KVRow label={t("managedAgents.detail.field.workspace")} value={binding.workspace} />
            <KVRow
              label={t("managedAgents.detail.field.attachmentRoot")}
              value={binding.attachmentRoot}
            />
            <KVRow label={t("managedAgents.detail.field.seedRepo")} value={binding.seedRepo} />
          </KVGrid>
        )}
      </section>

      <section>
        <SectionTitle>{t("managedAgents.detail.section.sandboxApi")}</SectionTitle>
        {!e2b ? (
          <EmptyNote>{t("managedAgents.detail.e2bFallback")}</EmptyNote>
        ) : (
          <KVGrid>
            <KVRow label={t("managedAgents.detail.field.e2bApiUrl")} value={e2b.apiURL} />
            <KVRow label={t("managedAgents.detail.field.e2bDomain")} value={e2b.domain} />
            <KVRow
              label={t("managedAgents.detail.field.e2bSecret")}
              value={e2b.credentialsSecret}
            />
            <KVRow
              label={t("managedAgents.detail.field.e2bHttps")}
              value={<BoolMark value={e2b.https !== false} />}
            />
          </KVGrid>
        )}
      </section>
    </div>
  )
}

// ─── Session & storage ────────────────────────────────────────────────────────

export function SessionSection({ agent }: { agent: ManagedAgent }) {
  const { t } = useTranslation()
  const session = agent.spec.session
  const persistence = session?.persistence
  const brain = agent.spec.brain

  return (
    <div className="flex flex-col gap-6">
      <section>
        <SectionTitle>{t("managedAgents.detail.section.session")}</SectionTitle>
        {!session ? (
          <EmptyNote>{t("managedAgents.detail.sessionEphemeral")}</EmptyNote>
        ) : (
          <KVGrid>
            <KVRow
              label={t("managedAgents.detail.field.persistenceEnabled")}
              value={<BoolMark value={persistence?.enabled === true} />}
            />
            <KVRow
              label={t("managedAgents.detail.field.existingClaim")}
              value={persistence?.existingClaim}
            />
            <KVRow label={t("managedAgents.detail.field.size")} value={persistence?.size} />
            <KVRow
              label={t("managedAgents.detail.field.storageClass")}
              value={persistence?.storageClass}
            />
            <KVRow
              label={t("managedAgents.detail.field.retentionDays")}
              value={session.retentionDays}
            />
          </KVGrid>
        )}
        {persistence?.enabled !== true && (
          <p className="text-muted-foreground mt-2 text-[11px]">
            {t("managedAgents.detail.persistenceHint")}
          </p>
        )}
      </section>

      <section>
        <SectionTitle>{t("managedAgents.detail.section.brain")}</SectionTitle>
        <KVGrid>
          <KVRow label={t("managedAgents.detail.field.gatewayPort")} value={brain?.gatewayPort} />
          <KVRow
            label={t("managedAgents.detail.field.workspaceFSPort")}
            value={brain?.workspaceFSPort}
          />
          <KVRow
            label={t("managedAgents.detail.field.serviceAccount")}
            value={brain?.serviceAccountName}
          />
          <KVRow
            label={t("managedAgents.detail.field.extraEnvCount")}
            value={brain?.extraEnv?.length ?? 0}
          />
        </KVGrid>
      </section>
    </div>
  )
}

// ─── Observability ────────────────────────────────────────────────────────────

export function ObservabilitySection({ agent }: { agent: ManagedAgent }) {
  const { t } = useTranslation()
  const langfuse = agent.spec.observability?.langfuse

  if (!langfuse) {
    return <EmptyNote>{t("managedAgents.detail.noObservability")}</EmptyNote>
  }

  return (
    <section>
      <SectionTitle>Langfuse</SectionTitle>
      <KVGrid>
        <KVRow
          label={t("managedAgents.detail.field.enabled")}
          value={<BoolMark value={langfuse.enabled !== false} />}
        />
        <KVRow label={t("managedAgents.detail.field.baseURL")} value={langfuse.baseURL} />
        <KVRow
          label={t("managedAgents.detail.field.langfuseEnvironment")}
          value={langfuse.environment}
        />
        <KVRow
          label={t("managedAgents.detail.field.publicKey")}
          value={
            langfuse.publicKeyRef
              ? `${langfuse.publicKeyRef.name}/${langfuse.publicKeyRef.key}`
              : undefined
          }
        />
        <KVRow
          label={t("managedAgents.detail.field.secretKey")}
          value={
            langfuse.secretKeyRef
              ? `${langfuse.secretKeyRef.name}/${langfuse.secretKeyRef.key}`
              : undefined
          }
        />
      </KVGrid>
      <p className="text-muted-foreground mt-2 text-[11px]">
        {t("managedAgents.detail.langfuseEnvironmentHint")}
      </p>
    </section>
  )
}

// ─── YAML ─────────────────────────────────────────────────────────────────────

export function YamlSection({ agent }: { agent: ManagedAgent }) {
  const { t } = useTranslation()

  // The server may render the CRD itself; when it does not, the same document is
  // reassembled from the resource so the tab is never empty.
  const yaml = useMemo(() => {
    if (agent.crdYaml) return agent.crdYaml
    return yamlStringify({
      apiVersion: "agents.navix.sh/v1alpha1",
      kind: "ManagedAgent",
      metadata: { name: agent.name, namespace: agent.namespace },
      spec: agent.spec,
    })
  }, [agent])

  const handleExport = () => {
    const blob = new Blob([yaml], { type: "text/yaml" })
    const url = URL.createObjectURL(blob)
    const a = document.createElement("a")
    a.href = url
    a.download = `${agent.name}.yaml`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <section>
      <div className="mb-2 flex items-center justify-between">
        <SectionTitle>{t("managedAgents.detail.section.yaml")}</SectionTitle>
        <Button variant="outline" size="sm" className="h-7 gap-1.5 text-xs" onClick={handleExport}>
          <Download className="h-3 w-3" />
          {t("managedAgents.detail.exportYaml")}
        </Button>
      </div>
      <pre className="bg-secondary overflow-auto rounded border p-3 font-mono text-xs leading-relaxed">
        {yaml}
      </pre>
    </section>
  )
}
