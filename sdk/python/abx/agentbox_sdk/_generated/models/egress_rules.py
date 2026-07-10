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






T = TypeVar("T", bound="EgressRules")



@_attrs_define
class EgressRules:
    """ Allow/deny rules for sandbox outbound traffic.

        Attributes:
            allowed_domains (list[str] | Unset): Permit egress to matching hostnames. Exact ('pypi.org'), wildcard-all
                ('*'), or suffix ('*.pythonhosted.org'). Matched via TLS SNI (443) / HTTP Host (80).
            allowed_cid_rs (list[str] | Unset): Permit egress to these CIDR blocks / bare IPs (promoted to /32).
            denied_cid_rs (list[str] | Unset): Block egress to these CIDR blocks / bare IPs. Domains are not supported for
                deny.
     """

    allowed_domains: list[str] | Unset = UNSET
    allowed_cid_rs: list[str] | Unset = UNSET
    denied_cid_rs: list[str] | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        allowed_domains: list[str] | Unset = UNSET
        if not isinstance(self.allowed_domains, Unset):
            allowed_domains = self.allowed_domains



        allowed_cid_rs: list[str] | Unset = UNSET
        if not isinstance(self.allowed_cid_rs, Unset):
            allowed_cid_rs = self.allowed_cid_rs



        denied_cid_rs: list[str] | Unset = UNSET
        if not isinstance(self.denied_cid_rs, Unset):
            denied_cid_rs = self.denied_cid_rs




        field_dict: dict[str, Any] = {}

        field_dict.update({
        })
        if allowed_domains is not UNSET:
            field_dict["allowedDomains"] = allowed_domains
        if allowed_cid_rs is not UNSET:
            field_dict["allowedCIDRs"] = allowed_cid_rs
        if denied_cid_rs is not UNSET:
            field_dict["deniedCIDRs"] = denied_cid_rs

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        allowed_domains = cast(list[str], d.pop("allowedDomains", UNSET))


        allowed_cid_rs = cast(list[str], d.pop("allowedCIDRs", UNSET))


        denied_cid_rs = cast(list[str], d.pop("deniedCIDRs", UNSET))


        egress_rules = cls(
            allowed_domains=allowed_domains,
            allowed_cid_rs=allowed_cid_rs,
            denied_cid_rs=denied_cid_rs,
        )

        return egress_rules

