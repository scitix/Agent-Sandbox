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
  from ..models.sandbox_pool_statistics_by_namespace import SandboxPoolStatisticsByNamespace





T = TypeVar("T", bound="SandboxPoolStatistics")



@_attrs_define
class SandboxPoolStatistics:
    """ 
        Attributes:
            total (int): Total number of SandboxPools across all namespaces.
            total_replicas (int): Total pod replica count across all pools.
            total_idle_replicas (int): Total number of idle pod replicas across all pools.
            total_running_replicas (int): Total number of running (sandbox-serving) pod replicas across all pools.
            total_failed_replicas (int): Total number of failed pod replicas across all pools.
            by_namespace (SandboxPoolStatisticsByNamespace): Pool count broken down by Kubernetes namespace.
     """

    total: int
    total_replicas: int
    total_idle_replicas: int
    total_running_replicas: int
    total_failed_replicas: int
    by_namespace: SandboxPoolStatisticsByNamespace
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.sandbox_pool_statistics_by_namespace import SandboxPoolStatisticsByNamespace
        total = self.total

        total_replicas = self.total_replicas

        total_idle_replicas = self.total_idle_replicas

        total_running_replicas = self.total_running_replicas

        total_failed_replicas = self.total_failed_replicas

        by_namespace = self.by_namespace.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "total": total,
            "totalReplicas": total_replicas,
            "totalIdleReplicas": total_idle_replicas,
            "totalRunningReplicas": total_running_replicas,
            "totalFailedReplicas": total_failed_replicas,
            "byNamespace": by_namespace,
        })

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.sandbox_pool_statistics_by_namespace import SandboxPoolStatisticsByNamespace
        d = dict(src_dict)
        total = d.pop("total")

        total_replicas = d.pop("totalReplicas")

        total_idle_replicas = d.pop("totalIdleReplicas")

        total_running_replicas = d.pop("totalRunningReplicas")

        total_failed_replicas = d.pop("totalFailedReplicas")

        by_namespace = SandboxPoolStatisticsByNamespace.from_dict(d.pop("byNamespace"))




        sandbox_pool_statistics = cls(
            total=total,
            total_replicas=total_replicas,
            total_idle_replicas=total_idle_replicas,
            total_running_replicas=total_running_replicas,
            total_failed_replicas=total_failed_replicas,
            by_namespace=by_namespace,
        )


        sandbox_pool_statistics.additional_properties = d
        return sandbox_pool_statistics

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
