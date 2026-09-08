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

from ..models.approval_decision_request_decision import ApprovalDecisionRequestDecision
from ..models.approval_decision_request_scope import ApprovalDecisionRequestScope
from ..types import UNSET, Unset






T = TypeVar("T", bound="ApprovalDecisionRequest")



@_attrs_define
class ApprovalDecisionRequest:
    """ 
        Attributes:
            decision (ApprovalDecisionRequestDecision):
            scope (ApprovalDecisionRequestScope | Unset): Ignored when denying. `session` needs the request to carry a
                session id; both wider scopes are refused for a once-only operation. Default: ApprovalDecisionRequestScope.ONCE.
     """

    decision: ApprovalDecisionRequestDecision
    scope: ApprovalDecisionRequestScope | Unset = ApprovalDecisionRequestScope.ONCE





    def to_dict(self) -> dict[str, Any]:
        decision = self.decision.value

        scope: str | Unset = UNSET
        if not isinstance(self.scope, Unset):
            scope = self.scope.value



        field_dict: dict[str, Any] = {}

        field_dict.update({
            "decision": decision,
        })
        if scope is not UNSET:
            field_dict["scope"] = scope

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        decision = ApprovalDecisionRequestDecision(d.pop("decision"))




        _scope = d.pop("scope", UNSET)
        scope: ApprovalDecisionRequestScope | Unset
        if isinstance(_scope,  Unset):
            scope = UNSET
        else:
            scope = ApprovalDecisionRequestScope(_scope)




        approval_decision_request = cls(
            decision=decision,
            scope=scope,
        )

        return approval_decision_request

