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
import datetime

if TYPE_CHECKING:
  from ..models.sandbox_env_labels import SandboxEnvLabels
  from ..models.sandbox_env_spec import SandboxEnvSpec
  from ..models.sandbox_env_status import SandboxEnvStatus





T = TypeVar("T", bound="SandboxEnv")



@_attrs_define
class SandboxEnv:
    """ 
        Attributes:
            name (str):
            namespace (str):
            spec (SandboxEnvSpec):
            labels (SandboxEnvLabels | Unset):
            status (SandboxEnvStatus | Unset):
            team (str | Unset):
            user (str | Unset):
            created_at (datetime.datetime | Unset):
            env_docs (str | Unset): Rendered Markdown documentation for this Env, sourced from the linked SandboxTemplate's
                agentbox.navix.sh/docs annotation. The substitutions ${AGBX_ENV_NAME}, ${AGBX_POOL_NAME} (renders the env name
                for backward compat), ${AGBX_CLUSTER_ID}, ${AGBX_API_KEY} are performed server-side before the response is
                returned. When the docs reference ${AGBX_API_KEY} but the caller has no key with a recoverable plaintext token,
                GetSandboxEnv returns 422 with errorCode API_KEY_REQUIRED.
     """

    name: str
    namespace: str
    spec: SandboxEnvSpec
    labels: SandboxEnvLabels | Unset = UNSET
    status: SandboxEnvStatus | Unset = UNSET
    team: str | Unset = UNSET
    user: str | Unset = UNSET
    created_at: datetime.datetime | Unset = UNSET
    env_docs: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.sandbox_env_labels import SandboxEnvLabels
        from ..models.sandbox_env_spec import SandboxEnvSpec
        from ..models.sandbox_env_status import SandboxEnvStatus
        name = self.name

        namespace = self.namespace

        spec = self.spec.to_dict()

        labels: dict[str, Any] | Unset = UNSET
        if not isinstance(self.labels, Unset):
            labels = self.labels.to_dict()

        status: dict[str, Any] | Unset = UNSET
        if not isinstance(self.status, Unset):
            status = self.status.to_dict()

        team = self.team

        user = self.user

        created_at: str | Unset = UNSET
        if not isinstance(self.created_at, Unset):
            created_at = self.created_at.isoformat()

        env_docs = self.env_docs


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "name": name,
            "namespace": namespace,
            "spec": spec,
        })
        if labels is not UNSET:
            field_dict["labels"] = labels
        if status is not UNSET:
            field_dict["status"] = status
        if team is not UNSET:
            field_dict["team"] = team
        if user is not UNSET:
            field_dict["user"] = user
        if created_at is not UNSET:
            field_dict["createdAt"] = created_at
        if env_docs is not UNSET:
            field_dict["envDocs"] = env_docs

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.sandbox_env_labels import SandboxEnvLabels
        from ..models.sandbox_env_spec import SandboxEnvSpec
        from ..models.sandbox_env_status import SandboxEnvStatus
        d = dict(src_dict)
        name = d.pop("name")

        namespace = d.pop("namespace")

        spec = SandboxEnvSpec.from_dict(d.pop("spec"))




        _labels = d.pop("labels", UNSET)
        labels: SandboxEnvLabels | Unset
        if isinstance(_labels,  Unset):
            labels = UNSET
        else:
            labels = SandboxEnvLabels.from_dict(_labels)




        _status = d.pop("status", UNSET)
        status: SandboxEnvStatus | Unset
        if isinstance(_status,  Unset):
            status = UNSET
        else:
            status = SandboxEnvStatus.from_dict(_status)




        team = d.pop("team", UNSET)

        user = d.pop("user", UNSET)

        _created_at = d.pop("createdAt", UNSET)
        created_at: datetime.datetime | Unset
        if isinstance(_created_at,  Unset):
            created_at = UNSET
        else:
            created_at = datetime.datetime.fromisoformat(_created_at)




        env_docs = d.pop("envDocs", UNSET)

        sandbox_env = cls(
            name=name,
            namespace=namespace,
            spec=spec,
            labels=labels,
            status=status,
            team=team,
            user=user,
            created_at=created_at,
            env_docs=env_docs,
        )


        sandbox_env.additional_properties = d
        return sandbox_env

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
