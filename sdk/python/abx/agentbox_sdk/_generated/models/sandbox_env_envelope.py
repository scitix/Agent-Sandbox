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
  from ..models.sandbox_env import SandboxEnv
  from ..models.upsert_sandbox_env_request import UpsertSandboxEnvRequest





T = TypeVar("T", bound="SandboxEnvEnvelope")



@_attrs_define
class SandboxEnvEnvelope:
    """ 
        Attributes:
            env (SandboxEnv):
            editable (UpsertSandboxEnvRequest | Unset): What a client may set on an Env — the SAME body for create and
                update,
                and the body `GET /envs/{name}` returns as `editable`.

                One shape rather than two subsets, because two subsets drift: while
                create accepted `mode` and update did not, a person editing an Env had
                no way to send back what the API had just handed them, and the file a
                client exported from a read was not a file a write accepted.

                Fields marked `x-immutable` are fixed at create. An update must carry
                them UNCHANGED: leaving one out, or sending a different value, is a 400
                that names the value in force rather than a silent ignore — a body that
                drops the template is far more likely to be a bug in the caller than a
                request to have no template.
     """

    env: SandboxEnv
    editable: UpsertSandboxEnvRequest | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.sandbox_env import SandboxEnv # noqa: PLC0415
        from ..models.upsert_sandbox_env_request import UpsertSandboxEnvRequest # noqa: PLC0415
        env = self.env.to_dict()

        editable: dict[str, Any] | Unset = UNSET
        if not isinstance(self.editable, Unset):
            editable = self.editable.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "env": env,
        })
        if editable is not UNSET:
            field_dict["editable"] = editable

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.sandbox_env import SandboxEnv # noqa: PLC0415
        from ..models.upsert_sandbox_env_request import UpsertSandboxEnvRequest # noqa: PLC0415
        d = dict(src_dict)
        env = SandboxEnv.from_dict(d.pop("env"))




        _editable = d.pop("editable", UNSET)
        editable: UpsertSandboxEnvRequest | Unset
        if isinstance(_editable,  Unset):
            editable = UNSET
        else:
            editable = UpsertSandboxEnvRequest.from_dict(_editable)




        sandbox_env_envelope = cls(
            env=env,
            editable=editable,
        )


        sandbox_env_envelope.additional_properties = d
        return sandbox_env_envelope

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
