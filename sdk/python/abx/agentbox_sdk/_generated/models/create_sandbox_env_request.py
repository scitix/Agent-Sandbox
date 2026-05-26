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

from ..models.create_sandbox_env_request_mode import CreateSandboxEnvRequestMode
from ..types import UNSET, Unset
from typing import cast

if TYPE_CHECKING:
  from ..models.create_sandbox_env_request_annotations import CreateSandboxEnvRequestAnnotations
  from ..models.create_sandbox_env_request_labels import CreateSandboxEnvRequestLabels
  from ..models.env_overrides import EnvOverrides
  from ..models.sandbox_env_template_ref import SandboxEnvTemplateRef





T = TypeVar("T", bound="CreateSandboxEnvRequest")



@_attrs_define
class CreateSandboxEnvRequest:
    """ 
        Attributes:
            name (str): RFC 1123 DNS label. Capped at 24 chars so derived names (PoolName = EnvName + ResourceKey +
                QuotaShort, PodName = PoolName + UUID) stay under the 63-char label/DNS limit.
            template_ref (SandboxEnvTemplateRef):
            mode (CreateSandboxEnvRequestMode | Unset):  Default: CreateSandboxEnvRequestMode.WARMPOOL.
            overrides (EnvOverrides | Unset): SandboxTemplate fields this Env replaces uniformly for every member Pool. The
                Env represents a single class of sandbox runtime, so image, image policy, default timeouts and image-pull
                credentials are expected to be shared; per-Pool variation lives on each EnvClusterMember.
            labels (CreateSandboxEnvRequestLabels | Unset):
            annotations (CreateSandboxEnvRequestAnnotations | Unset):
     """

    name: str
    template_ref: SandboxEnvTemplateRef
    mode: CreateSandboxEnvRequestMode | Unset = CreateSandboxEnvRequestMode.WARMPOOL
    overrides: EnvOverrides | Unset = UNSET
    labels: CreateSandboxEnvRequestLabels | Unset = UNSET
    annotations: CreateSandboxEnvRequestAnnotations | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.create_sandbox_env_request_annotations import CreateSandboxEnvRequestAnnotations
        from ..models.create_sandbox_env_request_labels import CreateSandboxEnvRequestLabels
        from ..models.env_overrides import EnvOverrides
        from ..models.sandbox_env_template_ref import SandboxEnvTemplateRef
        name = self.name

        template_ref = self.template_ref.to_dict()

        mode: str | Unset = UNSET
        if not isinstance(self.mode, Unset):
            mode = self.mode.value


        overrides: dict[str, Any] | Unset = UNSET
        if not isinstance(self.overrides, Unset):
            overrides = self.overrides.to_dict()

        labels: dict[str, Any] | Unset = UNSET
        if not isinstance(self.labels, Unset):
            labels = self.labels.to_dict()

        annotations: dict[str, Any] | Unset = UNSET
        if not isinstance(self.annotations, Unset):
            annotations = self.annotations.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "name": name,
            "templateRef": template_ref,
        })
        if mode is not UNSET:
            field_dict["mode"] = mode
        if overrides is not UNSET:
            field_dict["overrides"] = overrides
        if labels is not UNSET:
            field_dict["labels"] = labels
        if annotations is not UNSET:
            field_dict["annotations"] = annotations

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.create_sandbox_env_request_annotations import CreateSandboxEnvRequestAnnotations
        from ..models.create_sandbox_env_request_labels import CreateSandboxEnvRequestLabels
        from ..models.env_overrides import EnvOverrides
        from ..models.sandbox_env_template_ref import SandboxEnvTemplateRef
        d = dict(src_dict)
        name = d.pop("name")

        template_ref = SandboxEnvTemplateRef.from_dict(d.pop("templateRef"))




        _mode = d.pop("mode", UNSET)
        mode: CreateSandboxEnvRequestMode | Unset
        if isinstance(_mode,  Unset):
            mode = UNSET
        else:
            mode = CreateSandboxEnvRequestMode(_mode)




        _overrides = d.pop("overrides", UNSET)
        overrides: EnvOverrides | Unset
        if isinstance(_overrides,  Unset):
            overrides = UNSET
        else:
            overrides = EnvOverrides.from_dict(_overrides)




        _labels = d.pop("labels", UNSET)
        labels: CreateSandboxEnvRequestLabels | Unset
        if isinstance(_labels,  Unset):
            labels = UNSET
        else:
            labels = CreateSandboxEnvRequestLabels.from_dict(_labels)




        _annotations = d.pop("annotations", UNSET)
        annotations: CreateSandboxEnvRequestAnnotations | Unset
        if isinstance(_annotations,  Unset):
            annotations = UNSET
        else:
            annotations = CreateSandboxEnvRequestAnnotations.from_dict(_annotations)




        create_sandbox_env_request = cls(
            name=name,
            template_ref=template_ref,
            mode=mode,
            overrides=overrides,
            labels=labels,
            annotations=annotations,
        )


        create_sandbox_env_request.additional_properties = d
        return create_sandbox_env_request

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
