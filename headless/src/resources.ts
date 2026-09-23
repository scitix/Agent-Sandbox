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
 * This is the single source the console's routes, the CLI's positional grammar
 * and the conformance manifest all derive from. Before it existed the same
 * mapping was written out four times — the console's path builder, the
 * assistant's navigation catalog, the MCP open_page tool, and the CLI's own
 * segment table — and they had drifted: one emitted a link to a resource that
 * does not exist, another to a page that 404s, a third advertised a
 * sub-resource nobody had registered.
 *
 * Adding a resource here is what makes it addressable on both surfaces. There
 * is no second place to register it, which is the point.
 */

import type { ApiOperation, ResourceSpec, Segment } from './types'

/**
 * Where the prose lives.
 *
 * The public documentation site, and the `.md` twin of each page: a fetchable
 * document is what an agent wants, and the HTML is rendered from the same
 * source. One constant, because every link the CLI prints is this plus a slug.
 */
export const DOCS_BASE = 'https://scitix.github.io/Agent-Sandbox/docs'

/** The document for a registry entry, if it has one. */
export function docsURL(spec: { docs?: string }): string | undefined {
  return spec.docs ? `${DOCS_BASE}/${spec.docs}.md` : undefined
}

export const RESOURCES: readonly ResourceSpec[] = [
  {
    kind: 'cluster',
    plural: 'clusters',
    describe: 'Clusters this endpoint can reach.',
    docs: 'concepts/cross-cluster',
    api: { list: '/clusters' },
    clusterScoped: false,
    // Switching clusters is a dropdown in the console's sidebar, not a page:
    // `/clusters` has no route of its own, so a "view in console" link to it
    // is a 404 wearing the clothes of a working alternative.
    consolePage: false,
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
    docs: 'concepts/envs',
    api: { list: '/envs', item: '/envs/{name}', verbs: ['create', 'update', 'delete'] },
    detail: true,
    columns: [
      { id: 'name', describe: 'Env name.', filter: 'name' },
      { id: 'templateName', describe: 'The bound SandboxTemplate.', filter: 'templateName' },
      { id: 'mode', describe: 'WarmPool keeps idle Pods ready; OnDemandJob creates one per sandbox.', filter: 'mode' },
      { id: 'memberCount', describe: 'Number of member pools, summed across clusters.' },
      { id: 'desiredReplicas', describe: 'Sum of spec.replicas across observed members.' },
      { id: 'idleReplicas', describe: 'Pods available to claim right now.' },
      { id: 'ready', describe: 'Whether every member pool is serving.', filter: 'ready' },
      {
        id: 'poolSizing',
        describe: 'What a pool added here must declare: billed, free-form, or either.',
        filter: 'poolSizing',
        optional: true,
      },
      { id: 'team', describe: 'Owning team.', filter: 'team', optional: true },
    ],
    // The get response is a different shape, not a wider one: the template, the
    // mode and every replica count sit under spec/status. Reusing the summary
    // columns for it printed a row of dashes with the data sitting right there
    // in the same response.
    //
    // `envDocs` is not a field here because it is a view (`abx envs <env>
    // docs`): it is a document, and a document belongs on its own page rather
    // than in the middle of a list of scalars.
    detailFields: [
      { id: 'name', describe: 'Env name.' },
      { id: 'namespace', describe: 'Namespace the Env lives in.' },
      { id: 'template', path: 'spec.templateRef.name', describe: 'The bound SandboxTemplate.' },
      {
        id: 'templateVersion',
        path: 'spec.templateRef.version',
        describe: 'Pinned template revision. Empty means follow the template’s current body.',
      },
      { id: 'mode', path: 'spec.mode', describe: 'WarmPool or OnDemandJob.' },
      { id: 'team', describe: 'Owning team.' },
      { id: 'user', describe: 'Owning user.' },
      { id: 'createdAt', describe: 'When the Env was created.' },
      {
        id: 'poolSizing',
        describe:
          'What a pool added to this env must declare: billed (a quota and an instance type), ' +
          'free-form (inlineResources only), or either (this deployment has no rule). Read it ' +
          'before writing a pool body — the server refuses the wrong shape.',
      },
      { id: 'memberCount', path: 'status.memberCount', describe: 'Member pools across every cluster segment.' },
      { id: 'desiredReplicas', path: 'status.desiredReplicas', describe: 'Sum of the members’ desired replicas.' },
      { id: 'runningReplicas', path: 'status.runningReplicas', describe: 'Pods currently claimed by a sandbox.' },
      { id: 'idleReplicas', path: 'status.idleReplicas', describe: 'Pods available to claim right now.' },
      {
        id: 'overrides',
        path: 'spec.overrides',
        describe: 'Written whole: `abx update` replaces this object, it does not merge into it.',
      },
    ],
    filters: [
      { key: 'name', describe: 'substring of the env name' },
      { key: 'templateName', describe: 'the bound template' },
      { key: 'mode', describe: 'provisioning mode', values: ['WarmPool', 'OnDemandJob'] },
      { key: 'ready', describe: 'readiness', values: ['true', 'false'] },
      {
        key: 'poolSizing',
        describe: 'what a pool added to this env must declare',
        values: ['billed', 'free-form', 'either'],
      },
      { key: 'team', describe: 'owning team' },
    ],
    views: [
      { segment: 'metrics', describe: 'Time-series for this env.' },
      {
        segment: 'docs',
        describe:
          'Rendered Markdown documentation for this env: the E2B endpoints of its cluster, and how to reach its sandboxes.',
        api: '/envs/{name}',
        shape: 'docs',
        field: 'envDocs',
      },
    ],
  },
  {
    kind: 'pool',
    plural: 'pools',
    parent: 'envs',
    describe:
      'A member warm pool. Always belongs to an env, and takes its sizing shape from it: ' +
      'a billed env’s pools name a quota and an instance type, an unbilled env’s are sized ' +
      'with inlineResources. `abx envs <env>` prints which.',
    docs: 'concepts/pools',
    api: {
      list: '/envs/{name}/sandboxpools',
      item: '/envs/{name}/sandboxpools/{poolName}',
      verbs: ['create', 'update', 'delete', 'scale'],
    },
    detail: true,
    columns: [
      { id: 'name', describe: 'Pool name, derived from the env name and the resource shape.', filter: 'name' },
      { id: 'owningEnv', describe: 'The env this pool is a member of.', filter: 'owningEnv' },
      { id: 'replicas', path: 'spec.replicas', describe: 'Desired size. Owned by the autoscaler when its group is enabled.' },
      { id: 'idleReplicas', path: 'status.idleReplicas', describe: 'Pods available to claim right now.' },
      { id: 'runningReplicas', path: 'status.runningReplicas', describe: 'Pods currently claimed by a sandbox.' },
      { id: 'scalingGroup', describe: 'The autoscaling group this member belongs to.', filter: 'scalingGroup' },
      { id: 'phase', path: 'status.phase', describe: 'Pool phase.', filter: 'phase', optional: true },
    ],
    inParentDetail: true,
    // A pool is where the interesting numbers are, and the list view holds
    // back most of them to stay readable. The detail view is the place to show
    // the whole set — plus the rendered pod template, which is what a diff
    // against a template edit is actually read against.
    detailFields: [
      { id: 'name', describe: 'Pool name.' },
      { id: 'owningEnv', describe: 'The env this pool is a member of.' },
      { id: 'namespace', describe: 'Namespace the pool lives in.' },
      { id: 'phase', path: 'status.phase', describe: 'Pending, Ready, ScalingUp/Down, Degraded or Terminating.' },
      { id: 'replicas', path: 'spec.replicas', describe: 'Desired size. Owned by the autoscaler when its group is enabled.' },
      { id: 'idleReplicas', path: 'status.idleReplicas', describe: 'Pods available to claim right now.' },
      {
        id: 'unavailableIdleReplicas',
        path: 'status.unavailableIdleReplicas',
        describe: 'Idle pods that are not Ready — counted above, but unable to take a request.',
      },
      { id: 'runningReplicas', path: 'status.runningReplicas', describe: 'Pods currently claimed by a sandbox.' },
      { id: 'startingReplicas', path: 'status.startingReplicas', describe: 'Pods still starting up.' },
      { id: 'stoppingReplicas', path: 'status.stoppingReplicas', describe: 'Pods transitioning back to Idle.' },
      { id: 'failedReplicas', path: 'status.failedReplicas', describe: 'Pods in a Failed state.' },
      { id: 'pendingRequests', path: 'status.pendingRequests', describe: 'Claims queued against this pool.' },
      { id: 'cpu', describe: 'CPU per pod, as requested.' },
      { id: 'memory', describe: 'Memory per pod, as requested.' },
      { id: 'scalingGroup', describe: 'The autoscaling group this member belongs to.' },
      { id: 'templateVersion', describe: 'SandboxTemplate revision this pool was last rendered from.' },
      { id: 'updateRevision', path: 'status.updateRevision', describe: 'Template revision the pool is rolling towards.' },
      { id: 'updatedReplicas', path: 'status.updatedReplicas', describe: 'Pods already at updateRevision.' },
      { id: 'createdAt', describe: 'When the pool was created.' },
      {
        id: 'specYaml',
        describe: 'The rendered pod template, full text. What a template edit is diffed against.',
        text: true,
      },
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
    kind: 'scaling-group',
    plural: 'scaling-groups',
    parent: 'envs',
    describe:
      'An autoscaling group: the bounds and policies a set of member pools scales under. Named for what the API field and the pool column both call it — only the old route said "autoscaling".',
    docs: 'concepts/autoscaling',
    api: {
      list: '/envs/{name}/autoscaling/groups',
      item: '/envs/{name}/autoscaling/groups/{groupName}',
      // No create: a group comes into being when a member declares its
      // scalingGroup, and is collected when the last one stops. There is
      // nothing to POST, and offering one would suggest groups exist
      // independently of members.
      verbs: ['update', 'delete'],
    },
    detail: true,
    inParentDetail: true,
    columns: [
      { id: 'name', describe: 'Group name, as member pools reference it.', filter: 'name' },
      { id: 'enabled', describe: 'Whether the autoscaler acts on this group at all.', filter: 'enabled' },
      { id: 'minReplicas', describe: 'Floor. Absent means no floor.' },
      { id: 'maxReplicas', describe: 'Ceiling. Absent means no ceiling.' },
      {
        id: 'mode',
        path: 'scaleUpPolicy.mode',
        describe: 'Governs step size in both directions — scale-down mirrors it.',
        filter: 'mode',
      },
      { id: 'cooldownSeconds', path: 'scaleUpPolicy.cooldownSeconds', describe: 'Minimum gap between scale-ups.', optional: true },
      { id: 'idleTimeoutSeconds', path: 'scaleDownPolicy.idleTimeoutSeconds', describe: 'How long a Pod idles before it counts as removable.', optional: true },
    ],
    filters: [
      { key: 'name', describe: 'substring of the group name' },
      { key: 'enabled', describe: 'whether the group is active', values: ['true', 'false'] },
      { key: 'mode', describe: 'scale-up mode', values: ['Conservative', 'Default', 'Aggressive'] },
    ],
    // A group is four numbers and two policies; the list can only show three
    // of them before it stops being a table.
    detailFields: [
      { id: 'name', describe: 'Group name.' },
      { id: 'enabled', describe: 'Whether the autoscaler acts on this group at all.' },
      { id: 'minReplicas', describe: 'Floor on the group’s aggregate replicas.' },
      { id: 'maxReplicas', describe: 'Ceiling on the group’s aggregate replicas.' },
      { id: 'scaleUpMode', path: 'scaleUpPolicy.mode', describe: 'Governs step size in both directions.' },
      {
        id: 'cooldownSeconds',
        path: 'scaleUpPolicy.cooldownSeconds',
        describe: 'Minimum gap between two scale-ups.',
      },
      {
        id: 'idleThresholdSeconds',
        path: 'scaleUpPolicy.idleThresholdSeconds',
        describe: 'How long aggregate idle must stay zero before the proactive scale-up fires.',
      },
      {
        id: 'idleZeroQuietWindowSeconds',
        path: 'scaleUpPolicy.idleZeroQuietWindowSeconds',
        describe: 'Suppresses that trigger when nothing has been claimed for this long.',
      },
      {
        id: 'saturationCooldownSeconds',
        path: 'scaleUpPolicy.saturationCooldownSeconds',
        describe: 'How long a member stays marked saturated after a failed probe.',
      },
      {
        id: 'idleTimeoutSeconds',
        path: 'scaleDownPolicy.idleTimeoutSeconds',
        describe: 'How long a Pod idles before it counts as removable.',
      },
      {
        id: 'stabilizationSeconds',
        path: 'scaleDownPolicy.stabilizationSeconds',
        describe: 'Minimum gap between two scale-downs.',
      },
      {
        id: 'protectionWindowSeconds',
        path: 'scaleDownPolicy.protectionWindowSeconds',
        describe: 'How long a marked Pod can still be claimed, cancelling its deletion.',
      },
    ],
  },
  {
    kind: 'event',
    plural: 'events',
    parent: 'envs',
    describe: 'Recent Kubernetes events for this env and the objects it renders.',
    api: { list: '/envs/{name}/events' },
    // Rendered as a panel on the env detail page rather than a route of its own.
    consolePage: false,
    columns: [
      { id: 'type', describe: 'Normal or Warning.', filter: 'type' },
      { id: 'reason', describe: 'Short machine-readable cause.', filter: 'reason' },
      { id: 'object', describe: 'The object the event is about.' },
      { id: 'message', describe: 'What happened.' },
      { id: 'lastTimestamp', describe: 'When it last occurred.' },
    ],
    filters: [
      { key: 'type', describe: 'event type', values: ['Normal', 'Warning'] },
      { key: 'reason', describe: 'substring of the reason' },
    ],
  },
  {
    kind: 'sandbox',
    plural: 'sandboxes',
    describe:
      'A running sandbox. Created and driven with the E2B SDK, not this CLI — these are read views of what that produced.',
    docs: 'concepts/inplace-update',
    api: { list: '/sandboxes', item: '/sandboxes/{sandboxId}' },
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
    detailFields: [
      { id: 'sandboxId', describe: 'Sandbox id.' },
      { id: 'status', describe: 'Lifecycle status.' },
      { id: 'envName', describe: 'The env it was claimed from.' },
      { id: 'poolName', describe: 'The member pool it was claimed from.' },
      { id: 'namespace', describe: 'Namespace the pod runs in.' },
      { id: 'podName', describe: 'The Kubernetes pod backing it.' },
      { id: 'team', describe: 'Owning team.' },
      { id: 'user', describe: 'Owning user.' },
      { id: 'nodeName', describe: 'Node the pod was scheduled onto.' },
      { id: 'cpu', describe: 'CPU allocated to the sandbox.' },
      { id: 'memory', describe: 'Memory allocated to the sandbox.' },
      { id: 'claimedAt', describe: 'When it was claimed.' },
      { id: 'startedAt', describe: 'When it reached Running.' },
      { id: 'terminatedAt', describe: 'When its workload ended.' },
      { id: 'durationSeconds', describe: 'Wall-clock seconds, for a finished sandbox.' },
      { id: 'idleTimeoutSeconds', describe: 'Idle timeout in effect for this sandbox alone.' },
      { id: 'failureReason', describe: 'Machine-readable reason, when it failed.' },
      { id: 'exitCode', describe: 'Exit code of the main container.' },
      { id: 'failureMessage', describe: 'What went wrong, when it failed.' },
      { id: 'metadata', describe: 'Key/value pairs supplied on create.' },
      { id: 'containerImages', describe: 'Image per container in the pod.' },
    ],
    views: [
      {
        segment: 'logs',
        describe: 'Logs for this sandbox.',
        api: '/sandboxes/{sandboxId}/logs',
        shape: 'logs',
      },
    ],
  },
  {
    kind: 'template',
    plural: 'templates',
    describe: 'Reusable Pod templates an env can be rendered from.',
    docs: 'concepts/templates',
    api: { list: '/sandbox-templates', item: '/sandbox-templates/{name}' },
    detail: true,
    columns: [
      { id: 'name', describe: 'Template name.', filter: 'name' },
      { id: 'version', describe: 'spec.version — names a template revision.' },
      { id: 'description', describe: 'What this template is for.' },
    ],
    filters: [{ key: 'name', describe: 'substring of the template name' }],
    helpNote:
      'This is the catalog everyone reads. Writing a template is `abx admin-templates`, which an admin key is required for.',
    // The console's template page renders `docs`, and so does this: the whole
    // point of a template's documentation is to be read, and it is safe to
    // print for the same reason an env's is now: neither carries a credential.
    // `${AGBX_API_KEY}` arrives as a placeholder, which is what the console
    // fills in and what a reader is told to replace.
    detailFields: [
      { id: 'name', describe: 'Template name.' },
      { id: 'version', describe: 'spec.version — names a template revision.' },
      { id: 'description', describe: 'What this template is for.' },
      { id: 'syncSource', describe: 'global (synced from the hub) or local.' },
      { id: 'createdAt', describe: 'When the template was created.' },
      { id: 'docs', describe: 'Rendered Markdown documentation, full text.', text: true },
      {
        id: 'crdYaml',
        describe: 'The raw SandboxTemplate object as YAML, full text. Includes the resourceVersion a PUT must carry back.',
        text: true,
      },
    ],
  },
  {
    kind: 'instancetype',
    plural: 'instancetypes',
    describe: 'The quota classes a pool can be sized against.',
    docs: 'concepts/pools',
    api: { list: '/instancetypes' },
    // No console route: the console surfaces these inside the pool form's
    // picker rather than as a page, so there is nothing to link to.
    consolePage: false,
    columns: [
      { id: 'name', describe: 'Instance type name.', filter: 'name' },
      { id: 'baseResources', path: 'baseResources.limits', describe: 'CPU / memory / GPU per unit.' },
      { id: 'cost', describe: 'Relative cost, for comparison only.', optional: true },
    ],
    filters: [{ key: 'name', describe: 'substring of the instance type name' }],
    gate: 'instanceType',
  },
  {
    kind: 'quota',
    plural: 'quotas',
    describe: 'Per-team quota and how much of it is in use.',
    docs: 'concepts/pools',
    api: { list: '/quotas' },
    // One row per (quota × instance type), because that is the unit a caller
    // decides on: a pool is sized against one quota AND one instance type, so
    // "which quota do I put in my env" is answered per instance type and never
    // by a quota's total. The quota's own `name` is the value a pool's
    // `labels["quota.scitix.ai/url"]` carries.
    expand: { field: 'resources', key: 'instanceType', keepEmpty: true },
    columns: [
      { id: 'name', describe: 'Quota url — what a pool\'s quota label carries.', filter: 'name' },
      { id: 'team', describe: 'Owning team.', filter: 'team' },
      {
        id: 'poolType',
        path: 'metadata["quota.scitix.ai/pool-type"]',
        describe: 'exclusive, shared, ondemand, idle or spot.',
        filter: 'poolType',
      },
      { id: 'pool', path: 'metadata["quota.scitix.ai/pool-name"]', describe: 'The resource pool behind it.' },
      { id: 'instanceType', describe: 'Instance type the numbers on this row belong to.' },
      { id: 'used', path: 'resources.used', describe: 'In use now.' },
      { id: 'ceiling', derive: 'quota-ceiling', describe: 'The enforced limit: a number, 0, or unlimited.' },
      { id: 'free', derive: 'quota-free', describe: 'Room left.' },
      { id: 'note', derive: 'quota-note', describe: 'What a ceiling of 0 or unlimited means here.' },
    ],
    filters: [
      { key: 'name', describe: 'substring of the quota name' },
      { key: 'team', describe: 'owning team' },
      {
        key: 'poolType',
        describe: 'pool type',
        values: ['exclusive', 'shared', 'ondemand', 'idle', 'spot'],
      },
    ],
    // The one thing a column cannot say for itself: a 0 and an unlimited are
    // both "no number you can spend", and reading either as the other sends a
    // caller at a quota that will refuse them.
    helpNote:
      'ceiling is the number the platform enforces, and it is one of three things. A number is the cap. 0 is a hard zero — nothing allocated, and a submission against it is refused. unlimited means this deployment skips the quota check for that pool, which is how the ondemand and spot pools are built: the number is not a limit and what decides is the pool\'s own stock, so it is worth trying. `name` is the quota url a pool carries as labels["quota.scitix.ai/url"].',
    gate: 'quota',
  },
  {
    kind: 'volume',
    plural: 'volumes',
    describe: 'Persistent volumes a sandbox can mount, as this tenant sees them.',
    api: { list: '/volumes' },
    // Surfaced in the env form's mount picker rather than as a page.
    consolePage: false,
    columns: [
      { id: 'claimName', describe: 'PVC name, which is what an env override references.', filter: 'claimName' },
      { id: 'displayName', describe: 'Human label.' },
      { id: 'capacity', describe: 'Declared size.' },
      { id: 'storageClass', describe: 'Backing storage class.', optional: true },
      { id: 'accessModes', describe: 'ReadWriteMany and friends.', optional: true },
    ],
    filters: [{ key: 'claimName', describe: 'substring of the claim name' }],
  },
  {
    kind: 'api-key',
    plural: 'api-keys',
    describe:
      'Credentials issued to this tenant. An `agent` key is the restricted kind: its writes wait for a person.',
    api: {
      list: '/api-keys',
      item: '/api-keys/{name}',
      itemReadable: false,
      verbs: ['create', 'delete'],
      agentForbidden: {
        create:
          'minting a credential would let an agent issue one without the agent restriction, and step out of its own approval gate',
      },
    },
    columns: [
      { id: 'keyId', describe: 'Key id. The value itself is shown once, at creation.', filter: 'keyId' },
      { id: 'mode', describe: 'unrestricted, or agent — whose writes need approval.', filter: 'mode' },
      { id: 'user', describe: 'Who it acts as.', filter: 'user' },
      { id: 'team', describe: 'Which team it acts in.', filter: 'team' },
      { id: 'role', describe: 'tenant or admin.', filter: 'role' },
      { id: 'description', describe: 'What it was issued for.', optional: true },
      { id: 'issuedAt', describe: 'When it was issued.', optional: true },
    ],
    filters: [
      { key: 'keyId', describe: 'substring of the key id' },
      { key: 'mode', describe: 'restriction mode', values: ['unrestricted', 'agent'] },
      { key: 'user', describe: 'acting user' },
      { key: 'team', describe: 'acting team' },
      { key: 'role', describe: 'granted role' },
    ],
  },
  {
    kind: 'admin-api-key',
    plural: 'admin-api-keys',
    describe: 'Every credential on the cluster. Admin only.',
    admin: true,
    api: {
      list: '/admin/api-keys',
      item: '/admin/api-keys/{name}',
      itemReadable: false,
      verbs: ['create', 'delete'],
      agentForbidden: {
        create: 'minting a credential would let an agent step out of its own approval gate',
      },
    },
    columns: [
      { id: 'keyId', describe: 'Key id.', filter: 'keyId' },
      { id: 'mode', describe: 'unrestricted, or agent.', filter: 'mode' },
      { id: 'user', describe: 'Who it acts as.', filter: 'user' },
      { id: 'team', describe: 'Which team it acts in.', filter: 'team' },
      { id: 'role', describe: 'tenant or admin.', filter: 'role' },
      { id: 'description', describe: 'What it was issued for.', optional: true },
    ],
    filters: [
      { key: 'keyId', describe: 'substring of the key id' },
      { key: 'mode', describe: 'restriction mode', values: ['unrestricted', 'agent'] },
      { key: 'user', describe: 'acting user' },
      { key: 'team', describe: 'acting team' },
      { key: 'role', describe: 'granted role' },
    ],
    actions: [
      {
        name: 'promote',
        describe: 'Raise a key to admin.',
        method: 'POST',
        path: '/admin/api-keys/{name}/promote',
        agentForbidden: 'promoting a key is minting authority by another name',
      },
    ],
  },
  {
    kind: 'approval',
    plural: 'approvals',
    describe:
      "Writes an agent key asked for and a person has not released yet, plus the standing grants that stop it asking again.",
    api: { list: '/approvals', listField: 'pending', item: '/approvals/{approvalId}' },
    detail: true,
    columns: [
      { id: 'id', describe: 'Approval id.', filter: 'id' },
      { id: 'operation', describe: 'What was asked for.', filter: 'operation' },
      { id: 'summary', describe: 'The request in one line.' },
      { id: 'status', describe: 'pending, approved, denied or expired.', filter: 'status' },
      { id: 'principal', path: 'principal.user', describe: 'Who asked.', filter: 'principal' },
      { id: 'expiresAt', describe: 'When it stops being answerable.', optional: true },
      { id: 'method', describe: 'HTTP method of the held request.', optional: true },
      { id: 'path', describe: 'Path of the held request.', optional: true },
    ],
    filters: [
      { key: 'id', describe: 'substring of the approval id' },
      { key: 'operation', describe: 'the operation asked for' },
      { key: 'status', describe: 'approval status' },
      { key: 'principal', describe: 'who asked' },
    ],
    // An approval is a decision someone makes in the console. What the CLI can
    // honestly answer is what was asked for, by whom, and how long is left —
    // which is exactly what the list holds back to stay narrow.
    detailFields: [
      { id: 'id', describe: 'Approval id.' },
      { id: 'status', describe: 'pending, approved, denied or expired.' },
      { id: 'operation', describe: 'The operation being asked for. A grant is written against this.' },
      { id: 'summary', describe: 'The request in one line, naming the object.' },
      { id: 'method', describe: 'HTTP method of the held request.' },
      { id: 'path', describe: 'Path of the held request.' },
      { id: 'user', path: 'principal.user', describe: 'Who asked.' },
      { id: 'team', path: 'principal.team', describe: 'The team they asked as.' },
      { id: 'keyId', path: 'principal.keyId', describe: 'Which credential asked.' },
      { id: 'sessionId', describe: 'The session it belongs to, when the caller declared one.' },
      {
        id: 'onceOnly',
        describe: 'True for destructive and credential-minting calls: "for this session" is not on offer.',
      },
      { id: 'createdAt', describe: 'When it was asked for.' },
      { id: 'expiresAt', describe: 'When it stops being answerable.' },
      { id: 'decidedBy', describe: 'Who released or refused it.' },
      { id: 'decidedAt', describe: 'When they did.' },
    ],
    actions: [
      {
        name: 'approve',
        describe: 'Release the held write.',
        method: 'POST',
        path: '/approvals/{approvalId}/decision',
        body: { decision: 'approve' },
        agentForbidden:
          'the point of the gate is that someone other than the agent decides — a key cannot approve its own request',
      },
      {
        name: 'deny',
        describe: 'Refuse the held write.',
        method: 'POST',
        path: '/approvals/{approvalId}/decision',
        body: { decision: 'deny' },
        agentForbidden: 'deciding is the person\'s half of the gate, not the agent\'s',
      },
    ],
  },
  {
    kind: 'grant',
    plural: 'grants',
    describe:
      'A standing approval: an agent key that holds one stops asking for that operation until it is revoked.',
    // Listed from the same response as approvals, under its other field: the
    // two are opposite ends of one mechanism and the API returns them together.
    api: {
      list: '/approvals',
      listField: 'grants',
      item: '/approvals/grants/{grantId}',
      itemReadable: false,
      verbs: ['delete'],
    },
    consolePage: false,
    columns: [
      { id: 'id', describe: 'Grant id.', filter: 'id' },
      { id: 'operation', describe: 'What it covers.', filter: 'operation' },
      { id: 'scope', describe: 'session, or key — which ends only when revoked.', filter: 'scope' },
      { id: 'principal', path: 'principal.user', describe: 'Whose key holds it.', filter: 'principal' },
      { id: 'grantedBy', describe: 'Who granted it.' },
      { id: 'expiresAt', describe: 'When it lapses. Absent for a key-scoped grant.', optional: true },
    ],
    filters: [
      { key: 'id', describe: 'substring of the grant id' },
      { key: 'operation', describe: 'the operation it covers' },
      { key: 'scope', describe: 'grant scope', values: ['session', 'key'] },
      { key: 'principal', describe: 'whose key holds it' },
    ],
  },
  {
    kind: 'team',
    plural: 'admin-teams',
    describe: 'Teams on this cluster. Admin only.',
    api: { list: '/admin/teams' },
    admin: true,
    consolePage: false,
    columns: [{ id: 'name', describe: 'Team name.', filter: 'name' }],
    filters: [{ key: 'name', describe: 'substring of the team name' }],
  },
  {
    kind: 'user',
    plural: 'users',
    parent: 'admin-teams',
    describe: 'Members of one team. Admin only.',
    api: { list: '/admin/teams/{team}/users' },
    admin: true,
    consolePage: false,
    columns: [{ id: 'name', describe: 'Username.', filter: 'name' }],
    filters: [{ key: 'name', describe: 'substring of the username' }],
  },
  {
    kind: 'namespace',
    plural: 'admin-namespaces',
    describe: 'Kubernetes namespaces this cluster maps tenants into. Admin only.',
    api: { list: '/admin/namespaces' },
    admin: true,
    consolePage: false,
    columns: [{ id: 'name', describe: 'Namespace name.', filter: 'name' }],
    filters: [{ key: 'name', describe: 'substring of the namespace' }],
  },
  {
    kind: 'statistics',
    plural: 'statistics',
    describe: 'Sandbox counts for this tenant, by status and namespace.',
    api: { list: '/statistics/sandboxes' },
    consolePage: false,
    columns: [
      { id: 'total', path: 'statistics.total', describe: 'Sandboxes in total.' },
      { id: 'byStatus', path: 'statistics.byStatus', describe: 'Count per lifecycle status.' },
      { id: 'byNamespace', path: 'statistics.byNamespace', describe: 'Count per namespace.', optional: true },
    ],
  },
  {
    kind: 'admin-statistics',
    plural: 'admin-statistics',
    describe: 'The same counts across every tenant. Admin only.',
    api: { list: '/admin/statistics/sandboxes' },
    admin: true,
    consolePage: false,
    columns: [
      { id: 'total', path: 'statistics.total', describe: 'Sandboxes in total.' },
      { id: 'byStatus', path: 'statistics.byStatus', describe: 'Count per lifecycle status.' },
      { id: 'byNamespace', path: 'statistics.byNamespace', describe: 'Count per namespace.' },
    ],
  },
  {
    kind: 'admin-template',
    plural: 'admin-templates',
    describe:
      'Templates as their owner edits them. `templates` is the catalog everyone reads; this is the write side, and it is admin only.',
    docs: 'concepts/templates',
    api: {
      list: '/admin/sandbox-templates',
      item: '/admin/sandbox-templates/{name}',
      itemReadable: false,
      verbs: ['create', 'update', 'delete'],
    },
    admin: true,
    consolePage: false,
    columns: [
      { id: 'name', describe: 'Template name.', filter: 'name' },
      { id: 'version', describe: 'spec.version.' },
      { id: 'description', describe: 'What this template is for.', optional: true },
    ],
    filters: [{ key: 'name', describe: 'substring of the template name' }],
  },
] as const

export function resourceOf(token: string): ResourceSpec | undefined {
  // Plurals only, and deliberately: `abx env` and `abx envs` being two
  // spellings of one command means every error message has to know which one a
  // reader used to explain itself, and half the plurals are irregular anyway
  // (scaling-groups, admin-statistics). One token per resource, and the
  // refusal names the set that works.
  return RESOURCES.find((r) => r.plural === token)
}

/** Resources addressable at the top level — everything that is not a child. */
export function rootResources(): readonly ResourceSpec[] {
  return RESOURCES.filter((r) => !r.parent)
}

/** The children of a resource, which are also its sub-resource segments. */
export function childrenOf(plural: Segment): readonly ResourceSpec[] {
  return RESOURCES.filter((r) => r.parent === plural)
}

/**
 * Every operation the registry claims, in the API's own spelling.
 *
 * The CLI's capability manifest is this list rather than a maintained copy of
 * it, so "the CLI grew a resource" and "the CLI reaches this operation" cannot
 * disagree. `scale` deliberately contributes nothing here: it reuses the PUT
 * that `update` already declares.
 */
export function operations(): readonly ApiOperation[] {
  const out: ApiOperation[] = []
  for (const r of RESOURCES) {
    const verbs = new Set(r.api.verbs ?? [])
    if (r.api.list) {
      out.push({ method: 'GET', path: r.api.list, resource: r.plural })
      if (verbs.has('create')) out.push({ method: 'POST', path: r.api.list, resource: r.plural })
    }
    if (r.api.item) {
      if (r.api.itemReadable !== false) {
        out.push({ method: 'GET', path: r.api.item, resource: r.plural })
      }
      if (verbs.has('update')) out.push({ method: 'PUT', path: r.api.item, resource: r.plural })
      if (verbs.has('delete')) out.push({ method: 'DELETE', path: r.api.item, resource: r.plural })
    }
    for (const v of r.views ?? []) {
      if (v.api) out.push({ method: 'GET', path: v.api, resource: r.plural })
    }
    for (const a of r.actions ?? []) {
      out.push({ method: a.method, path: a.path, resource: r.plural })
    }
  }
  // Two actions can share one endpoint — approve and deny are one POST with
  // different bodies — so the same operation must not be claimed twice.
  const seen = new Set<string>()
  return out.filter((o) => {
    const k = `${o.method} ${o.path}`
    if (seen.has(k)) return false
    seen.add(k)
    return true
  })
}
