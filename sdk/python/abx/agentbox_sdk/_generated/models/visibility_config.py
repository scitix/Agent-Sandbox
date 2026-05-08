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

if TYPE_CHECKING:
  from ..models.visibility_rule import VisibilityRule





T = TypeVar("T", bound="VisibilityConfig")



@_attrs_define
class VisibilityConfig:
    """ 
        Attributes:
            rules (list[VisibilityRule] | Unset): List of visibility rules controlling which teams and users can see this
                template.
     """

    rules: list[VisibilityRule] | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        from ..models.visibility_rule import VisibilityRule
        rules: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.rules, Unset):
            rules = []
            for rules_item_data in self.rules:
                rules_item = rules_item_data.to_dict()
                rules.append(rules_item)




        field_dict: dict[str, Any] = {}

        field_dict.update({
        })
        if rules is not UNSET:
            field_dict["rules"] = rules

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.visibility_rule import VisibilityRule
        d = dict(src_dict)
        _rules = d.pop("rules", UNSET)
        rules: list[VisibilityRule] | Unset = UNSET
        if _rules is not UNSET:
            rules = []
            for rules_item_data in _rules:
                rules_item = VisibilityRule.from_dict(rules_item_data)



                rules.append(rules_item)


        visibility_config = cls(
            rules=rules,
        )

        return visibility_config

