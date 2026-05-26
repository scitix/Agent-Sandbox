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
  from ..models.env_cluster_member_config import EnvClusterMemberConfig





T = TypeVar("T", bound="EnvClusterMember")



@_attrs_define
class EnvClusterMember:
    """ One SandboxPool participating in an Env. Identity is `name`; everything
    the caller can declare (sizing, scaling-group, routing priorities,
    user-supplied labels/annotations) lives under `config`. The
    materialised Pool's metadata + spec are server-internal state
    (captured after plugin admission ran) and are NOT exposed here — query
    the SandboxPool CR directly via `GET /v1/sandboxenvs/{name}/sandboxpools/{poolName}`
    to inspect the rendered Pool.

        Attributes:
            name (str): SandboxPool's metadata.name within the Env's namespace. Acts as the member identity within the Env.
            config (EnvClusterMemberConfig | Unset): User-declared intent for one Env member. Plugins do not mutate this —
                it stays equal to whatever the caller supplied at AddMember /
                UpdateMember time, so it remains a faithful description of the
                request shape across the member's lifetime.
     """

    name: str
    config: EnvClusterMemberConfig | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.env_cluster_member_config import EnvClusterMemberConfig
        name = self.name

        config: dict[str, Any] | Unset = UNSET
        if not isinstance(self.config, Unset):
            config = self.config.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "name": name,
        })
        if config is not UNSET:
            field_dict["config"] = config

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.env_cluster_member_config import EnvClusterMemberConfig
        d = dict(src_dict)
        name = d.pop("name")

        _config = d.pop("config", UNSET)
        config: EnvClusterMemberConfig | Unset
        if isinstance(_config,  Unset):
            config = UNSET
        else:
            config = EnvClusterMemberConfig.from_dict(_config)




        env_cluster_member = cls(
            name=name,
            config=config,
        )


        env_cluster_member.additional_properties = d
        return env_cluster_member

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
