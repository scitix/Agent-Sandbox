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






T = TypeVar("T", bound="SandboxEndpoint")



@_attrs_define
class SandboxEndpoint:
    """ 
        Attributes:
            url (str): Fully-qualified URL to reach this runtime endpoint (e.g. https://sandbox.example.com/api).
            log_dir (str | Unset): Path to the runtime log file inside the container, if configured
     """

    url: str
    log_dir: str | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        url = self.url

        log_dir = self.log_dir


        field_dict: dict[str, Any] = {}

        field_dict.update({
            "url": url,
        })
        if log_dir is not UNSET:
            field_dict["logDir"] = log_dir

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        url = d.pop("url")

        log_dir = d.pop("logDir", UNSET)

        sandbox_endpoint = cls(
            url=url,
            log_dir=log_dir,
        )

        return sandbox_endpoint

