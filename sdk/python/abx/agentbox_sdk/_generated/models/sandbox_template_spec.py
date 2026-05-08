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
  from ..models.runtime import Runtime
  from ..models.visibility_config import VisibilityConfig





T = TypeVar("T", bound="SandboxTemplateSpec")



@_attrs_define
class SandboxTemplateSpec:
    """ 
        Attributes:
            version (str | Unset): User-defined version string for the template (e.g. "v1.2.3").
            description (str | Unset): Human-readable description of the template and its intended use.
            idle_image (str | Unset): Container image used when the pod is in Idle phase (lightweight placeholder image).
            template (str | Unset): Kubernetes PodTemplateSpec serialized as YAML/JSON string, passed through as-is
            runtimes (list[Runtime] | Unset): Runtime port mappings for the sandbox
            visibility (VisibilityConfig | Unset):
     """

    version: str | Unset = UNSET
    description: str | Unset = UNSET
    idle_image: str | Unset = UNSET
    template: str | Unset = UNSET
    runtimes: list[Runtime] | Unset = UNSET
    visibility: VisibilityConfig | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.runtime import Runtime
        from ..models.visibility_config import VisibilityConfig
        version = self.version

        description = self.description

        idle_image = self.idle_image

        template = self.template

        runtimes: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.runtimes, Unset):
            runtimes = []
            for runtimes_item_data in self.runtimes:
                runtimes_item = runtimes_item_data.to_dict()
                runtimes.append(runtimes_item)



        visibility: dict[str, Any] | Unset = UNSET
        if not isinstance(self.visibility, Unset):
            visibility = self.visibility.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if version is not UNSET:
            field_dict["version"] = version
        if description is not UNSET:
            field_dict["description"] = description
        if idle_image is not UNSET:
            field_dict["idleImage"] = idle_image
        if template is not UNSET:
            field_dict["template"] = template
        if runtimes is not UNSET:
            field_dict["runtimes"] = runtimes
        if visibility is not UNSET:
            field_dict["visibility"] = visibility

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.runtime import Runtime
        from ..models.visibility_config import VisibilityConfig
        d = dict(src_dict)
        version = d.pop("version", UNSET)

        description = d.pop("description", UNSET)

        idle_image = d.pop("idleImage", UNSET)

        template = d.pop("template", UNSET)

        _runtimes = d.pop("runtimes", UNSET)
        runtimes: list[Runtime] | Unset = UNSET
        if _runtimes is not UNSET:
            runtimes = []
            for runtimes_item_data in _runtimes:
                runtimes_item = Runtime.from_dict(runtimes_item_data)



                runtimes.append(runtimes_item)


        _visibility = d.pop("visibility", UNSET)
        visibility: VisibilityConfig | Unset
        if isinstance(_visibility,  Unset):
            visibility = UNSET
        else:
            visibility = VisibilityConfig.from_dict(_visibility)




        sandbox_template_spec = cls(
            version=version,
            description=description,
            idle_image=idle_image,
            template=template,
            runtimes=runtimes,
            visibility=visibility,
        )


        sandbox_template_spec.additional_properties = d
        return sandbox_template_spec

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
