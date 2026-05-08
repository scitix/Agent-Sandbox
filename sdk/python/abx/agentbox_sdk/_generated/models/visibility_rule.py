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






T = TypeVar("T", bound="VisibilityRule")



@_attrs_define
class VisibilityRule:
    """ 
        Attributes:
            team (str | Unset): Team name that this rule applies to.
            users (list[str] | Unset): Specific users within the team that this rule applies to. Empty means all users in
                the team.
     """

    team: str | Unset = UNSET
    users: list[str] | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        team = self.team

        users: list[str] | Unset = UNSET
        if not isinstance(self.users, Unset):
            users = self.users




        field_dict: dict[str, Any] = {}

        field_dict.update({
        })
        if team is not UNSET:
            field_dict["team"] = team
        if users is not UNSET:
            field_dict["users"] = users

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        team = d.pop("team", UNSET)

        users = cast(list[str], d.pop("users", UNSET))


        visibility_rule = cls(
            team=team,
            users=users,
        )

        return visibility_rule

