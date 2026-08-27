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
  from ..models.volume_item_labels import VolumeItemLabels





T = TypeVar("T", bound="VolumeItem")



@_attrs_define
class VolumeItem:
    """ One PersistentVolumeClaim in the caller's namespace that a SandboxEnv may mount.

        Attributes:
            claim_name (str): The claim's name — the value to put in `overrides.volumes[].claimName`.
            phase (str): Claim phase. Only Bound claims are returned, so this is always 'Bound' today; present so a future
                filter relaxation stays legible.
            display_name (str | Unset): Human-readable name, resolved from the first non-empty label listed in the
                deployment's `--volume-display-name-labels`. Falls back to claimName when no label matches or none is
                configured, so this is always safe to render directly.
            capacity (str | Unset): Requested capacity, e.g. '5Ti'. Absent when the claim reports none.
            access_modes (list[str] | Unset): The claim's access modes, e.g. ['ReadWriteMany'].
            storage_class (str | Unset): The claim's storageClassName, when set.
            labels (VolumeItemLabels | Unset): The claim's labels, verbatim. Returned so a caller can apply its own
                presentation rules without this API having to know the storage platform's label vocabulary.
     """

    claim_name: str
    phase: str
    display_name: str | Unset = UNSET
    capacity: str | Unset = UNSET
    access_modes: list[str] | Unset = UNSET
    storage_class: str | Unset = UNSET
    labels: VolumeItemLabels | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        from ..models.volume_item_labels import VolumeItemLabels
        claim_name = self.claim_name

        phase = self.phase

        display_name = self.display_name

        capacity = self.capacity

        access_modes: list[str] | Unset = UNSET
        if not isinstance(self.access_modes, Unset):
            access_modes = self.access_modes



        storage_class = self.storage_class

        labels: dict[str, Any] | Unset = UNSET
        if not isinstance(self.labels, Unset):
            labels = self.labels.to_dict()


        field_dict: dict[str, Any] = {}

        field_dict.update({
            "claimName": claim_name,
            "phase": phase,
        })
        if display_name is not UNSET:
            field_dict["displayName"] = display_name
        if capacity is not UNSET:
            field_dict["capacity"] = capacity
        if access_modes is not UNSET:
            field_dict["accessModes"] = access_modes
        if storage_class is not UNSET:
            field_dict["storageClass"] = storage_class
        if labels is not UNSET:
            field_dict["labels"] = labels

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.volume_item_labels import VolumeItemLabels
        d = dict(src_dict)
        claim_name = d.pop("claimName")

        phase = d.pop("phase")

        display_name = d.pop("displayName", UNSET)

        capacity = d.pop("capacity", UNSET)

        access_modes = cast(list[str], d.pop("accessModes", UNSET))


        storage_class = d.pop("storageClass", UNSET)

        _labels = d.pop("labels", UNSET)
        labels: VolumeItemLabels | Unset
        if isinstance(_labels,  Unset):
            labels = UNSET
        else:
            labels = VolumeItemLabels.from_dict(_labels)




        volume_item = cls(
            claim_name=claim_name,
            phase=phase,
            display_name=display_name,
            capacity=capacity,
            access_modes=access_modes,
            storage_class=storage_class,
            labels=labels,
        )

        return volume_item

