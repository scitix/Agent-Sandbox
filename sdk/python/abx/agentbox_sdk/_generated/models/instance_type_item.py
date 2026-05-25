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
  from ..models.instance_type_item_extensions import InstanceTypeItemExtensions
  from ..models.resource_requirements import ResourceRequirements





T = TypeVar("T", bound="InstanceTypeItem")



@_attrs_define
class InstanceTypeItem:
    """ Catalog entry exposing a named base resource shape. Sandboxes pick an entry by `name` and a positive integer
    multiplier; resolution to Pod resources happens server-side.

        Attributes:
            name (str): Catalog key. Unique within a deployment.
            base_resources (ResourceRequirements): Subset of Kubernetes corev1.ResourceRequirements used for per-Pool
                resource sizing on EnvClusterMember.inlineResources.
            show_name (str | Unset): User-facing label. Falls back to `name` when empty.
            description (str | Unset): Free-form description for tooltips.
            max_multiplier (int | Unset): Maximum allowed multiplier. 0 means no cap.
            cost (str | Unset): Free-form cost weight for client-side sorting / display.
            extensions (InstanceTypeItemExtensions | Unset): Backend-specific key/value pairs (e.g. `gpu-type`). Open
                clients pass these through unchanged for display only.
     """

    name: str
    base_resources: ResourceRequirements
    show_name: str | Unset = UNSET
    description: str | Unset = UNSET
    max_multiplier: int | Unset = UNSET
    cost: str | Unset = UNSET
    extensions: InstanceTypeItemExtensions | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.instance_type_item_extensions import InstanceTypeItemExtensions
        from ..models.resource_requirements import ResourceRequirements
        name = self.name

        base_resources = self.base_resources.to_dict()

        show_name = self.show_name

        description = self.description

        max_multiplier = self.max_multiplier

        cost = self.cost

        extensions: dict[str, Any] | Unset = UNSET
        if not isinstance(self.extensions, Unset):
            extensions = self.extensions.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "name": name,
            "baseResources": base_resources,
        })
        if show_name is not UNSET:
            field_dict["showName"] = show_name
        if description is not UNSET:
            field_dict["description"] = description
        if max_multiplier is not UNSET:
            field_dict["maxMultiplier"] = max_multiplier
        if cost is not UNSET:
            field_dict["cost"] = cost
        if extensions is not UNSET:
            field_dict["extensions"] = extensions

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.instance_type_item_extensions import InstanceTypeItemExtensions
        from ..models.resource_requirements import ResourceRequirements
        d = dict(src_dict)
        name = d.pop("name")

        base_resources = ResourceRequirements.from_dict(d.pop("baseResources"))




        show_name = d.pop("showName", UNSET)

        description = d.pop("description", UNSET)

        max_multiplier = d.pop("maxMultiplier", UNSET)

        cost = d.pop("cost", UNSET)

        _extensions = d.pop("extensions", UNSET)
        extensions: InstanceTypeItemExtensions | Unset
        if isinstance(_extensions,  Unset):
            extensions = UNSET
        else:
            extensions = InstanceTypeItemExtensions.from_dict(_extensions)




        instance_type_item = cls(
            name=name,
            base_resources=base_resources,
            show_name=show_name,
            description=description,
            max_multiplier=max_multiplier,
            cost=cost,
            extensions=extensions,
        )


        instance_type_item.additional_properties = d
        return instance_type_item

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
