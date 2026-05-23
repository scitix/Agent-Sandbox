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
  from ..models.env_autoscaling_group import EnvAutoscalingGroup





T = TypeVar("T", bound="EnvAutoscalingSpec")



@_attrs_define
class EnvAutoscalingSpec:
    """ 
        Attributes:
            enabled (bool | Unset): Master switch. When false, the autoscaler is dormant — Pool replicas are managed
                manually.
            groups (list[EnvAutoscalingGroup] | Unset): Per-scaling-group policies. MVP only consults groups[0]; multi-group
                support arrives with multi-resource Envs.
     """

    enabled: bool | Unset = UNSET
    groups: list[EnvAutoscalingGroup] | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.env_autoscaling_group import EnvAutoscalingGroup
        enabled = self.enabled

        groups: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.groups, Unset):
            groups = []
            for groups_item_data in self.groups:
                groups_item = groups_item_data.to_dict()
                groups.append(groups_item)




        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if enabled is not UNSET:
            field_dict["enabled"] = enabled
        if groups is not UNSET:
            field_dict["groups"] = groups

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.env_autoscaling_group import EnvAutoscalingGroup
        d = dict(src_dict)
        enabled = d.pop("enabled", UNSET)

        _groups = d.pop("groups", UNSET)
        groups: list[EnvAutoscalingGroup] | Unset = UNSET
        if _groups is not UNSET:
            groups = []
            for groups_item_data in _groups:
                groups_item = EnvAutoscalingGroup.from_dict(groups_item_data)



                groups.append(groups_item)


        env_autoscaling_spec = cls(
            enabled=enabled,
            groups=groups,
        )


        env_autoscaling_spec.additional_properties = d
        return env_autoscaling_spec

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
