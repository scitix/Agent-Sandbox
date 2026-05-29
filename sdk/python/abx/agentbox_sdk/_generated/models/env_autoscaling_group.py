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
  from ..models.pool_scale_down_policy import PoolScaleDownPolicy
  from ..models.pool_scale_up_policy import PoolScaleUpPolicy





T = TypeVar("T", bound="EnvAutoscalingGroup")



@_attrs_define
class EnvAutoscalingGroup:
    """ 
        Attributes:
            name (str): ScalingGroup identifier this policy applies to. Matches the EnvClusterMember.scalingGroup of at
                least one member declared on the env — the group is created automatically when a member with this ScalingGroup
                is added, and garbage-collected by the Env reconciler once no member references it.
            enabled (bool | Unset): Per-group master switch. When false, this group's members keep manual Pool replicas; the
                autoscaler skips it. Each group is independent — other groups continue to run if Enabled=true.
            min_replicas (int | Unset): Lower bound on the group's aggregate desired replicas.
            max_replicas (int | Unset): Upper bound on the group's aggregate desired replicas.
            scale_up_policy (PoolScaleUpPolicy | Unset): Scale-up behaviour for a scaling group (mode + cooldown + idle
                threshold + saturation cooldown).
            scale_down_policy (PoolScaleDownPolicy | Unset): Scale-down behaviour for a scaling group.
     """

    name: str
    enabled: bool | Unset = UNSET
    min_replicas: int | Unset = UNSET
    max_replicas: int | Unset = UNSET
    scale_up_policy: PoolScaleUpPolicy | Unset = UNSET
    scale_down_policy: PoolScaleDownPolicy | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.pool_scale_down_policy import PoolScaleDownPolicy
        from ..models.pool_scale_up_policy import PoolScaleUpPolicy
        name = self.name

        enabled = self.enabled

        min_replicas = self.min_replicas

        max_replicas = self.max_replicas

        scale_up_policy: dict[str, Any] | Unset = UNSET
        if not isinstance(self.scale_up_policy, Unset):
            scale_up_policy = self.scale_up_policy.to_dict()

        scale_down_policy: dict[str, Any] | Unset = UNSET
        if not isinstance(self.scale_down_policy, Unset):
            scale_down_policy = self.scale_down_policy.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "name": name,
        })
        if enabled is not UNSET:
            field_dict["enabled"] = enabled
        if min_replicas is not UNSET:
            field_dict["minReplicas"] = min_replicas
        if max_replicas is not UNSET:
            field_dict["maxReplicas"] = max_replicas
        if scale_up_policy is not UNSET:
            field_dict["scaleUpPolicy"] = scale_up_policy
        if scale_down_policy is not UNSET:
            field_dict["scaleDownPolicy"] = scale_down_policy

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.pool_scale_down_policy import PoolScaleDownPolicy
        from ..models.pool_scale_up_policy import PoolScaleUpPolicy
        d = dict(src_dict)
        name = d.pop("name")

        enabled = d.pop("enabled", UNSET)

        min_replicas = d.pop("minReplicas", UNSET)

        max_replicas = d.pop("maxReplicas", UNSET)

        _scale_up_policy = d.pop("scaleUpPolicy", UNSET)
        scale_up_policy: PoolScaleUpPolicy | Unset
        if isinstance(_scale_up_policy,  Unset):
            scale_up_policy = UNSET
        else:
            scale_up_policy = PoolScaleUpPolicy.from_dict(_scale_up_policy)




        _scale_down_policy = d.pop("scaleDownPolicy", UNSET)
        scale_down_policy: PoolScaleDownPolicy | Unset
        if isinstance(_scale_down_policy,  Unset):
            scale_down_policy = UNSET
        else:
            scale_down_policy = PoolScaleDownPolicy.from_dict(_scale_down_policy)




        env_autoscaling_group = cls(
            name=name,
            enabled=enabled,
            min_replicas=min_replicas,
            max_replicas=max_replicas,
            scale_up_policy=scale_up_policy,
            scale_down_policy=scale_down_policy,
        )


        env_autoscaling_group.additional_properties = d
        return env_autoscaling_group

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
