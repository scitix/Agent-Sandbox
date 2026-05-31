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
