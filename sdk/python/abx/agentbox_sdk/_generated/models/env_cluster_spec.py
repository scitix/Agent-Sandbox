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
  from ..models.env_cluster_member import EnvClusterMember





T = TypeVar("T", bound="EnvClusterSpec")



@_attrs_define
class EnvClusterSpec:
    """ 
        Attributes:
            cluster_id (str): Cluster identifier that owns this segment. Each Worker only mutates the segment matching its
                own clusterID.
            members (list[EnvClusterMember] | Unset): Member Pools in this cluster. MVP allows multiple members per cluster
                but a single ScalingGroup.
     """

    cluster_id: str
    members: list[EnvClusterMember] | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.env_cluster_member import EnvClusterMember
        cluster_id = self.cluster_id

        members: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.members, Unset):
            members = []
            for members_item_data in self.members:
                members_item = members_item_data.to_dict()
                members.append(members_item)




        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "clusterID": cluster_id,
        })
        if members is not UNSET:
            field_dict["members"] = members

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.env_cluster_member import EnvClusterMember
        d = dict(src_dict)
        cluster_id = d.pop("clusterID")

        _members = d.pop("members", UNSET)
        members: list[EnvClusterMember] | Unset = UNSET
        if _members is not UNSET:
            members = []
            for members_item_data in _members:
                members_item = EnvClusterMember.from_dict(members_item_data)



                members.append(members_item)


        env_cluster_spec = cls(
            cluster_id=cluster_id,
            members=members,
        )


        env_cluster_spec.additional_properties = d
        return env_cluster_spec

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
