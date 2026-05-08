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

from typing import cast

if TYPE_CHECKING:
  from ..models.registry_credential import RegistryCredential





T = TypeVar("T", bound="ImagePullSecretInput")



@_attrs_define
class ImagePullSecretInput:
    """ 
        Attributes:
            registries (list[RegistryCredential]):
     """

    registries: list[RegistryCredential]
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.registry_credential import RegistryCredential
        registries = []
        for registries_item_data in self.registries:
            registries_item = registries_item_data.to_dict()
            registries.append(registries_item)




        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "registries": registries,
        })

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.registry_credential import RegistryCredential
        d = dict(src_dict)
        registries = []
        _registries = d.pop("registries")
        for registries_item_data in (_registries):
            registries_item = RegistryCredential.from_dict(registries_item_data)



            registries.append(registries_item)


        image_pull_secret_input = cls(
            registries=registries,
        )


        image_pull_secret_input.additional_properties = d
        return image_pull_secret_input

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
