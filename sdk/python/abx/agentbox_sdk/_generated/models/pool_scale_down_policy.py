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






T = TypeVar("T", bound="PoolScaleDownPolicy")



@_attrs_define
class PoolScaleDownPolicy:
    """ Controls scale-down behavior for a SandboxPool.

        Attributes:
            idle_timeout_seconds (int | Unset): Minimum seconds a pod must remain Idle before it becomes a scale-down
                candidate. Default: 300.
            stabilization_seconds (int | Unset): Minimum seconds between two consecutive scale-down events. Default: 60.
            protection_window_seconds (int | Unset): Seconds after a pod is marked for scale-down during which a new Claim
                can still cancel the intent. Default: 10.
     """

    idle_timeout_seconds: int | Unset = 300
    stabilization_seconds: int | Unset = 60
    protection_window_seconds: int | Unset = 10
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        idle_timeout_seconds = self.idle_timeout_seconds

        stabilization_seconds = self.stabilization_seconds

        protection_window_seconds = self.protection_window_seconds


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if idle_timeout_seconds is not UNSET:
            field_dict["idleTimeoutSeconds"] = idle_timeout_seconds
        if stabilization_seconds is not UNSET:
            field_dict["stabilizationSeconds"] = stabilization_seconds
        if protection_window_seconds is not UNSET:
            field_dict["protectionWindowSeconds"] = protection_window_seconds

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        idle_timeout_seconds = d.pop("idleTimeoutSeconds", UNSET)

        stabilization_seconds = d.pop("stabilizationSeconds", UNSET)

        protection_window_seconds = d.pop("protectionWindowSeconds", UNSET)

        pool_scale_down_policy = cls(
            idle_timeout_seconds=idle_timeout_seconds,
            stabilization_seconds=stabilization_seconds,
            protection_window_seconds=protection_window_seconds,
        )


        pool_scale_down_policy.additional_properties = d
        return pool_scale_down_policy

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
