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
  from ..models.sandbox_readiness_result_endpoints import SandboxReadinessResultEndpoints





T = TypeVar("T", bound="SandboxReadinessResult")



@_attrs_define
class SandboxReadinessResult:
    """ 
        Attributes:
            sandbox_id (str): Sandbox identifier. Single-cluster: bare UUID v7 (e.g.
                `5de15c92-8fb5-440f-a9ea-7f62f734f1b9`).
                Cross-cluster: `{clusterID}.{uuid}` composite (e.g. `cluster1.5de15c92-...`, dot-separated). NOT a strict RFC
                4122 UUID — treat as opaque.
                 Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.
            ready (bool): True when all configured readiness probes have passed.
            endpoints (SandboxReadinessResultEndpoints | Unset): Per-runtime readiness status, keyed by runtime name.
     """

    sandbox_id: str
    ready: bool
    endpoints: SandboxReadinessResultEndpoints | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.sandbox_readiness_result_endpoints import SandboxReadinessResultEndpoints
        sandbox_id = self.sandbox_id

        ready = self.ready

        endpoints: dict[str, Any] | Unset = UNSET
        if not isinstance(self.endpoints, Unset):
            endpoints = self.endpoints.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "sandboxId": sandbox_id,
            "ready": ready,
        })
        if endpoints is not UNSET:
            field_dict["endpoints"] = endpoints

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.sandbox_readiness_result_endpoints import SandboxReadinessResultEndpoints
        d = dict(src_dict)
        sandbox_id = d.pop("sandboxId")

        ready = d.pop("ready")

        _endpoints = d.pop("endpoints", UNSET)
        endpoints: SandboxReadinessResultEndpoints | Unset
        if isinstance(_endpoints,  Unset):
            endpoints = UNSET
        else:
            endpoints = SandboxReadinessResultEndpoints.from_dict(_endpoints)




        sandbox_readiness_result = cls(
            sandbox_id=sandbox_id,
            ready=ready,
            endpoints=endpoints,
        )


        sandbox_readiness_result.additional_properties = d
        return sandbox_readiness_result

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
