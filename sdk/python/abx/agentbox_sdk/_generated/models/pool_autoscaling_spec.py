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





T = TypeVar("T", bound="PoolAutoscalingSpec")



@_attrs_define
class PoolAutoscalingSpec:
    """ Autoscaling configuration for a SandboxPool. When nil or enabled=false, spec.replicas is the only source of truth.

        Attributes:
            enabled (bool | Unset): When false (default), the pool is managed manually via spec.replicas.
                MinReplicas/MaxReplicas are ignored. Default: False.
            scale_up_policy (PoolScaleUpPolicy | Unset): Controls scale-up behavior for a SandboxPool.
            scale_down_policy (PoolScaleDownPolicy | Unset): Controls scale-down behavior for a SandboxPool.
     """

    enabled: bool | Unset = False
    scale_up_policy: PoolScaleUpPolicy | Unset = UNSET
    scale_down_policy: PoolScaleDownPolicy | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.pool_scale_down_policy import PoolScaleDownPolicy
        from ..models.pool_scale_up_policy import PoolScaleUpPolicy
        enabled = self.enabled

        scale_up_policy: dict[str, Any] | Unset = UNSET
        if not isinstance(self.scale_up_policy, Unset):
            scale_up_policy = self.scale_up_policy.to_dict()

        scale_down_policy: dict[str, Any] | Unset = UNSET
        if not isinstance(self.scale_down_policy, Unset):
            scale_down_policy = self.scale_down_policy.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if enabled is not UNSET:
            field_dict["enabled"] = enabled
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
        enabled = d.pop("enabled", UNSET)

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




        pool_autoscaling_spec = cls(
            enabled=enabled,
            scale_up_policy=scale_up_policy,
            scale_down_policy=scale_down_policy,
        )


        pool_autoscaling_spec.additional_properties = d
        return pool_autoscaling_spec

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
