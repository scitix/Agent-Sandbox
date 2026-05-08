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






T = TypeVar("T", bound="PoolTemplateOverrides")



@_attrs_define
class PoolTemplateOverrides:
    """ Persisted pool-level overrides applied on top of the referenced template and re-applied during template sync.

        Attributes:
            image (str | Unset): Override the main container (containers[0]) image
            resource_multiplier (int | Unset): Uniform scale factor for all container CPU/Memory requests+limits (>= 1).
                Also multiplies reservation.replicaQuota values.
     """

    image: str | Unset = UNSET
    resource_multiplier: int | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        image = self.image

        resource_multiplier = self.resource_multiplier


        field_dict: dict[str, Any] = {}

        field_dict.update({
        })
        if image is not UNSET:
            field_dict["image"] = image
        if resource_multiplier is not UNSET:
            field_dict["resourceMultiplier"] = resource_multiplier

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        image = d.pop("image", UNSET)

        resource_multiplier = d.pop("resourceMultiplier", UNSET)

        pool_template_overrides = cls(
            image=image,
            resource_multiplier=resource_multiplier,
        )

        return pool_template_overrides

