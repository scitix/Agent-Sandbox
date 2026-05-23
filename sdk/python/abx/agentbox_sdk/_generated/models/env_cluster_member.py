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






T = TypeVar("T", bound="EnvClusterMember")



@_attrs_define
class EnvClusterMember:
    """ 
        Attributes:
            name (str): SandboxPool's metadata.name within the Env's namespace. Acts as the member identity within the Env.
            instance_type (str | Unset): Optional InstanceType catalog entry referenced by this member.
            multiplier (int | Unset): Multiplier applied to the InstanceType base resources for this member.
            scaling_group (str | Unset): ScalingGroup name (typically derived from the effective resources, e.g. '1c4Gi').
                Members in the same group share autoscaling policy.
            max_replicas (int | Unset): Upper bound on this member's spec.replicas. Enforced by the Env autoscaler when
                distributing scale-up delta.
            priority (int | Unset): Routing priority — lower preferred when EnvScheduler picks a member to dispatch a
                request.
            scale_up_priority (int | Unset): Scale-up order within the scaling group — lower scaled first. Same-value
                tiebreak by name.
            scale_down_priority (int | Unset): Scale-down order within the scaling group — lower shrunk first.
     """

    name: str
    instance_type: str | Unset = UNSET
    multiplier: int | Unset = UNSET
    scaling_group: str | Unset = UNSET
    max_replicas: int | Unset = UNSET
    priority: int | Unset = UNSET
    scale_up_priority: int | Unset = UNSET
    scale_down_priority: int | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        name = self.name

        instance_type = self.instance_type

        multiplier = self.multiplier

        scaling_group = self.scaling_group

        max_replicas = self.max_replicas

        priority = self.priority

        scale_up_priority = self.scale_up_priority

        scale_down_priority = self.scale_down_priority


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "name": name,
        })
        if instance_type is not UNSET:
            field_dict["instanceType"] = instance_type
        if multiplier is not UNSET:
            field_dict["multiplier"] = multiplier
        if scaling_group is not UNSET:
            field_dict["scalingGroup"] = scaling_group
        if max_replicas is not UNSET:
            field_dict["maxReplicas"] = max_replicas
        if priority is not UNSET:
            field_dict["priority"] = priority
        if scale_up_priority is not UNSET:
            field_dict["scaleUpPriority"] = scale_up_priority
        if scale_down_priority is not UNSET:
            field_dict["scaleDownPriority"] = scale_down_priority

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        name = d.pop("name")

        instance_type = d.pop("instanceType", UNSET)

        multiplier = d.pop("multiplier", UNSET)

        scaling_group = d.pop("scalingGroup", UNSET)

        max_replicas = d.pop("maxReplicas", UNSET)

        priority = d.pop("priority", UNSET)

        scale_up_priority = d.pop("scaleUpPriority", UNSET)

        scale_down_priority = d.pop("scaleDownPriority", UNSET)

        env_cluster_member = cls(
            name=name,
            instance_type=instance_type,
            multiplier=multiplier,
            scaling_group=scaling_group,
            max_replicas=max_replicas,
            priority=priority,
            scale_up_priority=scale_up_priority,
            scale_down_priority=scale_down_priority,
        )


        env_cluster_member.additional_properties = d
        return env_cluster_member

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
