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
  from ..models.injected_credential import InjectedCredential
  from ..models.injection_rule import InjectionRule





T = TypeVar("T", bound="SecretInjection")



@_attrs_define
class SecretInjection:
    """ Credential injection applied to matching outbound requests. Never carries a credential value: values live in Secrets
    and are resolved by the operator at push time.

        Attributes:
            credentials (list[InjectedCredential] | Unset): Named credentials that rules may reference.
            rules (list[InjectionRule] | Unset): Per-host injection actions.
            ca_cert_ttl (str | Unset): Lifetime of the per-sandbox CA minted for TLS interception, as a Go duration ('24h').
                Defaults to 24h.
     """

    credentials: list[InjectedCredential] | Unset = UNSET
    rules: list[InjectionRule] | Unset = UNSET
    ca_cert_ttl: str | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        from ..models.injected_credential import InjectedCredential
        from ..models.injection_rule import InjectionRule
        credentials: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.credentials, Unset):
            credentials = []
            for credentials_item_data in self.credentials:
                credentials_item = credentials_item_data.to_dict()
                credentials.append(credentials_item)



        rules: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.rules, Unset):
            rules = []
            for rules_item_data in self.rules:
                rules_item = rules_item_data.to_dict()
                rules.append(rules_item)



        ca_cert_ttl = self.ca_cert_ttl


        field_dict: dict[str, Any] = {}

        field_dict.update({
        })
        if credentials is not UNSET:
            field_dict["credentials"] = credentials
        if rules is not UNSET:
            field_dict["rules"] = rules
        if ca_cert_ttl is not UNSET:
            field_dict["caCertTTL"] = ca_cert_ttl

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.injected_credential import InjectedCredential
        from ..models.injection_rule import InjectionRule
        d = dict(src_dict)
        _credentials = d.pop("credentials", UNSET)
        credentials: list[InjectedCredential] | Unset = UNSET
        if _credentials is not UNSET:
            credentials = []
            for credentials_item_data in _credentials:
                credentials_item = InjectedCredential.from_dict(credentials_item_data)



                credentials.append(credentials_item)


        _rules = d.pop("rules", UNSET)
        rules: list[InjectionRule] | Unset = UNSET
        if _rules is not UNSET:
            rules = []
            for rules_item_data in _rules:
                rules_item = InjectionRule.from_dict(rules_item_data)



                rules.append(rules_item)


        ca_cert_ttl = d.pop("caCertTTL", UNSET)

        secret_injection = cls(
            credentials=credentials,
            rules=rules,
            ca_cert_ttl=ca_cert_ttl,
        )

        return secret_injection

