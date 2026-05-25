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
  from ..models.quota_resources_free import QuotaResourcesFree
  from ..models.quota_resources_reserved import QuotaResourcesReserved
  from ..models.quota_resources_total import QuotaResourcesTotal
  from ..models.quota_resources_used import QuotaResourcesUsed





T = TypeVar("T", bound="QuotaResources")



@_attrs_define
class QuotaResources:
    """ Resource accounting for a single quota, keyed by resource name (e.g. cpu, memory, nvidia.com/gpu, sci.c22-2).

        Attributes:
            total (QuotaResourcesTotal | Unset): Total capacity allocated to this quota.
            used (QuotaResourcesUsed | Unset): Currently consumed amount.
            reserved (QuotaResourcesReserved | Unset): Reserved but not yet actively consumed amount.
            free (QuotaResourcesFree | Unset): Available amount (total - used - reserved).
     """

    total: QuotaResourcesTotal | Unset = UNSET
    used: QuotaResourcesUsed | Unset = UNSET
    reserved: QuotaResourcesReserved | Unset = UNSET
    free: QuotaResourcesFree | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.quota_resources_free import QuotaResourcesFree
        from ..models.quota_resources_reserved import QuotaResourcesReserved
        from ..models.quota_resources_total import QuotaResourcesTotal
        from ..models.quota_resources_used import QuotaResourcesUsed
        total: dict[str, Any] | Unset = UNSET
        if not isinstance(self.total, Unset):
            total = self.total.to_dict()

        used: dict[str, Any] | Unset = UNSET
        if not isinstance(self.used, Unset):
            used = self.used.to_dict()

        reserved: dict[str, Any] | Unset = UNSET
        if not isinstance(self.reserved, Unset):
            reserved = self.reserved.to_dict()

        free: dict[str, Any] | Unset = UNSET
        if not isinstance(self.free, Unset):
            free = self.free.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if total is not UNSET:
            field_dict["total"] = total
        if used is not UNSET:
            field_dict["used"] = used
        if reserved is not UNSET:
            field_dict["reserved"] = reserved
        if free is not UNSET:
            field_dict["free"] = free

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.quota_resources_free import QuotaResourcesFree
        from ..models.quota_resources_reserved import QuotaResourcesReserved
        from ..models.quota_resources_total import QuotaResourcesTotal
        from ..models.quota_resources_used import QuotaResourcesUsed
        d = dict(src_dict)
        _total = d.pop("total", UNSET)
        total: QuotaResourcesTotal | Unset
        if isinstance(_total,  Unset):
            total = UNSET
        else:
            total = QuotaResourcesTotal.from_dict(_total)




        _used = d.pop("used", UNSET)
        used: QuotaResourcesUsed | Unset
        if isinstance(_used,  Unset):
            used = UNSET
        else:
            used = QuotaResourcesUsed.from_dict(_used)




        _reserved = d.pop("reserved", UNSET)
        reserved: QuotaResourcesReserved | Unset
        if isinstance(_reserved,  Unset):
            reserved = UNSET
        else:
            reserved = QuotaResourcesReserved.from_dict(_reserved)




        _free = d.pop("free", UNSET)
        free: QuotaResourcesFree | Unset
        if isinstance(_free,  Unset):
            free = UNSET
        else:
            free = QuotaResourcesFree.from_dict(_free)




        quota_resources = cls(
            total=total,
            used=used,
            reserved=reserved,
            free=free,
        )


        quota_resources.additional_properties = d
        return quota_resources

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
