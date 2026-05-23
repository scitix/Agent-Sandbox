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

if TYPE_CHECKING:
  from ..models.env_observed_member import EnvObservedMember





T = TypeVar("T", bound="EnvClusterStatus")



@_attrs_define
class EnvClusterStatus:
    """ 
        Attributes:
            cluster_id (str):
            is_local (bool | Unset): True on the Worker that owns this segment. Other segments arrive via Hub Sync (future).
            observed_members (list[EnvObservedMember] | Unset):
            last_scale_up_time (datetime.datetime | Unset):
            last_scale_down_time (datetime.datetime | Unset):
            idle_zero_since (datetime.datetime | Unset):
            last_snapshot_time (datetime.datetime | Unset):
     """

    cluster_id: str
    is_local: bool | Unset = UNSET
    observed_members: list[EnvObservedMember] | Unset = UNSET
    last_scale_up_time: datetime.datetime | Unset = UNSET
    last_scale_down_time: datetime.datetime | Unset = UNSET
    idle_zero_since: datetime.datetime | Unset = UNSET
    last_snapshot_time: datetime.datetime | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.env_observed_member import EnvObservedMember
        cluster_id = self.cluster_id

        is_local = self.is_local

        observed_members: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.observed_members, Unset):
            observed_members = []
            for observed_members_item_data in self.observed_members:
                observed_members_item = observed_members_item_data.to_dict()
                observed_members.append(observed_members_item)



        last_scale_up_time: str | Unset = UNSET
        if not isinstance(self.last_scale_up_time, Unset):
            last_scale_up_time = self.last_scale_up_time.isoformat()

        last_scale_down_time: str | Unset = UNSET
        if not isinstance(self.last_scale_down_time, Unset):
            last_scale_down_time = self.last_scale_down_time.isoformat()

        idle_zero_since: str | Unset = UNSET
        if not isinstance(self.idle_zero_since, Unset):
            idle_zero_since = self.idle_zero_since.isoformat()

        last_snapshot_time: str | Unset = UNSET
        if not isinstance(self.last_snapshot_time, Unset):
            last_snapshot_time = self.last_snapshot_time.isoformat()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "clusterID": cluster_id,
        })
        if is_local is not UNSET:
            field_dict["isLocal"] = is_local
        if observed_members is not UNSET:
            field_dict["observedMembers"] = observed_members
        if last_scale_up_time is not UNSET:
            field_dict["lastScaleUpTime"] = last_scale_up_time
        if last_scale_down_time is not UNSET:
            field_dict["lastScaleDownTime"] = last_scale_down_time
        if idle_zero_since is not UNSET:
            field_dict["idleZeroSince"] = idle_zero_since
        if last_snapshot_time is not UNSET:
            field_dict["lastSnapshotTime"] = last_snapshot_time

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.env_observed_member import EnvObservedMember
        d = dict(src_dict)
        cluster_id = d.pop("clusterID")

        is_local = d.pop("isLocal", UNSET)

        _observed_members = d.pop("observedMembers", UNSET)
        observed_members: list[EnvObservedMember] | Unset = UNSET
        if _observed_members is not UNSET:
            observed_members = []
            for observed_members_item_data in _observed_members:
                observed_members_item = EnvObservedMember.from_dict(observed_members_item_data)



                observed_members.append(observed_members_item)


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




        _last_snapshot_time = d.pop("lastSnapshotTime", UNSET)
        last_snapshot_time: datetime.datetime | Unset
        if isinstance(_last_snapshot_time,  Unset):
            last_snapshot_time = UNSET
        else:
            last_snapshot_time = isoparse(_last_snapshot_time)




        env_cluster_status = cls(
            cluster_id=cluster_id,
            is_local=is_local,
            observed_members=observed_members,
            last_scale_up_time=last_scale_up_time,
            last_scale_down_time=last_scale_down_time,
            idle_zero_since=idle_zero_since,
            last_snapshot_time=last_snapshot_time,
        )


        env_cluster_status.additional_properties = d
        return env_cluster_status

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
