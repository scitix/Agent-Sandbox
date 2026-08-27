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






T = TypeVar("T", bound="FeatureGates")



@_attrs_define
class FeatureGates:
    """ Boolean switches that tell clients which optional features are wired into the current deployment. Dashboards gate
    feature UI on these values; SDKs can short-circuit feature-specific calls when a gate is false.

        Attributes:
            quota (bool): True when a non-noop quota provider is active (quota selection on pool creation, quota listing
                endpoint). False on deployments with no quota backend wired in. Example: True.
            instance_type (bool): True when a non-noop InstanceType catalog provider is active (catalog-driven member sizing
                in the Env upsert sheet, `/instancetypes` listing endpoint). False on deployments with no InstanceType backend
                wired in. Example: True.
            volumes (bool | Unset): True when mounting existing PersistentVolumeClaims into sandboxes is enabled (the
                volumes panel in the Env upsert sheet, `/volumes` listing endpoint). When false the server also rejects a non-
                empty `overrides.volumes`, so this is a kill switch and not only a UI hint.
     """

    quota: bool
    instance_type: bool
    volumes: bool | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        quota = self.quota

        instance_type = self.instance_type

        volumes = self.volumes


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "quota": quota,
            "instanceType": instance_type,
        })
        if volumes is not UNSET:
            field_dict["volumes"] = volumes

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        quota = d.pop("quota")

        instance_type = d.pop("instanceType")

        volumes = d.pop("volumes", UNSET)

        feature_gates = cls(
            quota=quota,
            instance_type=instance_type,
            volumes=volumes,
        )


        feature_gates.additional_properties = d
        return feature_gates

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
