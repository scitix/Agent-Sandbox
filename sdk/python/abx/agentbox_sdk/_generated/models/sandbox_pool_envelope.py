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
  from ..models.sandbox_pool import SandboxPool
  from ..models.upsert_sandbox_pool_request import UpsertSandboxPoolRequest





T = TypeVar("T", bound="SandboxPoolEnvelope")



@_attrs_define
class SandboxPoolEnvelope:
    """ 
        Attributes:
            template (SandboxPool):  Example: {'name': 'poolname', 'namespace': 't-team-user', 'cpu': '1', 'memory': '16Gi',
                'team': 'team', 'user': 'user', 'spec': {'replicas': 2}, 'status': {'phase': 'Ready', 'idleReplicas': 1,
                'unavailableIdleReplicas': 0, 'runningReplicas': 1, 'startingReplicas': 0, 'stoppingReplicas': 0,
                'failedReplicas': 0}}.
            editable (UpsertSandboxPoolRequest | Unset): Add a member SandboxPool to an Env. The server derives:
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
                 Example: {'instanceType': 'sci.c23-2', 'multiplier': 1, 'replicas': 1, 'minReplicas': 0, 'maxReplicas': 4,
                'inlineResources': {'requests': {'cpu': '100m', 'memory': '500Mi'}, 'limits': {'cpu': '100m', 'memory':
                '500Mi'}}, 'labels': {'quota.scitix.ai/url': 'https://quota.example/q/1'}}.
     """

    template: SandboxPool
    editable: UpsertSandboxPoolRequest | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.sandbox_pool import SandboxPool # noqa: PLC0415
        from ..models.upsert_sandbox_pool_request import UpsertSandboxPoolRequest # noqa: PLC0415
        template = self.template.to_dict()

        editable: dict[str, Any] | Unset = UNSET
        if not isinstance(self.editable, Unset):
            editable = self.editable.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "template": template,
        })
        if editable is not UNSET:
            field_dict["editable"] = editable

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.sandbox_pool import SandboxPool # noqa: PLC0415
        from ..models.upsert_sandbox_pool_request import UpsertSandboxPoolRequest # noqa: PLC0415
        d = dict(src_dict)
        template = SandboxPool.from_dict(d.pop("template"))




        _editable = d.pop("editable", UNSET)
        editable: UpsertSandboxPoolRequest | Unset
        if isinstance(_editable,  Unset):
            editable = UNSET
        else:
            editable = UpsertSandboxPoolRequest.from_dict(_editable)




        sandbox_pool_envelope = cls(
            template=template,
            editable=editable,
        )


        sandbox_pool_envelope.additional_properties = d
        return sandbox_pool_envelope

    @property
    def additional_keys(self) -> list[str]:
        return list(self.additional_properties.keys())

    def __getitem__(self, key: str) -> Any:
        return self.additional_properties[key]

    def __setitem__(self, key: str, value: Any) -> None:
        self.additional_properties[key] = value

    def __delitem__(self, key: str) -> None:
        del self.additional_properties[key]

    def __contains__(self, key: str) -> bool:
        return key in self.additional_properties
