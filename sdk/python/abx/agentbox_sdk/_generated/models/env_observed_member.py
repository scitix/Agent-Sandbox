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
            update_revision (str | Unset): Mirror of the member Pool's status.updateRevision — the target revision the Pool
                is rolling towards.
            updated_replicas (int | Unset): Mirror of the member Pool's status.updatedReplicas. A rollout is in progress
                while this is below the member's replica count.
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
    update_revision: str | Unset = UNSET
    updated_replicas: int | Unset = UNSET
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

        update_revision = self.update_revision

        updated_replicas = self.updated_replicas


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
        if update_revision is not UNSET:
            field_dict["updateRevision"] = update_revision
        if updated_replicas is not UNSET:
            field_dict["updatedReplicas"] = updated_replicas

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




        update_revision = d.pop("updateRevision", UNSET)

        updated_replicas = d.pop("updatedReplicas", UNSET)

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
            update_revision=update_revision,
            updated_replicas=updated_replicas,
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
