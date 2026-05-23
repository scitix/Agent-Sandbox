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
  from ..models.env_autoscaling_spec import EnvAutoscalingSpec





T = TypeVar("T", bound="UpdateSandboxEnvRequest")



@_attrs_define
class UpdateSandboxEnvRequest:
    """ Patch one or more editable fields. MVP only allows updating autoscaling; the rest of the Env spec is managed by the
    Phase 1 adopter.

        Attributes:
            autoscaling (EnvAutoscalingSpec | Unset):
     """

    autoscaling: EnvAutoscalingSpec | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.env_autoscaling_spec import EnvAutoscalingSpec
        autoscaling: dict[str, Any] | Unset = UNSET
        if not isinstance(self.autoscaling, Unset):
            autoscaling = self.autoscaling.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if autoscaling is not UNSET:
            field_dict["autoscaling"] = autoscaling

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.env_autoscaling_spec import EnvAutoscalingSpec
        d = dict(src_dict)
        _autoscaling = d.pop("autoscaling", UNSET)
        autoscaling: EnvAutoscalingSpec | Unset
        if isinstance(_autoscaling,  Unset):
            autoscaling = UNSET
        else:
            autoscaling = EnvAutoscalingSpec.from_dict(_autoscaling)




        update_sandbox_env_request = cls(
            autoscaling=autoscaling,
        )


        update_sandbox_env_request.additional_properties = d
        return update_sandbox_env_request

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
