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






T = TypeVar("T", bound="EnvCondition")



@_attrs_define
class EnvCondition:
    """ 
        Attributes:
            type_ (str):
            status (str):
            reason (str | Unset):
            message (str | Unset):
            last_transition_time (datetime.datetime | Unset):
     """

    type_: str
    status: str
    reason: str | Unset = UNSET
    message: str | Unset = UNSET
    last_transition_time: datetime.datetime | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        type_ = self.type_

        status = self.status

        reason = self.reason

        message = self.message

        last_transition_time: str | Unset = UNSET
        if not isinstance(self.last_transition_time, Unset):
            last_transition_time = self.last_transition_time.isoformat()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "type": type_,
            "status": status,
        })
        if reason is not UNSET:
            field_dict["reason"] = reason
        if message is not UNSET:
            field_dict["message"] = message
        if last_transition_time is not UNSET:
            field_dict["lastTransitionTime"] = last_transition_time

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        type_ = d.pop("type")

        status = d.pop("status")

        reason = d.pop("reason", UNSET)

        message = d.pop("message", UNSET)

        _last_transition_time = d.pop("lastTransitionTime", UNSET)
        last_transition_time: datetime.datetime | Unset
        if isinstance(_last_transition_time,  Unset):
            last_transition_time = UNSET
        else:
            last_transition_time = datetime.datetime.fromisoformat(_last_transition_time)




        env_condition = cls(
            type_=type_,
            status=status,
            reason=reason,
            message=message,
            last_transition_time=last_transition_time,
        )


        env_condition.additional_properties = d
        return env_condition

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
