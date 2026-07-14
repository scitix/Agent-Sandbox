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






T = TypeVar("T", bound="EnvUpdateStrategy")



@_attrs_define
class EnvUpdateStrategy:
    """ Automatic rollout policy for member Pools when their rendered idle-Pod identity (Template edit, image /
    networkPolicy override) changes. Rollout mode is always Recreate: stale idle Pods are rebuilt; claimed
    (Running/Starting) Pods are never disrupted and roll after returning to Idle.

        Attributes:
            auto_update (bool | Unset): Whether the member auto-rolls when its revision changes. Resolution order: member →
                env → default true. Set false to freeze a member on its current revision.
            max_unavailable (str | Unset): Rollout unavailability budget as an absolute count ("3") or a percentage of
                desired idle replicas ("20%"). Rounded down, floored at 1. Resolution order: member → env → default "20%".
     """

    auto_update: bool | Unset = UNSET
    max_unavailable: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        auto_update = self.auto_update

        max_unavailable = self.max_unavailable


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if auto_update is not UNSET:
            field_dict["autoUpdate"] = auto_update
        if max_unavailable is not UNSET:
            field_dict["maxUnavailable"] = max_unavailable

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        auto_update = d.pop("autoUpdate", UNSET)

        max_unavailable = d.pop("maxUnavailable", UNSET)

        env_update_strategy = cls(
            auto_update=auto_update,
            max_unavailable=max_unavailable,
        )


        env_update_strategy.additional_properties = d
        return env_update_strategy

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
