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

from typing import cast

if TYPE_CHECKING:
  from ..models.approval_grant import ApprovalGrant
  from ..models.approval_request import ApprovalRequest





T = TypeVar("T", bound="ListApprovalsResult")



@_attrs_define
class ListApprovalsResult:
    """ 
        Attributes:
            pending (list[ApprovalRequest]):
            grants (list[ApprovalGrant]):
     """

    pending: list[ApprovalRequest]
    grants: list[ApprovalGrant]
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.approval_grant import ApprovalGrant # noqa: PLC0415
        from ..models.approval_request import ApprovalRequest # noqa: PLC0415
        pending = []
        for pending_item_data in self.pending:
            pending_item = pending_item_data.to_dict()
            pending.append(pending_item)



        grants = []
        for grants_item_data in self.grants:
            grants_item = grants_item_data.to_dict()
            grants.append(grants_item)




        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "pending": pending,
            "grants": grants,
        })

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.approval_grant import ApprovalGrant # noqa: PLC0415
        from ..models.approval_request import ApprovalRequest # noqa: PLC0415
        d = dict(src_dict)
        pending = []
        _pending = d.pop("pending")
        for pending_item_data in (_pending):
            pending_item = ApprovalRequest.from_dict(pending_item_data)



            pending.append(pending_item)


        grants = []
        _grants = d.pop("grants")
        for grants_item_data in (_grants):
            grants_item = ApprovalGrant.from_dict(grants_item_data)



            grants.append(grants_item)


        list_approvals_result = cls(
            pending=pending,
            grants=grants,
        )


        list_approvals_result.additional_properties = d
        return list_approvals_result

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
