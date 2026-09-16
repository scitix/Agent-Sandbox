# Copyright 2026 ScitiX
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from __future__ import annotations

from collections.abc import Mapping
from typing import Any, TypeVar, BinaryIO, TextIO, TYPE_CHECKING, Generator

from attrs import define as _attrs_define
from attrs import field as _attrs_field

from ..types import UNSET, Unset

from ..types import UNSET, Unset
from typing import cast

if TYPE_CHECKING:
  from ..models.env_update_strategy import EnvUpdateStrategy
  from ..models.resource_requirements import ResourceRequirements
  from ..models.string_map import StringMap





T = TypeVar("T", bound="UpsertSandboxPoolRequest")



@_attrs_define
class UpsertSandboxPoolRequest:
    """ Add a member SandboxPool to an Env. The server derives:
      - `name`         = "{envName}-{resourceKey}[-{quotaShort}]"
      - `scalingGroup` = `resourceKey` (e.g. "2c8Gi")

    where `resourceKey` is `instancetype.DeriveResourceKey(effective resources)` and
    `quotaShort` (when a quota label is supplied) is `quotaProvider.DeriveShortName(quotaID)`.
    Members in the same `scalingGroup` share an autoscaling policy.

    WHICH OF THE SHAPES BELOW IS ACCEPTED IS THE ENV'S TO SAY, not the
    caller's — read `poolSizing` off the Env this Pool joins (Template
    detail and Env detail both carry it; `abx envs <name>` prints it).

    Billed Env (`poolSizing: billed`) — the Template is billed, so the
    Pool must name what it spends:
      - `instanceType` (+ optional `multiplier`) alone → the Pod is sized to the full
        `instanceType × multiplier` envelope (default `multiplier` = 1).
      - `instanceType` (+ `multiplier`) AND `inlineResources` together → `instanceType ×
        multiplier` is the reservation/billing envelope, while `inlineResources` is the
        actual (possibly rounded-down) Pod request. Every dimension of `inlineResources`
        must be ≤ the envelope (round down allowed, round up rejected with 400); the
        reservation still charges quota for the whole instance.
      - `labels` must carry `quota.scitix.ai/url`.

    Free-form Env (`poolSizing: free-form`) — the Template is one the
    deployment does not bill, so the Pool is sized directly:
      - `inlineResources` alone → explicit per-Pool resource requests/limits.
      - `instanceType`, `multiplier` and the quota label are REJECTED (400):
        an instance type buys an instance nobody reserved, and a quota label
        on a Pool that is never submitted for reservation is a claim the
        server cannot honour.

    Unmanaged Env (`poolSizing: either`) — this deployment states no rule,
    so both shapes are accepted and the caller picks. Every deployment
    behaved this way before the rule existed.

    Under the two managed values there is no per-Pool choice: the shape
    follows from the Env, so two Pools of one Env are always sized the same
    way.
    `scalingGroup` / pool name are derived from the effective Pod request (the rounded-down
    `inlineResources` when supplied, else the full envelope), so the name reflects the Pod's
    real size and Pools downsized differently land in distinct scaling groups.

    This is also what an update takes, and what `GET` returns as `editable`:
    one body for create, update and export, so a client edits what the API
    handed it rather than translating between two subsets that drift.

    The fields marked `x-immutable` describe the Pool's SHAPE and are fixed
    at create. An update must carry them back unchanged — omitting one or
    changing one is a 400 that names the value in force, because a body that
    loses the instance type is a caller bug, not a request for a smaller
    machine.

        Example:
            {'instanceType': 'sci.c23-2', 'multiplier': 1, 'replicas': 1, 'minReplicas': 0, 'maxReplicas': 4,
                'inlineResources': {'requests': {'cpu': '100m', 'memory': '500Mi'}, 'limits': {'cpu': '100m', 'memory':
                '500Mi'}}, 'labels': {'quota.scitix.ai/url': 'https://quota.example/q/1'}}

        Attributes:
            instance_type (str | Unset): InstanceType catalog entry. Required when the Pool's Env is billed
                (env.poolSizing=billed); rejected when its Env is free-form (env.poolSizing=free-form); the caller's choice when
                the Env is unmanaged (env.poolSizing=either). May be combined with inlineResources to reserve a whole instance
                while running a smaller (rounded-down) Pod.
            multiplier (int | Unset): Multiplier applied to the InstanceType base resources to form the reservation
                envelope. Defaults to 1.
            inline_resources (ResourceRequirements | Unset): Subset of Kubernetes corev1.ResourceRequirements used for per-
                Pool resource sizing on EnvClusterMember.inlineResources.
            replicas (int | Unset): Initial replica count. Autoscaling, once enabled on this scalingGroup, owns subsequent
                changes.
            min_replicas (int | Unset): Lower bound on this pool's replicas, enforced as a per-member scale-down floor by
                the Env autoscaler.
            max_replicas (int | Unset): Upper bound on this pool's replicas, enforced when the Env autoscaler distributes
                scale-up delta.
            labels (StringMap | Unset): Free-form string key/value metadata (labels or annotations).
            annotations (StringMap | Unset): Free-form string key/value metadata (labels or annotations).
            update_strategy (EnvUpdateStrategy | Unset): Automatic rollout policy for member Pools when their rendered idle-
                Pod identity (Template edit, image / gateway override) changes. Rollout mode is always Recreate: stale idle Pods
                are rebuilt; claimed (Running/Starting) Pods are never disrupted and roll after returning to Idle.
     """

    instance_type: str | Unset = UNSET
    multiplier: int | Unset = UNSET
    inline_resources: ResourceRequirements | Unset = UNSET
    replicas: int | Unset = UNSET
    min_replicas: int | Unset = UNSET
    max_replicas: int | Unset = UNSET
    labels: StringMap | Unset = UNSET
    annotations: StringMap | Unset = UNSET
    update_strategy: EnvUpdateStrategy | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        from ..models.env_update_strategy import EnvUpdateStrategy # noqa: PLC0415
        from ..models.resource_requirements import ResourceRequirements # noqa: PLC0415
        from ..models.string_map import StringMap # noqa: PLC0415
        instance_type = self.instance_type

        multiplier = self.multiplier

        inline_resources: dict[str, Any] | Unset = UNSET
        if not isinstance(self.inline_resources, Unset):
            inline_resources = self.inline_resources.to_dict()

        replicas = self.replicas

        min_replicas = self.min_replicas

        max_replicas = self.max_replicas

        labels: dict[str, Any] | Unset = UNSET
        if not isinstance(self.labels, Unset):
            labels = self.labels.to_dict()

        annotations: dict[str, Any] | Unset = UNSET
        if not isinstance(self.annotations, Unset):
            annotations = self.annotations.to_dict()

        update_strategy: dict[str, Any] | Unset = UNSET
        if not isinstance(self.update_strategy, Unset):
            update_strategy = self.update_strategy.to_dict()


        field_dict: dict[str, Any] = {}

        field_dict.update({
        })
        if instance_type is not UNSET:
            field_dict["instanceType"] = instance_type
        if multiplier is not UNSET:
            field_dict["multiplier"] = multiplier
        if inline_resources is not UNSET:
            field_dict["inlineResources"] = inline_resources
        if replicas is not UNSET:
            field_dict["replicas"] = replicas
        if min_replicas is not UNSET:
            field_dict["minReplicas"] = min_replicas
        if max_replicas is not UNSET:
            field_dict["maxReplicas"] = max_replicas
        if labels is not UNSET:
            field_dict["labels"] = labels
        if annotations is not UNSET:
            field_dict["annotations"] = annotations
        if update_strategy is not UNSET:
            field_dict["updateStrategy"] = update_strategy

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.env_update_strategy import EnvUpdateStrategy # noqa: PLC0415
        from ..models.resource_requirements import ResourceRequirements # noqa: PLC0415
        from ..models.string_map import StringMap # noqa: PLC0415
        d = dict(src_dict)
        instance_type = d.pop("instanceType", UNSET)

        multiplier = d.pop("multiplier", UNSET)

        _inline_resources = d.pop("inlineResources", UNSET)
        inline_resources: ResourceRequirements | Unset
        if isinstance(_inline_resources,  Unset):
            inline_resources = UNSET
        else:
            inline_resources = ResourceRequirements.from_dict(_inline_resources)




        replicas = d.pop("replicas", UNSET)

        min_replicas = d.pop("minReplicas", UNSET)

        max_replicas = d.pop("maxReplicas", UNSET)

        _labels = d.pop("labels", UNSET)
        labels: StringMap | Unset
        if isinstance(_labels,  Unset):
            labels = UNSET
        else:
            labels = StringMap.from_dict(_labels)




        _annotations = d.pop("annotations", UNSET)
        annotations: StringMap | Unset
        if isinstance(_annotations,  Unset):
            annotations = UNSET
        else:
            annotations = StringMap.from_dict(_annotations)




        _update_strategy = d.pop("updateStrategy", UNSET)
        update_strategy: EnvUpdateStrategy | Unset
        if isinstance(_update_strategy,  Unset):
            update_strategy = UNSET
        else:
            update_strategy = EnvUpdateStrategy.from_dict(_update_strategy)




        upsert_sandbox_pool_request = cls(
            instance_type=instance_type,
            multiplier=multiplier,
            inline_resources=inline_resources,
            replicas=replicas,
            min_replicas=min_replicas,
            max_replicas=max_replicas,
            labels=labels,
            annotations=annotations,
            update_strategy=update_strategy,
        )

        return upsert_sandbox_pool_request

