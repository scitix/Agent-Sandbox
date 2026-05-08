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






T = TypeVar("T", bound="CreateAPIKeyRequest")



@_attrs_define
class CreateAPIKeyRequest:
    """ 
        Attributes:
            namespace (str | Unset): Kubernetes namespace to associate the key with.
            user (str | Unset): Username to associate the key with.
            team (str | Unset): Team name to associate the key with.
            description (str | Unset): Optional human-readable description for this key.
            expires_at (datetime.datetime | Unset): Optional RFC 3339 expiry timestamp. Omit for a non-expiring key.
            token_hash (str | Unset): Full SHA-256 hex hash of the raw token (64 hex chars). When provided together with
                hashPrefix, the key is imported using the given hash instead of generating a new random token. The operation is
                idempotent — if a key with the same hash already exists it is silently accepted. Admin-only.
            hash_prefix (str | Unset): First 16 hex characters of the tokenHash. Required when tokenHash is provided.
            issued_at (datetime.datetime | Unset): Original issue timestamp (import mode). Used to preserve the original
                creation time. Ignored when tokenHash is not provided.
            quota_url (str | Unset): Quota URL associated with the key (import mode).
     """

    namespace: str | Unset = UNSET
    user: str | Unset = UNSET
    team: str | Unset = UNSET
    description: str | Unset = UNSET
    expires_at: datetime.datetime | Unset = UNSET
    token_hash: str | Unset = UNSET
    hash_prefix: str | Unset = UNSET
    issued_at: datetime.datetime | Unset = UNSET
    quota_url: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        namespace = self.namespace

        user = self.user

        team = self.team

        description = self.description

        expires_at: str | Unset = UNSET
        if not isinstance(self.expires_at, Unset):
            expires_at = self.expires_at.isoformat()

        token_hash = self.token_hash

        hash_prefix = self.hash_prefix

        issued_at: str | Unset = UNSET
        if not isinstance(self.issued_at, Unset):
            issued_at = self.issued_at.isoformat()

        quota_url = self.quota_url


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if namespace is not UNSET:
            field_dict["namespace"] = namespace
        if user is not UNSET:
            field_dict["user"] = user
        if team is not UNSET:
            field_dict["team"] = team
        if description is not UNSET:
            field_dict["description"] = description
        if expires_at is not UNSET:
            field_dict["expiresAt"] = expires_at
        if token_hash is not UNSET:
            field_dict["tokenHash"] = token_hash
        if hash_prefix is not UNSET:
            field_dict["hashPrefix"] = hash_prefix
        if issued_at is not UNSET:
            field_dict["issuedAt"] = issued_at
        if quota_url is not UNSET:
            field_dict["quotaURL"] = quota_url

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        namespace = d.pop("namespace", UNSET)

        user = d.pop("user", UNSET)

        team = d.pop("team", UNSET)

        description = d.pop("description", UNSET)

        _expires_at = d.pop("expiresAt", UNSET)
        expires_at: datetime.datetime | Unset
        if isinstance(_expires_at,  Unset):
            expires_at = UNSET
        else:
            expires_at = isoparse(_expires_at)




        token_hash = d.pop("tokenHash", UNSET)

        hash_prefix = d.pop("hashPrefix", UNSET)

        _issued_at = d.pop("issuedAt", UNSET)
        issued_at: datetime.datetime | Unset
        if isinstance(_issued_at,  Unset):
            issued_at = UNSET
        else:
            issued_at = isoparse(_issued_at)




        quota_url = d.pop("quotaURL", UNSET)

        create_api_key_request = cls(
            namespace=namespace,
            user=user,
            team=team,
            description=description,
            expires_at=expires_at,
            token_hash=token_hash,
            hash_prefix=hash_prefix,
            issued_at=issued_at,
            quota_url=quota_url,
        )


        create_api_key_request.additional_properties = d
        return create_api_key_request

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
