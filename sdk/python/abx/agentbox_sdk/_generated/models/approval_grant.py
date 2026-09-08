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

from ..models.approval_grant_scope import ApprovalGrantScope
from ..types import UNSET, Unset
from typing import cast
import datetime

if TYPE_CHECKING:
  from ..models.approval_principal import ApprovalPrincipal





T = TypeVar("T", bound="ApprovalGrant")



@_attrs_define
class ApprovalGrant:
    """ 
        Attributes:
            id (str):
            principal (ApprovalPrincipal):
            scope (ApprovalGrantScope):
            operation (str):
            created_at (datetime.datetime):
            granted_by (str):
            session_id (str | Unset):
            expires_at (datetime.datetime | Unset): Absent for a key-scoped grant, which ends only when revoked.
     """

    id: str
    principal: ApprovalPrincipal
    scope: ApprovalGrantScope
    operation: str
    created_at: datetime.datetime
    granted_by: str
    session_id: str | Unset = UNSET
    expires_at: datetime.datetime | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.approval_principal import ApprovalPrincipal # noqa: PLC0415
        id = self.id

        principal = self.principal.to_dict()

        scope = self.scope.value

        operation = self.operation

        created_at = self.created_at.isoformat()

        granted_by = self.granted_by

        session_id = self.session_id

        expires_at: str | Unset = UNSET
        if not isinstance(self.expires_at, Unset):
            expires_at = self.expires_at.isoformat()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "id": id,
            "principal": principal,
            "scope": scope,
            "operation": operation,
            "createdAt": created_at,
            "grantedBy": granted_by,
        })
        if session_id is not UNSET:
            field_dict["sessionId"] = session_id
        if expires_at is not UNSET:
            field_dict["expiresAt"] = expires_at

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.approval_principal import ApprovalPrincipal # noqa: PLC0415
        d = dict(src_dict)
        id = d.pop("id")

        principal = ApprovalPrincipal.from_dict(d.pop("principal"))




        scope = ApprovalGrantScope(d.pop("scope"))




        operation = d.pop("operation")

        created_at = datetime.datetime.fromisoformat(d.pop("createdAt"))




        granted_by = d.pop("grantedBy")

        session_id = d.pop("sessionId", UNSET)

        _expires_at = d.pop("expiresAt", UNSET)
        expires_at: datetime.datetime | Unset
        if isinstance(_expires_at,  Unset):
            expires_at = UNSET
        else:
            expires_at = datetime.datetime.fromisoformat(_expires_at)




        approval_grant = cls(
            id=id,
            principal=principal,
            scope=scope,
            operation=operation,
            created_at=created_at,
            granted_by=granted_by,
            session_id=session_id,
            expires_at=expires_at,
        )


        approval_grant.additional_properties = d
        return approval_grant

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
