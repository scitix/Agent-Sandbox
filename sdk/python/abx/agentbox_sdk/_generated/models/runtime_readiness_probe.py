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
  from ..models.runtime_readiness_probe_http_get import RuntimeReadinessProbeHttpGet





T = TypeVar("T", bound="RuntimeReadinessProbe")



@_attrs_define
class RuntimeReadinessProbe:
    """ Lightweight readiness probe — only httpGet is supported. Advanced fields (initialDelaySeconds, periodSeconds, etc.)
    use Kubernetes defaults.

        Attributes:
            http_get (RuntimeReadinessProbeHttpGet | Unset):
     """

    http_get: RuntimeReadinessProbeHttpGet | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        from ..models.runtime_readiness_probe_http_get import RuntimeReadinessProbeHttpGet
        http_get: dict[str, Any] | Unset = UNSET
        if not isinstance(self.http_get, Unset):
            http_get = self.http_get.to_dict()


        field_dict: dict[str, Any] = {}

        field_dict.update({
        })
        if http_get is not UNSET:
            field_dict["httpGet"] = http_get

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.runtime_readiness_probe_http_get import RuntimeReadinessProbeHttpGet
        d = dict(src_dict)
        _http_get = d.pop("httpGet", UNSET)
        http_get: RuntimeReadinessProbeHttpGet | Unset
        if isinstance(_http_get,  Unset):
            http_get = UNSET
        else:
            http_get = RuntimeReadinessProbeHttpGet.from_dict(_http_get)




        runtime_readiness_probe = cls(
            http_get=http_get,
        )

        return runtime_readiness_probe

