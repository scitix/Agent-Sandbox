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
import datetime






T = TypeVar("T", bound="SandboxStatusDetail")



@_attrs_define
class SandboxStatusDetail:
    """ 
        Attributes:
            reason (str | Unset): Machine-readable reason code for the current status (mirrors Kubernetes condition reason).
            message (str | Unset): Human-readable message explaining the current status.
            last_updated_time (datetime.datetime | Unset): RFC 3339 timestamp of when this status detail was last updated.
     """

    reason: str | Unset = UNSET
    message: str | Unset = UNSET
    last_updated_time: datetime.datetime | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        reason = self.reason

        message = self.message

        last_updated_time: str | Unset = UNSET
        if not isinstance(self.last_updated_time, Unset):
            last_updated_time = self.last_updated_time.isoformat()


        field_dict: dict[str, Any] = {}

        field_dict.update({
        })
        if reason is not UNSET:
            field_dict["reason"] = reason
        if message is not UNSET:
            field_dict["message"] = message
        if last_updated_time is not UNSET:
            field_dict["lastUpdatedTime"] = last_updated_time

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        reason = d.pop("reason", UNSET)

        message = d.pop("message", UNSET)

        _last_updated_time = d.pop("lastUpdatedTime", UNSET)
        last_updated_time: datetime.datetime | Unset
        if isinstance(_last_updated_time,  Unset):
            last_updated_time = UNSET
        else:
            last_updated_time = datetime.datetime.fromisoformat(_last_updated_time)




        sandbox_status_detail = cls(
            reason=reason,
            message=message,
            last_updated_time=last_updated_time,
        )

        return sandbox_status_detail

