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
  from ..models.env_overrides import EnvOverrides





T = TypeVar("T", bound="UpdateSandboxEnvRequest")



@_attrs_define
class UpdateSandboxEnvRequest:
    """ Patch one or more editable Env shell fields. Omitted fields are left unchanged. Members are managed through
    `/envs/{name}/sandboxpools/*` and autoscaling through `/envs/{name}/autoscaling/*`.

        Attributes:
            overrides (EnvOverrides | Unset): SandboxTemplate fields this Env replaces uniformly for every member Pool. The
                Env represents a single class of sandbox runtime, so image, image policy, default timeouts and image-pull
                credentials are expected to be shared; per-Pool variation lives on each EnvClusterMember.
     """

    overrides: EnvOverrides | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.env_overrides import EnvOverrides
        overrides: dict[str, Any] | Unset = UNSET
        if not isinstance(self.overrides, Unset):
            overrides = self.overrides.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if overrides is not UNSET:
            field_dict["overrides"] = overrides

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.env_overrides import EnvOverrides
        d = dict(src_dict)
        _overrides = d.pop("overrides", UNSET)
        overrides: EnvOverrides | Unset
        if isinstance(_overrides,  Unset):
            overrides = UNSET
        else:
            overrides = EnvOverrides.from_dict(_overrides)




        update_sandbox_env_request = cls(
            overrides=overrides,
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
