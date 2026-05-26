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
from dateutil.parser import isoparse
from typing import cast
import datetime






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
            last_scale_up_time (datetime.datetime | Unset): Most recent scale-up event for this group. Drives the per-group
                cooldown window.
            last_scale_down_time (datetime.datetime | Unset): Most recent scale-down event for this group. Drives the per-
                group stabilization window.
            idle_zero_since (datetime.datetime | Unset): When this group's aggregate idle count first dropped to zero in the
                current continuous-zero window; clears when group idle > 0. Drives the proactive scale-up trigger.
     """

    name: str
    total_idle: int | Unset = UNSET
    total_running: int | Unset = UNSET
    total_desired: int | Unset = UNSET
    total_pending: int | Unset = UNSET
    last_scale_up_time: datetime.datetime | Unset = UNSET
    last_scale_down_time: datetime.datetime | Unset = UNSET
    idle_zero_since: datetime.datetime | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        name = self.name

        total_idle = self.total_idle

        total_running = self.total_running

        total_desired = self.total_desired

        total_pending = self.total_pending

        last_scale_up_time: str | Unset = UNSET
        if not isinstance(self.last_scale_up_time, Unset):
            last_scale_up_time = self.last_scale_up_time.isoformat()

        last_scale_down_time: str | Unset = UNSET
        if not isinstance(self.last_scale_down_time, Unset):
            last_scale_down_time = self.last_scale_down_time.isoformat()

        idle_zero_since: str | Unset = UNSET
        if not isinstance(self.idle_zero_since, Unset):
            idle_zero_since = self.idle_zero_since.isoformat()


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
        if last_scale_up_time is not UNSET:
            field_dict["lastScaleUpTime"] = last_scale_up_time
        if last_scale_down_time is not UNSET:
            field_dict["lastScaleDownTime"] = last_scale_down_time
        if idle_zero_since is not UNSET:
            field_dict["idleZeroSince"] = idle_zero_since

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        name = d.pop("name")

        total_idle = d.pop("totalIdle", UNSET)

        total_running = d.pop("totalRunning", UNSET)

        total_desired = d.pop("totalDesired", UNSET)

        total_pending = d.pop("totalPending", UNSET)

        _last_scale_up_time = d.pop("lastScaleUpTime", UNSET)
        last_scale_up_time: datetime.datetime | Unset
        if isinstance(_last_scale_up_time,  Unset):
            last_scale_up_time = UNSET
        else:
            last_scale_up_time = isoparse(_last_scale_up_time)




        _last_scale_down_time = d.pop("lastScaleDownTime", UNSET)
        last_scale_down_time: datetime.datetime | Unset
        if isinstance(_last_scale_down_time,  Unset):
            last_scale_down_time = UNSET
        else:
            last_scale_down_time = isoparse(_last_scale_down_time)




        _idle_zero_since = d.pop("idleZeroSince", UNSET)
        idle_zero_since: datetime.datetime | Unset
        if isinstance(_idle_zero_since,  Unset):
            idle_zero_since = UNSET
        else:
            idle_zero_since = isoparse(_idle_zero_since)




        env_scaling_group_status = cls(
            name=name,
            total_idle=total_idle,
            total_running=total_running,
            total_desired=total_desired,
            total_pending=total_pending,
            last_scale_up_time=last_scale_up_time,
            last_scale_down_time=last_scale_down_time,
            idle_zero_since=idle_zero_since,
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
