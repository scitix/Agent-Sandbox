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
  from ..models.sandbox_template_spec import SandboxTemplateSpec





T = TypeVar("T", bound="UpsertSandboxTemplateRequest")



@_attrs_define
class UpsertSandboxTemplateRequest:
    """ 
        Attributes:
            name (str | Unset): RFC 1123 DNS label (letter-start): lowercase letters, digits, hyphens; start with a letter,
                end with alphanumeric
            spec (SandboxTemplateSpec | Unset):
            crd_yaml (str | Unset): Complete SandboxTemplate CRD YAML string. When provided, name/spec/labels/annotations
                are extracted from the YAML and the individual fields above are ignored.
     """

    name: str | Unset = UNSET
    spec: SandboxTemplateSpec | Unset = UNSET
    crd_yaml: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.sandbox_template_spec import SandboxTemplateSpec
        name = self.name

        spec: dict[str, Any] | Unset = UNSET
        if not isinstance(self.spec, Unset):
            spec = self.spec.to_dict()

        crd_yaml = self.crd_yaml


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if name is not UNSET:
            field_dict["name"] = name
        if spec is not UNSET:
            field_dict["spec"] = spec
        if crd_yaml is not UNSET:
            field_dict["crdYaml"] = crd_yaml

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.sandbox_template_spec import SandboxTemplateSpec
        d = dict(src_dict)
        name = d.pop("name", UNSET)

        _spec = d.pop("spec", UNSET)
        spec: SandboxTemplateSpec | Unset
        if isinstance(_spec,  Unset):
            spec = UNSET
        else:
            spec = SandboxTemplateSpec.from_dict(_spec)




        crd_yaml = d.pop("crdYaml", UNSET)

        upsert_sandbox_template_request = cls(
            name=name,
            spec=spec,
            crd_yaml=crd_yaml,
        )


        upsert_sandbox_template_request.additional_properties = d
        return upsert_sandbox_template_request

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
