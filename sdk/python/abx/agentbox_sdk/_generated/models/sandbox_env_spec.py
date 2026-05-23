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

from ..models.sandbox_env_spec_mode import SandboxEnvSpecMode
from ..types import UNSET, Unset
from typing import cast

if TYPE_CHECKING:
  from ..models.env_autoscaling_spec import EnvAutoscalingSpec
  from ..models.env_cluster_spec import EnvClusterSpec
  from ..models.sandbox_env_defaults import SandboxEnvDefaults
  from ..models.sandbox_env_template_ref import SandboxEnvTemplateRef





T = TypeVar("T", bound="SandboxEnvSpec")



@_attrs_define
class SandboxEnvSpec:
    """ 
        Attributes:
            template_ref (SandboxEnvTemplateRef):
            mode (SandboxEnvSpecMode): Routing mode — WarmPool dispatches to pre-warmed member Pools; OnDemandJob (Phase 3)
                creates a SandboxJob per request.
            defaults (SandboxEnvDefaults | Unset):
            clusters (list[EnvClusterSpec] | Unset):
            autoscaling (EnvAutoscalingSpec | Unset):
     """

    template_ref: SandboxEnvTemplateRef
    mode: SandboxEnvSpecMode
    defaults: SandboxEnvDefaults | Unset = UNSET
    clusters: list[EnvClusterSpec] | Unset = UNSET
    autoscaling: EnvAutoscalingSpec | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.env_autoscaling_spec import EnvAutoscalingSpec
        from ..models.env_cluster_spec import EnvClusterSpec
        from ..models.sandbox_env_defaults import SandboxEnvDefaults
        from ..models.sandbox_env_template_ref import SandboxEnvTemplateRef
        template_ref = self.template_ref.to_dict()

        mode = self.mode.value

        defaults: dict[str, Any] | Unset = UNSET
        if not isinstance(self.defaults, Unset):
            defaults = self.defaults.to_dict()

        clusters: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.clusters, Unset):
            clusters = []
            for clusters_item_data in self.clusters:
                clusters_item = clusters_item_data.to_dict()
                clusters.append(clusters_item)



        autoscaling: dict[str, Any] | Unset = UNSET
        if not isinstance(self.autoscaling, Unset):
            autoscaling = self.autoscaling.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "templateRef": template_ref,
            "mode": mode,
        })
        if defaults is not UNSET:
            field_dict["defaults"] = defaults
        if clusters is not UNSET:
            field_dict["clusters"] = clusters
        if autoscaling is not UNSET:
            field_dict["autoscaling"] = autoscaling

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.env_autoscaling_spec import EnvAutoscalingSpec
        from ..models.env_cluster_spec import EnvClusterSpec
        from ..models.sandbox_env_defaults import SandboxEnvDefaults
        from ..models.sandbox_env_template_ref import SandboxEnvTemplateRef
        d = dict(src_dict)
        template_ref = SandboxEnvTemplateRef.from_dict(d.pop("templateRef"))




        mode = SandboxEnvSpecMode(d.pop("mode"))




        _defaults = d.pop("defaults", UNSET)
        defaults: SandboxEnvDefaults | Unset
        if isinstance(_defaults,  Unset):
            defaults = UNSET
        else:
            defaults = SandboxEnvDefaults.from_dict(_defaults)




        _clusters = d.pop("clusters", UNSET)
        clusters: list[EnvClusterSpec] | Unset = UNSET
        if _clusters is not UNSET:
            clusters = []
            for clusters_item_data in _clusters:
                clusters_item = EnvClusterSpec.from_dict(clusters_item_data)



                clusters.append(clusters_item)


        _autoscaling = d.pop("autoscaling", UNSET)
        autoscaling: EnvAutoscalingSpec | Unset
        if isinstance(_autoscaling,  Unset):
            autoscaling = UNSET
        else:
            autoscaling = EnvAutoscalingSpec.from_dict(_autoscaling)




        sandbox_env_spec = cls(
            template_ref=template_ref,
            mode=mode,
            defaults=defaults,
            clusters=clusters,
            autoscaling=autoscaling,
        )


        sandbox_env_spec.additional_properties = d
        return sandbox_env_spec

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
