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






T = TypeVar("T", bound="ClusterSummary")



@_attrs_define
class ClusterSummary:
    """ One cluster entry visible through the gateway's routing table.

        Attributes:
            id (str): Cluster identifier. Use this value as the prefix when addressing
                cross-cluster resources: `{id}.{uuid}` for sandboxes,
                `{id}::{poolName}` for pools.
                 Example: cluster1.
            local (bool): True when this entry is the cluster serving the current request —
                i.e. sandbox/pool identifiers without any cross-cluster prefix refer
                to this cluster.
                 Example: True.
            name (str | Unset): Human-readable display name (may equal id). Example: cluster1.
     """

    id: str
    local: bool
    name: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        id = self.id

        local = self.local

        name = self.name


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "id": id,
            "local": local,
        })
        if name is not UNSET:
            field_dict["name"] = name

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        id = d.pop("id")

        local = d.pop("local")

        name = d.pop("name", UNSET)

        cluster_summary = cls(
            id=id,
            local=local,
            name=name,
        )


        cluster_summary.additional_properties = d
        return cluster_summary

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
