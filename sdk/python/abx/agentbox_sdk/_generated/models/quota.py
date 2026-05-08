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
  from ..models.quota_free import QuotaFree
  from ..models.quota_reserved import QuotaReserved
  from ..models.quota_resources import QuotaResources
  from ..models.quota_used import QuotaUsed





T = TypeVar("T", bound="Quota")



@_attrs_define
class Quota:
    """ 
        Attributes:
            name (str): Name of the quota entry.
            quota_url (str): URL of the SI Scheduler quota resource.
            queue (str): SI Scheduler queue name associated with this quota.
            label (str): Display label for this quota entry.
            pool_name (str | Unset): Name of the SandboxPool bound to this quota, if any.
            team (str | Unset): Team that owns this quota.
            user (str | Unset): User that owns this quota.
            resources (QuotaResources | Unset): Total resource capacity for this quota, keyed by resource name (e.g. cpu,
                memory, nvidia.com/gpu).
            used (QuotaUsed | Unset): Currently consumed resources, keyed by resource name.
            reserved (QuotaReserved | Unset): Resources reserved but not yet actively used, keyed by resource name.
            free (QuotaFree | Unset): Available (unreserved) resources, keyed by resource name.
     """

    name: str
    quota_url: str
    queue: str
    label: str
    pool_name: str | Unset = UNSET
    team: str | Unset = UNSET
    user: str | Unset = UNSET
    resources: QuotaResources | Unset = UNSET
    used: QuotaUsed | Unset = UNSET
    reserved: QuotaReserved | Unset = UNSET
    free: QuotaFree | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.quota_free import QuotaFree
        from ..models.quota_reserved import QuotaReserved
        from ..models.quota_resources import QuotaResources
        from ..models.quota_used import QuotaUsed
        name = self.name

        quota_url = self.quota_url

        queue = self.queue

        label = self.label

        pool_name = self.pool_name

        team = self.team

        user = self.user

        resources: dict[str, Any] | Unset = UNSET
        if not isinstance(self.resources, Unset):
            resources = self.resources.to_dict()

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
            "name": name,
            "quotaUrl": quota_url,
            "queue": queue,
            "label": label,
        })
        if pool_name is not UNSET:
            field_dict["poolName"] = pool_name
        if team is not UNSET:
            field_dict["team"] = team
        if user is not UNSET:
            field_dict["user"] = user
        if resources is not UNSET:
            field_dict["resources"] = resources
        if used is not UNSET:
            field_dict["used"] = used
        if reserved is not UNSET:
            field_dict["reserved"] = reserved
        if free is not UNSET:
            field_dict["free"] = free

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.quota_free import QuotaFree
        from ..models.quota_reserved import QuotaReserved
        from ..models.quota_resources import QuotaResources
        from ..models.quota_used import QuotaUsed
        d = dict(src_dict)
        name = d.pop("name")

        quota_url = d.pop("quotaUrl")

        queue = d.pop("queue")

        label = d.pop("label")

        pool_name = d.pop("poolName", UNSET)

        team = d.pop("team", UNSET)

        user = d.pop("user", UNSET)

        _resources = d.pop("resources", UNSET)
        resources: QuotaResources | Unset
        if isinstance(_resources,  Unset):
            resources = UNSET
        else:
            resources = QuotaResources.from_dict(_resources)




        _used = d.pop("used", UNSET)
        used: QuotaUsed | Unset
        if isinstance(_used,  Unset):
            used = UNSET
        else:
            used = QuotaUsed.from_dict(_used)




        _reserved = d.pop("reserved", UNSET)
        reserved: QuotaReserved | Unset
        if isinstance(_reserved,  Unset):
            reserved = UNSET
        else:
            reserved = QuotaReserved.from_dict(_reserved)




        _free = d.pop("free", UNSET)
        free: QuotaFree | Unset
        if isinstance(_free,  Unset):
            free = UNSET
        else:
            free = QuotaFree.from_dict(_free)




        quota = cls(
            name=name,
            quota_url=quota_url,
            queue=queue,
            label=label,
            pool_name=pool_name,
            team=team,
            user=user,
            resources=resources,
            used=used,
            reserved=reserved,
            free=free,
        )


        quota.additional_properties = d
        return quota

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
