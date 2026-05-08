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

from typing import cast

if TYPE_CHECKING:
  from ..models.sandbox_statistics_by_namespace import SandboxStatisticsByNamespace
  from ..models.sandbox_statistics_by_status import SandboxStatisticsByStatus





T = TypeVar("T", bound="SandboxStatistics")



@_attrs_define
class SandboxStatistics:
    """ 
        Attributes:
            total (int): Total number of sandboxes across all namespaces.
            by_status (SandboxStatisticsByStatus): Sandbox count broken down by status (e.g. Running, Failed).
            by_namespace (SandboxStatisticsByNamespace): Sandbox count broken down by Kubernetes namespace.
     """

    total: int
    by_status: SandboxStatisticsByStatus
    by_namespace: SandboxStatisticsByNamespace
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.sandbox_statistics_by_namespace import SandboxStatisticsByNamespace
        from ..models.sandbox_statistics_by_status import SandboxStatisticsByStatus
        total = self.total

        by_status = self.by_status.to_dict()

        by_namespace = self.by_namespace.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "total": total,
            "byStatus": by_status,
            "byNamespace": by_namespace,
        })

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.sandbox_statistics_by_namespace import SandboxStatisticsByNamespace
        from ..models.sandbox_statistics_by_status import SandboxStatisticsByStatus
        d = dict(src_dict)
        total = d.pop("total")

        by_status = SandboxStatisticsByStatus.from_dict(d.pop("byStatus"))




        by_namespace = SandboxStatisticsByNamespace.from_dict(d.pop("byNamespace"))




        sandbox_statistics = cls(
            total=total,
            by_status=by_status,
            by_namespace=by_namespace,
        )


        sandbox_statistics.additional_properties = d
        return sandbox_statistics

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
