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
from dateutil.parser import isoparse
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
            saturated_until (datetime.datetime | Unset): Set by the autoscaler when a PreUpdatePool probe returned
                InsufficientResources or InvalidSpec. Until this time, the autoscaler skips probing this member and the router
                deprioritises it.
            last_scale_up_attempt_result (str | Unset): Outcome of the most recent probe-and-patch attempt: Success |
                InsufficientResources | InternalError | InvalidSpec.
            scale_up_error_message (str | Unset): Short error description from the most recent non-Success probe. Empty when
                LastScaleUpAttemptResult is Success.
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
    last_scale_up_attempt_result: str | Unset = UNSET
    scale_up_error_message: str | Unset = UNSET
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

        last_scale_up_attempt_result = self.last_scale_up_attempt_result

        scale_up_error_message = self.scale_up_error_message


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
        if last_scale_up_attempt_result is not UNSET:
            field_dict["lastScaleUpAttemptResult"] = last_scale_up_attempt_result
        if scale_up_error_message is not UNSET:
            field_dict["scaleUpErrorMessage"] = scale_up_error_message

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
            saturated_until = isoparse(_saturated_until)




        last_scale_up_attempt_result = d.pop("lastScaleUpAttemptResult", UNSET)

        scale_up_error_message = d.pop("scaleUpErrorMessage", UNSET)

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
            last_scale_up_attempt_result=last_scale_up_attempt_result,
            scale_up_error_message=scale_up_error_message,
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
