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

from ..models.upsert_sandbox_env_request_mode import UpsertSandboxEnvRequestMode
from ..types import UNSET, Unset
from typing import cast

if TYPE_CHECKING:
  from ..models.env_overrides import EnvOverrides
  from ..models.sandbox_env_template_ref import SandboxEnvTemplateRef
  from ..models.string_map import StringMap





T = TypeVar("T", bound="CreateSandboxEnvRequest")



@_attrs_define
class CreateSandboxEnvRequest:
    """ What `POST /envs` takes: the same body an update does, plus the one
    thing only a create can say — which name to make. `name` is required
    here and the `pattern` it has to match is on the property itself.

        Attributes:
            template_ref (SandboxEnvTemplateRef):
            name (str): The Env this file is about. `create` requires it; `update` takes the name from the address and
                refuses a file whose `name` says something else. It is part of the write body so that the file a create wrote is
                the file an update takes — one shape, sent to one address. RFC 1123 DNS label, capped at 24 chars so derived
                names (PoolName = EnvName + ResourceKey + QuotaShort, PodName = PoolName + UUID) stay under the 63-char
                label/DNS limit.
            mode (UpsertSandboxEnvRequestMode | Unset): WarmPool keeps idle Pods ready to claim. OnDemandJob creates a Pod
                per sandbox and tears it down after, trading start latency for holding no capacity between runs. Fixed after
                create. Default: UpsertSandboxEnvRequestMode.WARMPOOL.
            overrides (EnvOverrides | Unset): SandboxTemplate fields this Env replaces uniformly for every member Pool. The
                Env represents a single class of sandbox runtime, so image, image policy, default timeouts and image-pull
                credentials are expected to be shared; per-Pool variation lives on each EnvClusterMember.
            labels (StringMap | Unset): Free-form string key/value metadata (labels or annotations).
            annotations (StringMap | Unset): Free-form string key/value metadata (labels or annotations).
     """

    template_ref: SandboxEnvTemplateRef
    name: str
    mode: UpsertSandboxEnvRequestMode | Unset = UpsertSandboxEnvRequestMode.WARMPOOL
    overrides: EnvOverrides | Unset = UNSET
    labels: StringMap | Unset = UNSET
    annotations: StringMap | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.env_overrides import EnvOverrides # noqa: PLC0415
        from ..models.sandbox_env_template_ref import SandboxEnvTemplateRef # noqa: PLC0415
        from ..models.string_map import StringMap # noqa: PLC0415
        template_ref = self.template_ref.to_dict()

        name = self.name

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
            "templateRef": template_ref,
            "name": name,
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
        from ..models.env_overrides import EnvOverrides # noqa: PLC0415
        from ..models.sandbox_env_template_ref import SandboxEnvTemplateRef # noqa: PLC0415
        from ..models.string_map import StringMap # noqa: PLC0415
        d = dict(src_dict)
        template_ref = SandboxEnvTemplateRef.from_dict(d.pop("templateRef"))




        name = d.pop("name")

        _mode = d.pop("mode", UNSET)
        mode: UpsertSandboxEnvRequestMode | Unset
        if isinstance(_mode,  Unset):
            mode = UNSET
        else:
            mode = UpsertSandboxEnvRequestMode(_mode)




        _overrides = d.pop("overrides", UNSET)
        overrides: EnvOverrides | Unset
        if isinstance(_overrides,  Unset):
            overrides = UNSET
        else:
            overrides = EnvOverrides.from_dict(_overrides)




        _labels = d.pop("labels", UNSET)
        labels: StringMap | Unset
        if isinstance(_labels,  Unset):
            labels = UNSET
        else:
            labels = StringMap.from_dict(_labels)




        _annotations = d.pop("annotations", UNSET)
        annotations: StringMap | Unset
        if isinstance(_annotations,  Unset):
            annotations = UNSET
        else:
            annotations = StringMap.from_dict(_annotations)




        create_sandbox_env_request = cls(
            template_ref=template_ref,
            name=name,
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
