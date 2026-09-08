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

from ..models.approval_request_scope import ApprovalRequestScope
from ..models.approval_request_status import ApprovalRequestStatus
from ..types import UNSET, Unset
from typing import cast
import datetime

if TYPE_CHECKING:
  from ..models.approval_principal import ApprovalPrincipal





T = TypeVar("T", bound="ApprovalRequest")



@_attrs_define
class ApprovalRequest:
    """ 
        Attributes:
            id (str):
            principal (ApprovalPrincipal):
            operation (str): What is being asked for, e.g. `env.create`. A grant is written against this, not against a
                route.
            once_only (bool): True for destructive and credential-minting calls, which refuse the session and key scopes.
            method (str):
            path (str):
            summary (str): What the person is being asked, naming the thing acted on.
            created_at (datetime.datetime):
            expires_at (datetime.datetime):
            status (ApprovalRequestStatus):
            session_id (str | Unset): Absent when the caller sent no session header. Such a request can only ever be
                approved for one call.
            scope (ApprovalRequestScope | Unset): How far the decision reached. Set once decided.
            decided_by (str | Unset):
            decided_at (datetime.datetime | Unset):
     """

    id: str
    principal: ApprovalPrincipal
    operation: str
    once_only: bool
    method: str
    path: str
    summary: str
    created_at: datetime.datetime
    expires_at: datetime.datetime
    status: ApprovalRequestStatus
    session_id: str | Unset = UNSET
    scope: ApprovalRequestScope | Unset = UNSET
    decided_by: str | Unset = UNSET
    decided_at: datetime.datetime | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.approval_principal import ApprovalPrincipal # noqa: PLC0415
        id = self.id

        principal = self.principal.to_dict()

        operation = self.operation

        once_only = self.once_only

        method = self.method

        path = self.path

        summary = self.summary

        created_at = self.created_at.isoformat()

        expires_at = self.expires_at.isoformat()

        status = self.status.value

        session_id = self.session_id

        scope: str | Unset = UNSET
        if not isinstance(self.scope, Unset):
            scope = self.scope.value


        decided_by = self.decided_by

        decided_at: str | Unset = UNSET
        if not isinstance(self.decided_at, Unset):
            decided_at = self.decided_at.isoformat()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "id": id,
            "principal": principal,
            "operation": operation,
            "onceOnly": once_only,
            "method": method,
            "path": path,
            "summary": summary,
            "createdAt": created_at,
            "expiresAt": expires_at,
            "status": status,
        })
        if session_id is not UNSET:
            field_dict["sessionId"] = session_id
        if scope is not UNSET:
            field_dict["scope"] = scope
        if decided_by is not UNSET:
            field_dict["decidedBy"] = decided_by
        if decided_at is not UNSET:
            field_dict["decidedAt"] = decided_at

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.approval_principal import ApprovalPrincipal # noqa: PLC0415
        d = dict(src_dict)
        id = d.pop("id")

        principal = ApprovalPrincipal.from_dict(d.pop("principal"))




        operation = d.pop("operation")

        once_only = d.pop("onceOnly")

        method = d.pop("method")

        path = d.pop("path")

        summary = d.pop("summary")

        created_at = datetime.datetime.fromisoformat(d.pop("createdAt"))




        expires_at = datetime.datetime.fromisoformat(d.pop("expiresAt"))




        status = ApprovalRequestStatus(d.pop("status"))




        session_id = d.pop("sessionId", UNSET)

        _scope = d.pop("scope", UNSET)
        scope: ApprovalRequestScope | Unset
        if isinstance(_scope,  Unset):
            scope = UNSET
        else:
            scope = ApprovalRequestScope(_scope)




        decided_by = d.pop("decidedBy", UNSET)

        _decided_at = d.pop("decidedAt", UNSET)
        decided_at: datetime.datetime | Unset
        if isinstance(_decided_at,  Unset):
            decided_at = UNSET
        else:
            decided_at = datetime.datetime.fromisoformat(_decided_at)




        approval_request = cls(
            id=id,
            principal=principal,
            operation=operation,
            once_only=once_only,
            method=method,
            path=path,
            summary=summary,
            created_at=created_at,
            expires_at=expires_at,
            status=status,
            session_id=session_id,
            scope=scope,
            decided_by=decided_by,
            decided_at=decided_at,
        )


        approval_request.additional_properties = d
        return approval_request

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
