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
  from ..models.quota_metadata import QuotaMetadata
  from ..models.quota_resources import QuotaResources





T = TypeVar("T", bound="Quota")



@_attrs_define
class Quota:
    """ 
        Attributes:
            id (str): Stable identifier for this quota. Used by clients to reference the quota in subsequent API calls (e.g.
                as the reservation target). Opaque from the caller's perspective.
            name (str): Human-readable display name for this quota. Suitable for showing in UI; not guaranteed to be unique
                across providers.
            team (str | Unset): Team that owns this quota, if applicable.
            user (str | Unset): User that owns this quota, if applicable.
            resources (QuotaResources | Unset): Resource accounting for a single quota, keyed by resource name (e.g. cpu,
                memory, nvidia.com/gpu, sci.c22-2).
            metadata (QuotaMetadata | Unset): Provider-attached display hints, opaque to the core schema. Keys are provider-
                defined (e.g. a vendor-prefixed pool name/type); generic clients ignore unknown keys.
     """

    id: str
    name: str
    team: str | Unset = UNSET
    user: str | Unset = UNSET
    resources: QuotaResources | Unset = UNSET
    metadata: QuotaMetadata | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.quota_metadata import QuotaMetadata
        from ..models.quota_resources import QuotaResources
        id = self.id

        name = self.name

        team = self.team

        user = self.user

        resources: dict[str, Any] | Unset = UNSET
        if not isinstance(self.resources, Unset):
            resources = self.resources.to_dict()

        metadata: dict[str, Any] | Unset = UNSET
        if not isinstance(self.metadata, Unset):
            metadata = self.metadata.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "id": id,
            "name": name,
        })
        if team is not UNSET:
            field_dict["team"] = team
        if user is not UNSET:
            field_dict["user"] = user
        if resources is not UNSET:
            field_dict["resources"] = resources
        if metadata is not UNSET:
            field_dict["metadata"] = metadata

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.quota_metadata import QuotaMetadata
        from ..models.quota_resources import QuotaResources
        d = dict(src_dict)
        id = d.pop("id")

        name = d.pop("name")

        team = d.pop("team", UNSET)

        user = d.pop("user", UNSET)

        _resources = d.pop("resources", UNSET)
        resources: QuotaResources | Unset
        if isinstance(_resources,  Unset):
            resources = UNSET
        else:
            resources = QuotaResources.from_dict(_resources)




        _metadata = d.pop("metadata", UNSET)
        metadata: QuotaMetadata | Unset
        if isinstance(_metadata,  Unset):
            metadata = UNSET
        else:
            metadata = QuotaMetadata.from_dict(_metadata)




        quota = cls(
            id=id,
            name=name,
            team=team,
            user=user,
            resources=resources,
            metadata=metadata,
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
