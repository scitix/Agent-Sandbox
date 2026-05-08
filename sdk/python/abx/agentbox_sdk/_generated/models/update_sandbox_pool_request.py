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

from ..models.update_sandbox_pool_request_pod_creation_image_policy import UpdateSandboxPoolRequestPodCreationImagePolicy
from ..types import UNSET, Unset
from typing import cast

if TYPE_CHECKING:
  from ..models.pool_autoscaling_spec import PoolAutoscalingSpec
  from ..models.update_sandbox_pool_request_overrides import UpdateSandboxPoolRequestOverrides





T = TypeVar("T", bound="UpdateSandboxPoolRequest")



@_attrs_define
class UpdateSandboxPoolRequest:
    """ 
        Attributes:
            replicas (int | Unset):
            min_replicas (int | Unset): New minimum replicas bound for the autoscaler. Omit to leave unchanged.
            max_replicas (int | Unset): New maximum replicas bound for the autoscaler. Omit to leave unchanged.
            pod_creation_image_policy (UpdateSandboxPoolRequestPodCreationImagePolicy | Unset): Update the pod creation
                image policy. Omit to leave unchanged.
            overrides (UpdateSandboxPoolRequestOverrides | Unset): Partial overrides to update. Only image can be updated
                after pool creation.
            autoscaling (PoolAutoscalingSpec | Unset): Autoscaling configuration for a SandboxPool. When nil or
                enabled=false, spec.replicas is the only source of truth.
     """

    replicas: int | Unset = UNSET
    min_replicas: int | Unset = UNSET
    max_replicas: int | Unset = UNSET
    pod_creation_image_policy: UpdateSandboxPoolRequestPodCreationImagePolicy | Unset = UNSET
    overrides: UpdateSandboxPoolRequestOverrides | Unset = UNSET
    autoscaling: PoolAutoscalingSpec | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.pool_autoscaling_spec import PoolAutoscalingSpec
        from ..models.update_sandbox_pool_request_overrides import UpdateSandboxPoolRequestOverrides
        replicas = self.replicas

        min_replicas = self.min_replicas

        max_replicas = self.max_replicas

        pod_creation_image_policy: str | Unset = UNSET
        if not isinstance(self.pod_creation_image_policy, Unset):
            pod_creation_image_policy = self.pod_creation_image_policy.value


        overrides: dict[str, Any] | Unset = UNSET
        if not isinstance(self.overrides, Unset):
            overrides = self.overrides.to_dict()

        autoscaling: dict[str, Any] | Unset = UNSET
        if not isinstance(self.autoscaling, Unset):
            autoscaling = self.autoscaling.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if replicas is not UNSET:
            field_dict["replicas"] = replicas
        if min_replicas is not UNSET:
            field_dict["minReplicas"] = min_replicas
        if max_replicas is not UNSET:
            field_dict["maxReplicas"] = max_replicas
        if pod_creation_image_policy is not UNSET:
            field_dict["podCreationImagePolicy"] = pod_creation_image_policy
        if overrides is not UNSET:
            field_dict["overrides"] = overrides
        if autoscaling is not UNSET:
            field_dict["autoscaling"] = autoscaling

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.pool_autoscaling_spec import PoolAutoscalingSpec
        from ..models.update_sandbox_pool_request_overrides import UpdateSandboxPoolRequestOverrides
        d = dict(src_dict)
        replicas = d.pop("replicas", UNSET)

        min_replicas = d.pop("minReplicas", UNSET)

        max_replicas = d.pop("maxReplicas", UNSET)

        _pod_creation_image_policy = d.pop("podCreationImagePolicy", UNSET)
        pod_creation_image_policy: UpdateSandboxPoolRequestPodCreationImagePolicy | Unset
        if isinstance(_pod_creation_image_policy,  Unset):
            pod_creation_image_policy = UNSET
        else:
            pod_creation_image_policy = UpdateSandboxPoolRequestPodCreationImagePolicy(_pod_creation_image_policy)




        _overrides = d.pop("overrides", UNSET)
        overrides: UpdateSandboxPoolRequestOverrides | Unset
        if isinstance(_overrides,  Unset):
            overrides = UNSET
        else:
            overrides = UpdateSandboxPoolRequestOverrides.from_dict(_overrides)




        _autoscaling = d.pop("autoscaling", UNSET)
        autoscaling: PoolAutoscalingSpec | Unset
        if isinstance(_autoscaling,  Unset):
            autoscaling = UNSET
        else:
            autoscaling = PoolAutoscalingSpec.from_dict(_autoscaling)




        update_sandbox_pool_request = cls(
            replicas=replicas,
            min_replicas=min_replicas,
            max_replicas=max_replicas,
            pod_creation_image_policy=pod_creation_image_policy,
            overrides=overrides,
            autoscaling=autoscaling,
        )


        update_sandbox_pool_request.additional_properties = d
        return update_sandbox_pool_request

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
