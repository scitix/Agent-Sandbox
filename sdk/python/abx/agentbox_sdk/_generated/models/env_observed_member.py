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

from ..models.env_observed_member_state import EnvObservedMemberState
from ..types import UNSET, Unset
from typing import cast
import datetime






T = TypeVar("T", bound="EnvObservedMember")



@_attrs_define
class EnvObservedMember:
    """ 
        Attributes:
            name (str):
            instance_type (str | Unset):
            multiplier (int | Unset):
            state (EnvObservedMemberState | Unset):
            idle_count (int | Unset):
            running_count (int | Unset):
            desired_replicas (int | Unset):
            current_replicas (int | Unset):
            pending_requests (int | Unset): Mirror of SandboxPool.status.pendingRequests for this member — throttled at the
                source so visible value lags actual queue depth by up to ~3s.
            saturated_until (datetime.datetime | Unset): Read-only mirror of SandboxPool.status.autoscaling.saturatedUntil.
                Until this time, the router deprioritises the member because the per-Pool autoscaler reported the cluster cannot
                fit additional replicas.
            scaling_group (str | Unset): Autoscaling group this member belongs to on its owning cluster, echoed from spec.
                Empty when the member is in no group. Present on foreign (cross-cluster) members too, so a viewer without the
                foreign cluster's spec can still attribute the member to a group and link to that cluster's group detail.
            autoscaling_enabled (bool | Unset): Whether this member's scalingGroup has the autoscaler on in its owning
                cluster. Each cluster scales independently, so a same-named group may be enabled in one cluster and off in
                another; this is the per-pool, per-cluster truth. Disambiguates scaleUpHeadroom == 0 (at ceiling) from
                autoscaling being off.
            scale_up_headroom (int | Unset): Estimated replicas this member can still add on its owning cluster before
                hitting the smaller of its own MaxReplicas and its group's aggregate MaxReplicas. Meaningful only when
                autoscalingEnabled: omitted = off, or on with no finite ceiling (unbounded); 0 = at ceiling; >0 = room left.
                Advisory estimate — the group ceiling is shared across members and quota is not folded in; for foreign members
                it also lags by the federation TTL.
            update_revision (str | Unset): Mirror of the member Pool's status.updateRevision — the target revision the Pool
                is rolling towards.
            updated_replicas (int | Unset): Mirror of the member Pool's status.updatedReplicas. A rollout is in progress
                while this is below the member's replica count.
            template_version (str | Unset): SandboxTemplate spec.version the member Pool was last rendered from, read off
                its provenance annotation. An observation of what the member actually carries, not a constraint — members follow
                the Template's current body, and spec.templateRef.version pins nothing. Empty for foreign (cross-cluster)
                members.
     """

    name: str
    instance_type: str | Unset = UNSET
    multiplier: int | Unset = UNSET
    state: EnvObservedMemberState | Unset = UNSET
    idle_count: int | Unset = UNSET
    running_count: int | Unset = UNSET
    desired_replicas: int | Unset = UNSET
    current_replicas: int | Unset = UNSET
    pending_requests: int | Unset = UNSET
    saturated_until: datetime.datetime | Unset = UNSET
    scaling_group: str | Unset = UNSET
    autoscaling_enabled: bool | Unset = UNSET
    scale_up_headroom: int | Unset = UNSET
    update_revision: str | Unset = UNSET
    updated_replicas: int | Unset = UNSET
    template_version: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        name = self.name

        instance_type = self.instance_type

        multiplier = self.multiplier

        state: str | Unset = UNSET
        if not isinstance(self.state, Unset):
            state = self.state.value


        idle_count = self.idle_count

        running_count = self.running_count

        desired_replicas = self.desired_replicas

        current_replicas = self.current_replicas

        pending_requests = self.pending_requests

        saturated_until: str | Unset = UNSET
        if not isinstance(self.saturated_until, Unset):
            saturated_until = self.saturated_until.isoformat()

        scaling_group = self.scaling_group

        autoscaling_enabled = self.autoscaling_enabled

        scale_up_headroom = self.scale_up_headroom

        update_revision = self.update_revision

        updated_replicas = self.updated_replicas

        template_version = self.template_version


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "name": name,
        })
        if instance_type is not UNSET:
            field_dict["instanceType"] = instance_type
        if multiplier is not UNSET:
            field_dict["multiplier"] = multiplier
        if state is not UNSET:
            field_dict["state"] = state
        if idle_count is not UNSET:
            field_dict["idleCount"] = idle_count
        if running_count is not UNSET:
            field_dict["runningCount"] = running_count
        if desired_replicas is not UNSET:
            field_dict["desiredReplicas"] = desired_replicas
        if current_replicas is not UNSET:
            field_dict["currentReplicas"] = current_replicas
        if pending_requests is not UNSET:
            field_dict["pendingRequests"] = pending_requests
        if saturated_until is not UNSET:
            field_dict["saturatedUntil"] = saturated_until
        if scaling_group is not UNSET:
            field_dict["scalingGroup"] = scaling_group
        if autoscaling_enabled is not UNSET:
            field_dict["autoscalingEnabled"] = autoscaling_enabled
        if scale_up_headroom is not UNSET:
            field_dict["scaleUpHeadroom"] = scale_up_headroom
        if update_revision is not UNSET:
            field_dict["updateRevision"] = update_revision
        if updated_replicas is not UNSET:
            field_dict["updatedReplicas"] = updated_replicas
        if template_version is not UNSET:
            field_dict["templateVersion"] = template_version

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        name = d.pop("name")

        instance_type = d.pop("instanceType", UNSET)

        multiplier = d.pop("multiplier", UNSET)

        _state = d.pop("state", UNSET)
        state: EnvObservedMemberState | Unset
        if isinstance(_state,  Unset):
            state = UNSET
        else:
            state = EnvObservedMemberState(_state)




        idle_count = d.pop("idleCount", UNSET)

        running_count = d.pop("runningCount", UNSET)

        desired_replicas = d.pop("desiredReplicas", UNSET)

        current_replicas = d.pop("currentReplicas", UNSET)

        pending_requests = d.pop("pendingRequests", UNSET)

        _saturated_until = d.pop("saturatedUntil", UNSET)
        saturated_until: datetime.datetime | Unset
        if isinstance(_saturated_until,  Unset):
            saturated_until = UNSET
        else:
            saturated_until = datetime.datetime.fromisoformat(_saturated_until)




        scaling_group = d.pop("scalingGroup", UNSET)

        autoscaling_enabled = d.pop("autoscalingEnabled", UNSET)

        scale_up_headroom = d.pop("scaleUpHeadroom", UNSET)

        update_revision = d.pop("updateRevision", UNSET)

        updated_replicas = d.pop("updatedReplicas", UNSET)

        template_version = d.pop("templateVersion", UNSET)

        env_observed_member = cls(
            name=name,
            instance_type=instance_type,
            multiplier=multiplier,
            state=state,
            idle_count=idle_count,
            running_count=running_count,
            desired_replicas=desired_replicas,
            current_replicas=current_replicas,
            pending_requests=pending_requests,
            saturated_until=saturated_until,
            scaling_group=scaling_group,
            autoscaling_enabled=autoscaling_enabled,
            scale_up_headroom=scale_up_headroom,
            update_revision=update_revision,
            updated_replicas=updated_replicas,
            template_version=template_version,
        )


        env_observed_member.additional_properties = d
        return env_observed_member

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
