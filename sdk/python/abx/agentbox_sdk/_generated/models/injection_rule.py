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
  from ..models.header_injection import HeaderInjection





T = TypeVar("T", bound="InjectionRule")



@_attrs_define
class InjectionRule:
    """ 
        Attributes:
            host (str): Exact hostname. Wildcards are rejected: anyone controlling a matching subdomain would receive the
                credential.
            ports (list[int] | Unset): Destination ports the rule covers. Defaults to [80, 443]; other ports get no L7
                handling.
            headers (list[HeaderInjection] | Unset): Headers to inject.
            substitute (list[str] | Unset): Credentials whose placeholder may be swapped for the real value on this host.
            path_prefixes (list[str] | Unset): Narrow the rule to matching request paths. Empty means all paths.
            methods (list[str] | Unset): Narrow the rule to these HTTP methods. Empty means all methods.
     """

    host: str
    ports: list[int] | Unset = UNSET
    headers: list[HeaderInjection] | Unset = UNSET
    substitute: list[str] | Unset = UNSET
    path_prefixes: list[str] | Unset = UNSET
    methods: list[str] | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        from ..models.header_injection import HeaderInjection
        host = self.host

        ports: list[int] | Unset = UNSET
        if not isinstance(self.ports, Unset):
            ports = self.ports



        headers: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.headers, Unset):
            headers = []
            for headers_item_data in self.headers:
                headers_item = headers_item_data.to_dict()
                headers.append(headers_item)



        substitute: list[str] | Unset = UNSET
        if not isinstance(self.substitute, Unset):
            substitute = self.substitute



        path_prefixes: list[str] | Unset = UNSET
        if not isinstance(self.path_prefixes, Unset):
            path_prefixes = self.path_prefixes



        methods: list[str] | Unset = UNSET
        if not isinstance(self.methods, Unset):
            methods = self.methods




        field_dict: dict[str, Any] = {}

        field_dict.update({
            "host": host,
        })
        if ports is not UNSET:
            field_dict["ports"] = ports
        if headers is not UNSET:
            field_dict["headers"] = headers
        if substitute is not UNSET:
            field_dict["substitute"] = substitute
        if path_prefixes is not UNSET:
            field_dict["pathPrefixes"] = path_prefixes
        if methods is not UNSET:
            field_dict["methods"] = methods

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.header_injection import HeaderInjection
        d = dict(src_dict)
        host = d.pop("host")

        ports = cast(list[int], d.pop("ports", UNSET))


        _headers = d.pop("headers", UNSET)
        headers: list[HeaderInjection] | Unset = UNSET
        if _headers is not UNSET:
            headers = []
            for headers_item_data in _headers:
                headers_item = HeaderInjection.from_dict(headers_item_data)



                headers.append(headers_item)


        substitute = cast(list[str], d.pop("substitute", UNSET))


        path_prefixes = cast(list[str], d.pop("pathPrefixes", UNSET))


        methods = cast(list[str], d.pop("methods", UNSET))


        injection_rule = cls(
            host=host,
            ports=ports,
            headers=headers,
            substitute=substitute,
            path_prefixes=path_prefixes,
            methods=methods,
        )

        return injection_rule

