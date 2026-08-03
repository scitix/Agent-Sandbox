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

from ..models.header_injection_mode import HeaderInjectionMode
from ..types import UNSET, Unset






T = TypeVar("T", bound="HeaderInjection")



@_attrs_define
class HeaderInjection:
    """ 
        Attributes:
            name (str): Header name, compared case-insensitively.
            value (str): Value template referencing declared credentials as '{{ credName }}', e.g. 'Bearer {{ openai }}'. A
                literal here would be a plaintext secret in the CRD and is rejected.
            mode (HeaderInjectionMode | Unset): Override (default) replaces whatever the sandbox sent; IfAbsent injects only
                when the sandbox set no such header, so an agent supplying its own credential keeps it.
     """

    name: str
    value: str
    mode: HeaderInjectionMode | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        name = self.name

        value = self.value

        mode: str | Unset = UNSET
        if not isinstance(self.mode, Unset):
            mode = self.mode.value



        field_dict: dict[str, Any] = {}

        field_dict.update({
            "name": name,
            "value": value,
        })
        if mode is not UNSET:
            field_dict["mode"] = mode

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        name = d.pop("name")

        value = d.pop("value")

        _mode = d.pop("mode", UNSET)
        mode: HeaderInjectionMode | Unset
        if isinstance(_mode,  Unset):
            mode = UNSET
        else:
            mode = HeaderInjectionMode(_mode)




        header_injection = cls(
            name=name,
            value=value,
            mode=mode,
        )

        return header_injection

