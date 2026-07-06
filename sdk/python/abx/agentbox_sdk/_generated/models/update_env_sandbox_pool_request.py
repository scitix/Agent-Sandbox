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






T = TypeVar("T", bound="UpdateEnvSandboxPoolRequest")



@_attrs_define
class UpdateEnvSandboxPoolRequest:
    """ Update a member SandboxPool. Resource shape, instanceType, labels and
    annotations are immutable post-create; this PUT only accepts replica
    adjustments. When the scalingGroup has autoscaling enabled (via
    env.spec.autoscaling.enabled + a matching group entry), only
    `maxReplicas` is accepted — `replicas` is owned by the autoscaler.

        Attributes:
            replicas (int | Unset): Initial / desired replica count. Rejected when this pool's scalingGroup has autoscaling
                enabled.
            min_replicas (int | Unset): Lower bound on this pool's replicas, enforced as a per-member scale-down floor by
                the Env autoscaler. Always accepted.
            max_replicas (int | Unset): Upper bound on this pool's replicas. Always accepted.
     """

    replicas: int | Unset = UNSET
    min_replicas: int | Unset = UNSET
    max_replicas: int | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        replicas = self.replicas

        min_replicas = self.min_replicas

        max_replicas = self.max_replicas


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

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        replicas = d.pop("replicas", UNSET)

        min_replicas = d.pop("minReplicas", UNSET)

        max_replicas = d.pop("maxReplicas", UNSET)

        update_env_sandbox_pool_request = cls(
            replicas=replicas,
            min_replicas=min_replicas,
            max_replicas=max_replicas,
        )


        update_env_sandbox_pool_request.additional_properties = d
        return update_env_sandbox_pool_request

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
