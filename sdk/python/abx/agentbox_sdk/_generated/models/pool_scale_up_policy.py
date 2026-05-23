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

from ..models.pool_scale_up_policy_mode import PoolScaleUpPolicyMode
from ..types import UNSET, Unset






T = TypeVar("T", bound="PoolScaleUpPolicy")



@_attrs_define
class PoolScaleUpPolicy:
    """ Scale-up behaviour for a scaling group (mode + cooldown + idle threshold + saturation cooldown).

        Attributes:
            mode (PoolScaleUpPolicyMode | Unset): Conservative=+1 per decision; Default=+max(1,ceil(N/2)); Aggressive=double
                up to maxReplicas.
            cooldown_seconds (int | Unset): Minimum seconds between two consecutive scale-up events (group-level).
            idle_threshold_seconds (int | Unset): Aggregate idleReplicas=0 must persist for this long before the proactive
                trigger fires. Zero disables proactive scale-up.
            saturation_cooldown_seconds (int | Unset): How long a member stays marked saturated after a probe returned
                InsufficientResources / InvalidSpec. Default 60s.
     """

    mode: PoolScaleUpPolicyMode | Unset = UNSET
    cooldown_seconds: int | Unset = UNSET
    idle_threshold_seconds: int | Unset = UNSET
    saturation_cooldown_seconds: int | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        mode: str | Unset = UNSET
        if not isinstance(self.mode, Unset):
            mode = self.mode.value


        cooldown_seconds = self.cooldown_seconds

        idle_threshold_seconds = self.idle_threshold_seconds

        saturation_cooldown_seconds = self.saturation_cooldown_seconds


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if mode is not UNSET:
            field_dict["mode"] = mode
        if cooldown_seconds is not UNSET:
            field_dict["cooldownSeconds"] = cooldown_seconds
        if idle_threshold_seconds is not UNSET:
            field_dict["idleThresholdSeconds"] = idle_threshold_seconds
        if saturation_cooldown_seconds is not UNSET:
            field_dict["saturationCooldownSeconds"] = saturation_cooldown_seconds

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        _mode = d.pop("mode", UNSET)
        mode: PoolScaleUpPolicyMode | Unset
        if isinstance(_mode,  Unset):
            mode = UNSET
        else:
            mode = PoolScaleUpPolicyMode(_mode)




        cooldown_seconds = d.pop("cooldownSeconds", UNSET)

        idle_threshold_seconds = d.pop("idleThresholdSeconds", UNSET)

        saturation_cooldown_seconds = d.pop("saturationCooldownSeconds", UNSET)

        pool_scale_up_policy = cls(
            mode=mode,
            cooldown_seconds=cooldown_seconds,
            idle_threshold_seconds=idle_threshold_seconds,
            saturation_cooldown_seconds=saturation_cooldown_seconds,
        )


        pool_scale_up_policy.additional_properties = d
        return pool_scale_up_policy

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
