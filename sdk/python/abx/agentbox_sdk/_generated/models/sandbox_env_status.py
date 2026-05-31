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
  from ..models.env_cluster_status import EnvClusterStatus
  from ..models.env_condition import EnvCondition
  from ..models.env_scaling_group_status import EnvScalingGroupStatus





T = TypeVar("T", bound="SandboxEnvStatus")



@_attrs_define
class SandboxEnvStatus:
    """ 
        Attributes:
            conditions (list[EnvCondition] | Unset):
            clusters (list[EnvClusterStatus] | Unset):
            scaling_groups (list[EnvScalingGroupStatus] | Unset):
            member_count (int | Unset): Total member Pools across all cluster segments (today: the local segment only).
                Exists because printer columns cannot evaluate the nested clusters[].members[] array.
            desired_replicas (int | Unset): Env-wide sum of every member Pool's desired replicas.
            running_replicas (int | Unset): Env-wide sum of every member Pool's running replicas.
            idle_replicas (int | Unset): Env-wide sum of every member Pool's idle replicas.
     """

    conditions: list[EnvCondition] | Unset = UNSET
    clusters: list[EnvClusterStatus] | Unset = UNSET
    scaling_groups: list[EnvScalingGroupStatus] | Unset = UNSET
    member_count: int | Unset = UNSET
    desired_replicas: int | Unset = UNSET
    running_replicas: int | Unset = UNSET
    idle_replicas: int | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.env_cluster_status import EnvClusterStatus
        from ..models.env_condition import EnvCondition
        from ..models.env_scaling_group_status import EnvScalingGroupStatus
        conditions: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.conditions, Unset):
            conditions = []
            for conditions_item_data in self.conditions:
                conditions_item = conditions_item_data.to_dict()
                conditions.append(conditions_item)



        clusters: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.clusters, Unset):
            clusters = []
            for clusters_item_data in self.clusters:
                clusters_item = clusters_item_data.to_dict()
                clusters.append(clusters_item)



        scaling_groups: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.scaling_groups, Unset):
            scaling_groups = []
            for scaling_groups_item_data in self.scaling_groups:
                scaling_groups_item = scaling_groups_item_data.to_dict()
                scaling_groups.append(scaling_groups_item)



        member_count = self.member_count

        desired_replicas = self.desired_replicas

        running_replicas = self.running_replicas

        idle_replicas = self.idle_replicas


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if conditions is not UNSET:
            field_dict["conditions"] = conditions
        if clusters is not UNSET:
            field_dict["clusters"] = clusters
        if scaling_groups is not UNSET:
            field_dict["scalingGroups"] = scaling_groups
        if member_count is not UNSET:
            field_dict["memberCount"] = member_count
        if desired_replicas is not UNSET:
            field_dict["desiredReplicas"] = desired_replicas
        if running_replicas is not UNSET:
            field_dict["runningReplicas"] = running_replicas
        if idle_replicas is not UNSET:
            field_dict["idleReplicas"] = idle_replicas

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.env_cluster_status import EnvClusterStatus
        from ..models.env_condition import EnvCondition
        from ..models.env_scaling_group_status import EnvScalingGroupStatus
        d = dict(src_dict)
        _conditions = d.pop("conditions", UNSET)
        conditions: list[EnvCondition] | Unset = UNSET
        if _conditions is not UNSET:
            conditions = []
            for conditions_item_data in _conditions:
                conditions_item = EnvCondition.from_dict(conditions_item_data)



                conditions.append(conditions_item)


        _clusters = d.pop("clusters", UNSET)
        clusters: list[EnvClusterStatus] | Unset = UNSET
        if _clusters is not UNSET:
            clusters = []
            for clusters_item_data in _clusters:
                clusters_item = EnvClusterStatus.from_dict(clusters_item_data)



                clusters.append(clusters_item)


        _scaling_groups = d.pop("scalingGroups", UNSET)
        scaling_groups: list[EnvScalingGroupStatus] | Unset = UNSET
        if _scaling_groups is not UNSET:
            scaling_groups = []
            for scaling_groups_item_data in _scaling_groups:
                scaling_groups_item = EnvScalingGroupStatus.from_dict(scaling_groups_item_data)



                scaling_groups.append(scaling_groups_item)


        member_count = d.pop("memberCount", UNSET)

        desired_replicas = d.pop("desiredReplicas", UNSET)

        running_replicas = d.pop("runningReplicas", UNSET)

        idle_replicas = d.pop("idleReplicas", UNSET)

        sandbox_env_status = cls(
            conditions=conditions,
            clusters=clusters,
            scaling_groups=scaling_groups,
            member_count=member_count,
            desired_replicas=desired_replicas,
            running_replicas=running_replicas,
            idle_replicas=idle_replicas,
        )


        sandbox_env_status.additional_properties = d
        return sandbox_env_status

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
