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

from typing import cast

if TYPE_CHECKING:
  from ..models.sandbox import Sandbox





T = TypeVar("T", bound="SandboxEnvelope")



@_attrs_define
class SandboxEnvelope:
    """ 
        Attributes:
            sandbox (Sandbox):  Example: {'sandboxId': '5de15c92-8fb5-440f-a9ea-7f62f734f1b9', 'namespace': 't-team-user',
                'poolName': 'poolname', 'podName': 'poolname-wxtfc', 'status': 'Completed', 'claimedAt': '2026-04-07T14:37:54Z',
                'startedAt': '2026-04-07T14:37:55Z', 'terminatedAt': '2026-04-07T14:38:10Z', 'recycledAt':
                '2026-04-07T14:38:11Z', 'durationSeconds': 15, 'cpu': '1', 'memory': '16Gi', 'team': 'team', 'user': 'user',
                'containerImages': {'sandbox': 'docker.io/project/name:tag'}}.
     """

    sandbox: Sandbox
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.sandbox import Sandbox
        sandbox = self.sandbox.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "sandbox": sandbox,
        })

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.sandbox import Sandbox
        d = dict(src_dict)
        sandbox = Sandbox.from_dict(d.pop("sandbox"))




        sandbox_envelope = cls(
            sandbox=sandbox,
        )


        sandbox_envelope.additional_properties = d
        return sandbox_envelope

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
