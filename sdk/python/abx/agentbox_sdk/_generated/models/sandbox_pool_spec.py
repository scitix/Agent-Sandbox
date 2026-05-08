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

from ..models.sandbox_pool_spec_pod_creation_image_policy import SandboxPoolSpecPodCreationImagePolicy
from ..types import UNSET, Unset
from typing import cast

if TYPE_CHECKING:
  from ..models.pool_autoscaling_spec import PoolAutoscalingSpec





T = TypeVar("T", bound="SandboxPoolSpec")



@_attrs_define
class SandboxPoolSpec:
    """ 
        Attributes:
            replicas (int): Desired number of pre-warmed idle pods in the pool.
            min_replicas (int | Unset): Minimum number of replicas when auto-scaling is enabled.
            max_replicas (int | Unset): Maximum number of replicas when auto-scaling is enabled.
            template_name (str | Unset): Name of the SandboxTemplate cluster resource to use as the pod spec source.
            autoscaling (PoolAutoscalingSpec | Unset): Autoscaling configuration for a SandboxPool. When nil or
                enabled=false, spec.replicas is the only source of truth.
            pod_creation_image_policy (SandboxPoolSpecPodCreationImagePolicy | Unset): Controls which image newly created
                Pods start with. IdleImage (default) uses spec.idleImage; PoolDefaultImage uses the template container image.
                Default: SandboxPoolSpecPodCreationImagePolicy.IDLEIMAGE.
            default_startup_timeout (str | Unset): Default startup timeout for sandbox create requests in this pool when the
                request does not specify startupTimeout. Also serves as the upper bound for the Starting phase: the controller
                deletes pods stuck in Starting for longer than this value. If not set, the internal default (2m) is used for
                create requests; no cleanup is enforced by default. Duration string, e.g. '5m'.
            default_idle_timeout (str | Unset): Default idle timeout for sandboxes created in this pool. Applied when a
                CreateSandbox request does not specify idleTimeout. If not set, sandboxes have no idle timeout by default.
                Duration string, e.g. '30m'.
     """

    replicas: int
    min_replicas: int | Unset = UNSET
    max_replicas: int | Unset = UNSET
    template_name: str | Unset = UNSET
    autoscaling: PoolAutoscalingSpec | Unset = UNSET
    pod_creation_image_policy: SandboxPoolSpecPodCreationImagePolicy | Unset = SandboxPoolSpecPodCreationImagePolicy.IDLEIMAGE
    default_startup_timeout: str | Unset = UNSET
    default_idle_timeout: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.pool_autoscaling_spec import PoolAutoscalingSpec
        replicas = self.replicas

        min_replicas = self.min_replicas

        max_replicas = self.max_replicas

        template_name = self.template_name

        autoscaling: dict[str, Any] | Unset = UNSET
        if not isinstance(self.autoscaling, Unset):
            autoscaling = self.autoscaling.to_dict()

        pod_creation_image_policy: str | Unset = UNSET
        if not isinstance(self.pod_creation_image_policy, Unset):
            pod_creation_image_policy = self.pod_creation_image_policy.value


        default_startup_timeout = self.default_startup_timeout

        default_idle_timeout = self.default_idle_timeout


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "replicas": replicas,
        })
        if min_replicas is not UNSET:
            field_dict["minReplicas"] = min_replicas
        if max_replicas is not UNSET:
            field_dict["maxReplicas"] = max_replicas
        if template_name is not UNSET:
            field_dict["templateName"] = template_name
        if autoscaling is not UNSET:
            field_dict["autoscaling"] = autoscaling
        if pod_creation_image_policy is not UNSET:
            field_dict["podCreationImagePolicy"] = pod_creation_image_policy
        if default_startup_timeout is not UNSET:
            field_dict["defaultStartupTimeout"] = default_startup_timeout
        if default_idle_timeout is not UNSET:
            field_dict["defaultIdleTimeout"] = default_idle_timeout

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.pool_autoscaling_spec import PoolAutoscalingSpec
        d = dict(src_dict)
        replicas = d.pop("replicas")

        min_replicas = d.pop("minReplicas", UNSET)

        max_replicas = d.pop("maxReplicas", UNSET)

        template_name = d.pop("templateName", UNSET)

        _autoscaling = d.pop("autoscaling", UNSET)
        autoscaling: PoolAutoscalingSpec | Unset
        if isinstance(_autoscaling,  Unset):
            autoscaling = UNSET
        else:
            autoscaling = PoolAutoscalingSpec.from_dict(_autoscaling)




        _pod_creation_image_policy = d.pop("podCreationImagePolicy", UNSET)
        pod_creation_image_policy: SandboxPoolSpecPodCreationImagePolicy | Unset
        if isinstance(_pod_creation_image_policy,  Unset):
            pod_creation_image_policy = UNSET
        else:
            pod_creation_image_policy = SandboxPoolSpecPodCreationImagePolicy(_pod_creation_image_policy)




        default_startup_timeout = d.pop("defaultStartupTimeout", UNSET)

        default_idle_timeout = d.pop("defaultIdleTimeout", UNSET)

        sandbox_pool_spec = cls(
            replicas=replicas,
            min_replicas=min_replicas,
            max_replicas=max_replicas,
            template_name=template_name,
            autoscaling=autoscaling,
            pod_creation_image_policy=pod_creation_image_policy,
            default_startup_timeout=default_startup_timeout,
            default_idle_timeout=default_idle_timeout,
        )


        sandbox_pool_spec.additional_properties = d
        return sandbox_pool_spec

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
