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
from dateutil.parser import isoparse
from typing import cast
import datetime






T = TypeVar("T", bound="EnvEvent")



@_attrs_define
class EnvEvent:
    """ 
        Attributes:
            involved_kind (str): Kind of the K8s object this event was emitted against. One of SandboxEnv | SandboxPool.
            involved_name (str): metadata.name of the involved object.
            reason (str): Event reason (machine-readable verb): ScaleUp / ScaleDown / PoolReady / PoolRecovered / Degraded /
                AutoscalerScaleUp / AutoscalerScaleDown / SandboxPoolPhase*.
            message (str): Human-readable message body.
            type_ (str): Normal | Warning
            count (int): Number of times this event has fired. K8s coalesces repeated identical events and bumps this
                counter.
            action (str | Unset): Event action (machine-readable). Sometimes absent on older events.
            first_timestamp (datetime.datetime | Unset): First time this event was observed (RFC3339).
            last_timestamp (datetime.datetime | Unset): Most recent time this event was observed (RFC3339).
     """

    involved_kind: str
    involved_name: str
    reason: str
    message: str
    type_: str
    count: int
    action: str | Unset = UNSET
    first_timestamp: datetime.datetime | Unset = UNSET
    last_timestamp: datetime.datetime | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        involved_kind = self.involved_kind

        involved_name = self.involved_name

        reason = self.reason

        message = self.message

        type_ = self.type_

        count = self.count

        action = self.action

        first_timestamp: str | Unset = UNSET
        if not isinstance(self.first_timestamp, Unset):
            first_timestamp = self.first_timestamp.isoformat()

        last_timestamp: str | Unset = UNSET
        if not isinstance(self.last_timestamp, Unset):
            last_timestamp = self.last_timestamp.isoformat()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "involvedKind": involved_kind,
            "involvedName": involved_name,
            "reason": reason,
            "message": message,
            "type": type_,
            "count": count,
        })
        if action is not UNSET:
            field_dict["action"] = action
        if first_timestamp is not UNSET:
            field_dict["firstTimestamp"] = first_timestamp
        if last_timestamp is not UNSET:
            field_dict["lastTimestamp"] = last_timestamp

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        involved_kind = d.pop("involvedKind")

        involved_name = d.pop("involvedName")

        reason = d.pop("reason")

        message = d.pop("message")

        type_ = d.pop("type")

        count = d.pop("count")

        action = d.pop("action", UNSET)

        _first_timestamp = d.pop("firstTimestamp", UNSET)
        first_timestamp: datetime.datetime | Unset
        if isinstance(_first_timestamp,  Unset):
            first_timestamp = UNSET
        else:
            first_timestamp = isoparse(_first_timestamp)




        _last_timestamp = d.pop("lastTimestamp", UNSET)
        last_timestamp: datetime.datetime | Unset
        if isinstance(_last_timestamp,  Unset):
            last_timestamp = UNSET
        else:
            last_timestamp = isoparse(_last_timestamp)




        env_event = cls(
            involved_kind=involved_kind,
            involved_name=involved_name,
            reason=reason,
            message=message,
            type_=type_,
            count=count,
            action=action,
            first_timestamp=first_timestamp,
            last_timestamp=last_timestamp,
        )


        env_event.additional_properties = d
        return env_event

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
