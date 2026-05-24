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
  from ..models.resource_requirements_limits import ResourceRequirementsLimits
  from ..models.resource_requirements_requests import ResourceRequirementsRequests





T = TypeVar("T", bound="ResourceRequirements")



@_attrs_define
class ResourceRequirements:
    """ Subset of Kubernetes corev1.ResourceRequirements used for per-Pool resource sizing on
    EnvClusterMember.inlineResources.

        Attributes:
            requests (ResourceRequirementsRequests | Unset): Resource requests keyed by resource name (e.g. cpu, memory).
                Values use Kubernetes Quantity strings, e.g. '500m', '1Gi'.
            limits (ResourceRequirementsLimits | Unset): Resource limits keyed by resource name.
     """

    requests: ResourceRequirementsRequests | Unset = UNSET
    limits: ResourceRequirementsLimits | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.resource_requirements_limits import ResourceRequirementsLimits
        from ..models.resource_requirements_requests import ResourceRequirementsRequests
        requests: dict[str, Any] | Unset = UNSET
        if not isinstance(self.requests, Unset):
            requests = self.requests.to_dict()

        limits: dict[str, Any] | Unset = UNSET
        if not isinstance(self.limits, Unset):
            limits = self.limits.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if requests is not UNSET:
            field_dict["requests"] = requests
        if limits is not UNSET:
            field_dict["limits"] = limits

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.resource_requirements_limits import ResourceRequirementsLimits
        from ..models.resource_requirements_requests import ResourceRequirementsRequests
        d = dict(src_dict)
        _requests = d.pop("requests", UNSET)
        requests: ResourceRequirementsRequests | Unset
        if isinstance(_requests,  Unset):
            requests = UNSET
        else:
            requests = ResourceRequirementsRequests.from_dict(_requests)




        _limits = d.pop("limits", UNSET)
        limits: ResourceRequirementsLimits | Unset
        if isinstance(_limits,  Unset):
            limits = UNSET
        else:
            limits = ResourceRequirementsLimits.from_dict(_limits)




        resource_requirements = cls(
            requests=requests,
            limits=limits,
        )


        resource_requirements.additional_properties = d
        return resource_requirements

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
