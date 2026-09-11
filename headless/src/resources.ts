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
 * The resource registry: one entry per thing the platform addresses.
 *
 * This is the single source the console's routes and the CLI's positional
 * grammar both derive from. Before it existed the same mapping was written out
 * four times — the console's path builder, the assistant's navigation catalog,
 * the MCP open_page tool, and the CLI's own segment table — and they had
 * drifted: one emitted a link to a resource that does not exist, another to a
 * page that 404s, a third advertised a sub-resource nobody had registered.
 *
 * Adding a resource here is what makes it addressable on both surfaces. There
 * is no second place to register it, which is the point.
 */

import type { ResourceSpec } from './types'

export const RESOURCES: readonly ResourceSpec[] = [
  {
    kind: 'cluster',
    plural: 'clusters',
    describe: 'Clusters this endpoint can reach.',
    columns: [
      { id: 'id', describe: 'Cluster id — the value every other command takes as --cluster.' },
      { id: 'name', describe: 'Display name.' },
    ],
  },
  {
    kind: 'env',
    plural: 'envs',
    describe:
      'A SandboxEnv binds one template and fans out to member warm pools. This is the object to create first; pools are added to it.',
    detail: true,
    columns: [
      { id: 'name', describe: 'Env name.', filter: 'name' },
      { id: 'templateName', describe: 'The bound SandboxTemplate.', filter: 'templateName' },
      { id: 'mode', describe: 'WarmPool keeps idle Pods ready; OnDemandJob creates one per sandbox.', filter: 'mode' },
      { id: 'memberCount', describe: 'Number of member pools, summed across clusters.' },
      { id: 'desiredReplicas', describe: 'Sum of spec.replicas across observed members.' },
      { id: 'idleReplicas', describe: 'Pods available to claim right now.' },
      { id: 'ready', describe: 'Whether every member pool is serving.', filter: 'ready' },
      { id: 'team', describe: 'Owning team.', filter: 'team', optional: true },
    ],
    filters: [
      { key: 'name', describe: 'substring of the env name' },
      { key: 'templateName', describe: 'the bound template' },
      { key: 'mode', describe: 'provisioning mode', values: ['WarmPool', 'OnDemandJob'] },
      { key: 'ready', describe: 'readiness', values: ['true', 'false'] },
      { key: 'team', describe: 'owning team' },
    ],
    subResources: [
      { segment: 'pools', describe: 'Member pools of this env.' },
      {
        segment: 'scaling-groups',
        describe:
          'Autoscaling groups. Named for what they are: the API field and the pool column both call this a scalingGroup, and only the old route called it "autoscaling".',
      },
      { segment: 'events', describe: 'Recent Kubernetes events for this env.' },
    ],
    views: [{ segment: 'metrics', describe: 'Time-series for this env.' }],
  },
  {
    kind: 'pool',
    plural: 'pools',
    describe: 'A member warm pool. Always belongs to an env.',
    detail: true,
    columns: [
      { id: 'name', describe: 'Pool name, derived from the env name and the resource shape.', filter: 'name' },
      { id: 'owningEnv', describe: 'The env this pool is a member of.', filter: 'owningEnv' },
      { id: 'replicas', describe: 'Desired size. Owned by the autoscaler when its group is enabled.' },
      { id: 'idleReplicas', describe: 'Pods available to claim right now.' },
      { id: 'runningReplicas', describe: 'Pods currently claimed by a sandbox.' },
      { id: 'scalingGroup', describe: 'The autoscaling group this member belongs to.', filter: 'scalingGroup' },
      { id: 'phase', describe: 'Pool phase.', filter: 'phase', optional: true },
    ],
    filters: [
      { key: 'name', describe: 'substring of the pool name' },
      { key: 'owningEnv', describe: 'the owning env' },
      { key: 'scalingGroup', describe: 'the autoscaling group' },
      { key: 'phase', describe: 'pool phase' },
    ],
    views: [{ segment: 'metrics', describe: 'Time-series for this pool.' }],
  },
  {
    kind: 'sandbox',
    plural: 'sandboxes',
    describe:
      'A running sandbox. Created and driven with the E2B SDK, not this CLI — these are read views of what that produced.',
    detail: true,
    columns: [
      { id: 'sandboxId', describe: 'Sandbox id.', filter: 'sandboxId' },
      { id: 'status', describe: 'Lifecycle status.', filter: 'status' },
      { id: 'envName', describe: 'The env it was claimed from.', filter: 'envName' },
      { id: 'poolName', describe: 'The member pool it was claimed from.', filter: 'poolName' },
      {
        id: 'metadata',
        describe:
          'Key/value pairs supplied on create. The assistant records which conversation a sandbox belongs to here, which is what tells two sandboxes apart when they share an identity.',
        optional: true,
      },
      { id: 'team', describe: 'Owning team.', filter: 'team', optional: true },
      { id: 'user', describe: 'Owning user.', filter: 'user', optional: true },
    ],
    filters: [
      { key: 'sandboxId', describe: 'substring of the sandbox id' },
      { key: 'status', describe: 'lifecycle status' },
      { key: 'poolName', describe: 'the member pool' },
      { key: 'envName', describe: 'the owning env' },
      { key: 'team', describe: 'owning team' },
      { key: 'user', describe: 'owning user' },
    ],
    views: [{ segment: 'logs', describe: 'Logs for this sandbox.' }],
  },
  {
    kind: 'template',
    plural: 'templates',
    describe: 'Reusable Pod templates an env can be rendered from.',
    detail: true,
    columns: [
      { id: 'name', describe: 'Template name.', filter: 'name' },
      { id: 'version', describe: 'spec.version — names a template revision.' },
      { id: 'description', describe: 'What this template is for.', optional: true },
    ],
    filters: [{ key: 'name', describe: 'substring of the template name' }],
  },
  {
    kind: 'instancetype',
    plural: 'instancetypes',
    describe: 'The quota classes a pool can be sized against.',
    columns: [
      { id: 'name', describe: 'Instance type name.', filter: 'name' },
      { id: 'baseResources', describe: 'CPU / memory / GPU per unit.' },
      { id: 'cost', describe: 'Relative cost, for comparison only.', optional: true },
    ],
    filters: [{ key: 'name', describe: 'substring of the instance type name' }],
    gate: 'instanceType',
  },
  {
    kind: 'quota',
    plural: 'quotas',
    describe: 'Per-team quota and how much of it is in use.',
    columns: [
      { id: 'name', describe: 'Quota name.', filter: 'name' },
      { id: 'team', describe: 'Owning team.', filter: 'team' },
      { id: 'used', describe: 'Committed against this quota.' },
      { id: 'total', describe: 'Ceiling.' },
    ],
    filters: [
      { key: 'name', describe: 'substring of the quota name' },
      { key: 'team', describe: 'owning team' },
    ],
    gate: 'quota',
  },
] as const

export function resourceOf(token: string): ResourceSpec | undefined {
  return RESOURCES.find((r) => r.plural === token || r.kind === token)
}
