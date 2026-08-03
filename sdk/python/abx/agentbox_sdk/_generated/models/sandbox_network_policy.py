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
  from ..models.egress_rules import EgressRules
  from ..models.secret_injection import SecretInjection





T = TypeVar("T", bound="SandboxNetworkPolicy")



@_attrs_define
class SandboxNetworkPolicy:
    """ Sandbox egress network policy, enforced by an in-Pod transparent proxy sidecar (supports domain matching, which the
    cluster CNIs cannot). Allowlist / default-deny semantics.

        Attributes:
            disable_egress (bool | Unset): Block all outbound traffic (DNS still resolves). A quick 'no internet' switch;
                takes precedence over egress.
            egress (EgressRules | Unset): Allow/deny rules for sandbox outbound traffic.
            allow_private_networks (bool | Unset): Disable the default deny of private / link-local / cloud-metadata ranges
                (RFC1918, 169.254.0.0/16, ...). Default false — the anti-SSRF baseline stays on.
            secret_injection (SecretInjection | Unset): Credential injection applied to matching outbound requests. Never
                carries a credential value: values live in Secrets and are resolved by the operator at push time.
     """

    disable_egress: bool | Unset = UNSET
    egress: EgressRules | Unset = UNSET
    allow_private_networks: bool | Unset = UNSET
    secret_injection: SecretInjection | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        from ..models.egress_rules import EgressRules
        from ..models.secret_injection import SecretInjection
        disable_egress = self.disable_egress

        egress: dict[str, Any] | Unset = UNSET
        if not isinstance(self.egress, Unset):
            egress = self.egress.to_dict()

        allow_private_networks = self.allow_private_networks

        secret_injection: dict[str, Any] | Unset = UNSET
        if not isinstance(self.secret_injection, Unset):
            secret_injection = self.secret_injection.to_dict()


        field_dict: dict[str, Any] = {}

        field_dict.update({
        })
        if disable_egress is not UNSET:
            field_dict["disableEgress"] = disable_egress
        if egress is not UNSET:
            field_dict["egress"] = egress
        if allow_private_networks is not UNSET:
            field_dict["allowPrivateNetworks"] = allow_private_networks
        if secret_injection is not UNSET:
            field_dict["secretInjection"] = secret_injection

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.egress_rules import EgressRules
        from ..models.secret_injection import SecretInjection
        d = dict(src_dict)
        disable_egress = d.pop("disableEgress", UNSET)

        _egress = d.pop("egress", UNSET)
        egress: EgressRules | Unset
        if isinstance(_egress,  Unset):
            egress = UNSET
        else:
            egress = EgressRules.from_dict(_egress)




        allow_private_networks = d.pop("allowPrivateNetworks", UNSET)

        _secret_injection = d.pop("secretInjection", UNSET)
        secret_injection: SecretInjection | Unset
        if isinstance(_secret_injection,  Unset):
            secret_injection = UNSET
        else:
            secret_injection = SecretInjection.from_dict(_secret_injection)




        sandbox_network_policy = cls(
            disable_egress=disable_egress,
            egress=egress,
            allow_private_networks=allow_private_networks,
            secret_injection=secret_injection,
        )

        return sandbox_network_policy

