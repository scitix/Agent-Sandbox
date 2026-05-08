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






T = TypeVar("T", bound="UpdateSandboxPoolRequestOverrides")



@_attrs_define
class UpdateSandboxPoolRequestOverrides:
    """ Partial overrides to update. Only image can be updated after pool creation.

        Attributes:
            image (str | Unset): Override the main container (containers[0]) image
     """

    image: str | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        image = self.image


        field_dict: dict[str, Any] = {}

        field_dict.update({
        })
        if image is not UNSET:
            field_dict["image"] = image

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        image = d.pop("image", UNSET)

        update_sandbox_pool_request_overrides = cls(
            image=image,
        )

        return update_sandbox_pool_request_overrides

