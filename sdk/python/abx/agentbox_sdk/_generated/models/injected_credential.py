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
  from ..models.secret_key_ref import SecretKeyRef





T = TypeVar("T", bound="InjectedCredential")



@_attrs_define
class InjectedCredential:
    """ 
        Attributes:
            name (str): How rules refer to this credential in a value template ('{{ name }}'). Also the key under which the
                value is stored in the Env's credential Secret, so it must be a valid Secret key: alphanumeric plus - and _.
            value (str | Unset): The credential itself. Write-only: the server stores it in the Env's credential Secret (one
                Secret per Env, keyed by this credential's name), fills in valueFrom, and never returns it. Supply this OR
                valueFrom, not both. On update, omit it to keep the stored value unchanged.
            value_from (SecretKeyRef | Unset):
            expose_as (str | Unset): Environment variable name handed to the sandbox carrying the decoy value (placeholder
                mode). Omit to use this credential through header injection only.
            placeholder (str | Unset): Decoy value given to the sandbox. Omit for a fresh random 'agbx_ph_<32 hex>' per
                claim. Set it when a client validates credential shape before sending. Minimum 16 characters; placeholders must
                not overlap.
            value_digest (str | Unset): First 8 hex characters of the SHA-256 of the resolved credential, so callers can
                tell whether a value is configured or has changed. The value itself is never returned.
     """

    name: str
    value: str | Unset = UNSET
    value_from: SecretKeyRef | Unset = UNSET
    expose_as: str | Unset = UNSET
    placeholder: str | Unset = UNSET
    value_digest: str | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        from ..models.secret_key_ref import SecretKeyRef
        name = self.name

        value = self.value

        value_from: dict[str, Any] | Unset = UNSET
        if not isinstance(self.value_from, Unset):
            value_from = self.value_from.to_dict()

        expose_as = self.expose_as

        placeholder = self.placeholder

        value_digest = self.value_digest


        field_dict: dict[str, Any] = {}

        field_dict.update({
            "name": name,
        })
        if value is not UNSET:
            field_dict["value"] = value
        if value_from is not UNSET:
            field_dict["valueFrom"] = value_from
        if expose_as is not UNSET:
            field_dict["exposeAs"] = expose_as
        if placeholder is not UNSET:
            field_dict["placeholder"] = placeholder
        if value_digest is not UNSET:
            field_dict["valueDigest"] = value_digest

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.secret_key_ref import SecretKeyRef
        d = dict(src_dict)
        name = d.pop("name")

        value = d.pop("value", UNSET)

        _value_from = d.pop("valueFrom", UNSET)
        value_from: SecretKeyRef | Unset
        if isinstance(_value_from,  Unset):
            value_from = UNSET
        else:
            value_from = SecretKeyRef.from_dict(_value_from)




        expose_as = d.pop("exposeAs", UNSET)

        placeholder = d.pop("placeholder", UNSET)

        value_digest = d.pop("valueDigest", UNSET)

        injected_credential = cls(
            name=name,
            value=value,
            value_from=value_from,
            expose_as=expose_as,
            placeholder=placeholder,
            value_digest=value_digest,
        )

        return injected_credential

