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






T = TypeVar("T", bound="EnvScalingGroupStatus")



@_attrs_define
class EnvScalingGroupStatus:
    """ 
        Attributes:
            name (str):
            total_idle (int | Unset):
            total_running (int | Unset):
            total_desired (int | Unset):
            total_pending (int | Unset): Aggregate ObservedMember.pendingRequests across all members of this group.
     """

    name: str
    total_idle: int | Unset = UNSET
    total_running: int | Unset = UNSET
    total_desired: int | Unset = UNSET
    total_pending: int | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        name = self.name

        total_idle = self.total_idle

        total_running = self.total_running

        total_desired = self.total_desired

        total_pending = self.total_pending


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "name": name,
        })
        if total_idle is not UNSET:
            field_dict["totalIdle"] = total_idle
        if total_running is not UNSET:
            field_dict["totalRunning"] = total_running
        if total_desired is not UNSET:
            field_dict["totalDesired"] = total_desired
        if total_pending is not UNSET:
            field_dict["totalPending"] = total_pending

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        name = d.pop("name")

        total_idle = d.pop("totalIdle", UNSET)

        total_running = d.pop("totalRunning", UNSET)

        total_desired = d.pop("totalDesired", UNSET)

        total_pending = d.pop("totalPending", UNSET)

        env_scaling_group_status = cls(
            name=name,
            total_idle=total_idle,
            total_running=total_running,
            total_desired=total_desired,
            total_pending=total_pending,
        )


        env_scaling_group_status.additional_properties = d
        return env_scaling_group_status

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
