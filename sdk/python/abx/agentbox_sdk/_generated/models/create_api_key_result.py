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






T = TypeVar("T", bound="CreateAPIKeyResult")



@_attrs_define
class CreateAPIKeyResult:
    """ 
        Attributes:
            api_key (str): The raw API key value (only returned once at creation time; store it securely).
            key_id (str): Unique identifier for this API key (used to look up or delete the key).
            role (str): Role granted by this key (e.g. tenant, admin).
            issued_at (datetime.datetime): RFC 3339 timestamp when the key was created.
            user (str | Unset): Username associated with the key.
            team (str | Unset): Team associated with the key.
            description (str | Unset): Human-readable description of the key.
            expires_at (datetime.datetime | Unset): RFC 3339 expiry timestamp, or absent if the key never expires.
     """

    api_key: str
    key_id: str
    role: str
    issued_at: datetime.datetime
    user: str | Unset = UNSET
    team: str | Unset = UNSET
    description: str | Unset = UNSET
    expires_at: datetime.datetime | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        api_key = self.api_key

        key_id = self.key_id

        role = self.role

        issued_at = self.issued_at.isoformat()

        user = self.user

        team = self.team

        description = self.description

        expires_at: str | Unset = UNSET
        if not isinstance(self.expires_at, Unset):
            expires_at = self.expires_at.isoformat()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "apiKey": api_key,
            "keyId": key_id,
            "role": role,
            "issuedAt": issued_at,
        })
        if user is not UNSET:
            field_dict["user"] = user
        if team is not UNSET:
            field_dict["team"] = team
        if description is not UNSET:
            field_dict["description"] = description
        if expires_at is not UNSET:
            field_dict["expiresAt"] = expires_at

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        api_key = d.pop("apiKey")

        key_id = d.pop("keyId")

        role = d.pop("role")

        issued_at = isoparse(d.pop("issuedAt"))




        user = d.pop("user", UNSET)

        team = d.pop("team", UNSET)

        description = d.pop("description", UNSET)

        _expires_at = d.pop("expiresAt", UNSET)
        expires_at: datetime.datetime | Unset
        if isinstance(_expires_at,  Unset):
            expires_at = UNSET
        else:
            expires_at = isoparse(_expires_at)




        create_api_key_result = cls(
            api_key=api_key,
            key_id=key_id,
            role=role,
            issued_at=issued_at,
            user=user,
            team=team,
            description=description,
            expires_at=expires_at,
        )


        create_api_key_result.additional_properties = d
        return create_api_key_result

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
