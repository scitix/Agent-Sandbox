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

from ..models.pool_sizing import PoolSizing
from ..models.sandbox_env_summary_mode import SandboxEnvSummaryMode
from ..types import UNSET, Unset
from typing import cast
import datetime






T = TypeVar("T", bound="SandboxEnvSummary")



@_attrs_define
class SandboxEnvSummary:
    """ Lightweight summary returned by the List endpoint — omits the full spec (autoscaling policies, per-member config)
    and detailed per-member status. Fetch GET /envs/{name} for the complete SandboxEnv.

        Attributes:
            name (str): Name of the SandboxEnv (RFC 1123 DNS label, unique within its namespace).
            namespace (str | Unset):
            team (str | Unset):
            user (str | Unset):
            created_at (datetime.datetime | Unset): RFC 3339 timestamp when the Env was created.
            template_name (str | Unset): The bound SandboxTemplate name (from spec.templateRef.name).
            mode (SandboxEnvSummaryMode | Unset): How the Env satisfies sandbox-create requests.
            member_count (int | Unset): Total member Pools across all cluster segments.
            desired_replicas (int | Unset): Env-wide sum of every member Pool's desired replicas.
            running_replicas (int | Unset): Env-wide sum of every member Pool's running replicas.
            idle_replicas (int | Unset): Env-wide sum of every member Pool's idle replicas.
            scaling_group_count (int | Unset): Total number of autoscaling groups declared on the Env.
            autoscaling_enabled_group_count (int | Unset): Number of autoscaling groups with enabled=true. There is no Env-
                level autoscaling switch; autoscaling is toggled per group.
            ready (bool | Unset): True when the Env's Ready condition is True (all members Active).
            pool_sizing (PoolSizing | Unset): How the member Pools of a template — and therefore of an Env bound to
                it — must be sized. Reported by the quota provider that governs the
                deployment, on the template itself and (stamped onto it) on the Env, so
                that a console, a CLI or any other client reads the answer instead of
                re-deriving the rule.

                Three values, not two: "billed" and "free-form" are a deployment with a
                billing rule deciding per template, while "either" is a deployment with
                no rule at all — which must keep letting the caller choose, because an
                InstanceType catalog with no quota backend is a supported way to size
                Pools.

                  - `billed` — the template's Pools spend quota. A Pool MUST carry the
                    `quota.scitix.ai/url` label AND an `instanceType` (with an optional
                    `multiplier`). It is refused without either: the quota is what the
                    Pool is charged against, and the instance type is the unit it is
                    charged in. This is the shape ordinary users' templates take.
                  - `free-form` — the template is not billed, so a Pool MUST be sized
                    by `inlineResources` alone. `instanceType`, `multiplier` and the
                    quota label are refused with 400, because each one asserts
                    something the server would not honour. Templates reserved for
                    specific callers take this shape.
                  - `either` — this deployment has no billing rule (no quota backend,
                    or one that states no policy), so both shapes are accepted and the
                    caller picks. This is the behaviour of every deployment before the
                    rule existed, and of the open-source build.

                Read-only. The value mirrors the template's and is maintained by the
                API server at Env create and by the Env reconciler on every pass; it is
                never part of a write body.
     """

    name: str
    namespace: str | Unset = UNSET
    team: str | Unset = UNSET
    user: str | Unset = UNSET
    created_at: datetime.datetime | Unset = UNSET
    template_name: str | Unset = UNSET
    mode: SandboxEnvSummaryMode | Unset = UNSET
    member_count: int | Unset = UNSET
    desired_replicas: int | Unset = UNSET
    running_replicas: int | Unset = UNSET
    idle_replicas: int | Unset = UNSET
    scaling_group_count: int | Unset = UNSET
    autoscaling_enabled_group_count: int | Unset = UNSET
    ready: bool | Unset = UNSET
    pool_sizing: PoolSizing | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        name = self.name

        namespace = self.namespace

        team = self.team

        user = self.user

        created_at: str | Unset = UNSET
        if not isinstance(self.created_at, Unset):
            created_at = self.created_at.isoformat()

        template_name = self.template_name

        mode: str | Unset = UNSET
        if not isinstance(self.mode, Unset):
            mode = self.mode.value


        member_count = self.member_count

        desired_replicas = self.desired_replicas

        running_replicas = self.running_replicas

        idle_replicas = self.idle_replicas

        scaling_group_count = self.scaling_group_count

        autoscaling_enabled_group_count = self.autoscaling_enabled_group_count

        ready = self.ready

        pool_sizing: str | Unset = UNSET
        if not isinstance(self.pool_sizing, Unset):
            pool_sizing = self.pool_sizing.value



        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "name": name,
        })
        if namespace is not UNSET:
            field_dict["namespace"] = namespace
        if team is not UNSET:
            field_dict["team"] = team
        if user is not UNSET:
            field_dict["user"] = user
        if created_at is not UNSET:
            field_dict["createdAt"] = created_at
        if template_name is not UNSET:
            field_dict["templateName"] = template_name
        if mode is not UNSET:
            field_dict["mode"] = mode
        if member_count is not UNSET:
            field_dict["memberCount"] = member_count
        if desired_replicas is not UNSET:
            field_dict["desiredReplicas"] = desired_replicas
        if running_replicas is not UNSET:
            field_dict["runningReplicas"] = running_replicas
        if idle_replicas is not UNSET:
            field_dict["idleReplicas"] = idle_replicas
        if scaling_group_count is not UNSET:
            field_dict["scalingGroupCount"] = scaling_group_count
        if autoscaling_enabled_group_count is not UNSET:
            field_dict["autoscalingEnabledGroupCount"] = autoscaling_enabled_group_count
        if ready is not UNSET:
            field_dict["ready"] = ready
        if pool_sizing is not UNSET:
            field_dict["poolSizing"] = pool_sizing

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        name = d.pop("name")

        namespace = d.pop("namespace", UNSET)

        team = d.pop("team", UNSET)

        user = d.pop("user", UNSET)

        _created_at = d.pop("createdAt", UNSET)
        created_at: datetime.datetime | Unset
        if isinstance(_created_at,  Unset):
            created_at = UNSET
        else:
            created_at = datetime.datetime.fromisoformat(_created_at)




        template_name = d.pop("templateName", UNSET)

        _mode = d.pop("mode", UNSET)
        mode: SandboxEnvSummaryMode | Unset
        if isinstance(_mode,  Unset):
            mode = UNSET
        else:
            mode = SandboxEnvSummaryMode(_mode)




        member_count = d.pop("memberCount", UNSET)

        desired_replicas = d.pop("desiredReplicas", UNSET)

        running_replicas = d.pop("runningReplicas", UNSET)

        idle_replicas = d.pop("idleReplicas", UNSET)

        scaling_group_count = d.pop("scalingGroupCount", UNSET)

        autoscaling_enabled_group_count = d.pop("autoscalingEnabledGroupCount", UNSET)

        ready = d.pop("ready", UNSET)

        _pool_sizing = d.pop("poolSizing", UNSET)
        pool_sizing: PoolSizing | Unset
        if isinstance(_pool_sizing,  Unset):
            pool_sizing = UNSET
        else:
            pool_sizing = PoolSizing(_pool_sizing)




        sandbox_env_summary = cls(
            name=name,
            namespace=namespace,
            team=team,
            user=user,
            created_at=created_at,
            template_name=template_name,
            mode=mode,
            member_count=member_count,
            desired_replicas=desired_replicas,
            running_replicas=running_replicas,
            idle_replicas=idle_replicas,
            scaling_group_count=scaling_group_count,
            autoscaling_enabled_group_count=autoscaling_enabled_group_count,
            ready=ready,
            pool_sizing=pool_sizing,
        )


        sandbox_env_summary.additional_properties = d
        return sandbox_env_summary

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
