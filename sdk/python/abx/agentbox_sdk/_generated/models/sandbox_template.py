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






T = TypeVar("T", bound="SandboxTemplate")



@_attrs_define
class SandboxTemplate:
    """ 
        Attributes:
            name (str): Name of the SandboxTemplate cluster resource (RFC 1123 DNS label).
            version (str | Unset): User-defined version of this template (from spec.version).
            description (str | Unset): Human-readable description of the template (from spec.description).
            created_at (datetime.datetime | Unset): RFC 3339 timestamp when the template was created.
            cpu (str | Unset): CPU resource quantity derived from the template's pod spec (e.g. "1", "100m").
            memory (str | Unset): Memory resource quantity derived from the template's pod spec (e.g. "16Gi", "512Mi").
            sync_source (str | Unset): Resource origin: 'global' (synced via ws-proxy) or 'local' (created directly on
                worker). Empty for legacy resources.
            docs (str | Unset): Markdown documentation for the template, stored in the agentbox.navix.sh/docs annotation.
                Supports the placeholders listed under SandboxEnv.envDocs. On Template Get the env-scoped
                ones become readable hints (YOUR_ENV_NAME, YOUR_POOL_NAME, YOUR_API_KEY) because there is
                no env context, while everything derived from the serving cluster's config
                (${AGBX_CLUSTER_ID}, the gateway URLs, ${AGBX_HOST}, ${AGBX_INNER_IP}, ${AGBX_HTTPS},
                ${AGBX_REGISTRY_HOST}) is substituted for real; GetSandboxEnv substitutes everything.

                Editing only the docs does not require bumping spec.version — docs never reach the
                rendered Pod, so they name no new template revision.
            crd_yaml (str | Unset): Complete raw SandboxTemplate CRD YAML (without managedFields). Includes resourceVersion
                and uid for optimistic locking on PUT.
     """

    name: str
    version: str | Unset = UNSET
    description: str | Unset = UNSET
    created_at: datetime.datetime | Unset = UNSET
    cpu: str | Unset = UNSET
    memory: str | Unset = UNSET
    sync_source: str | Unset = UNSET
    docs: str | Unset = UNSET
    crd_yaml: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        name = self.name

        version = self.version

        description = self.description

        created_at: str | Unset = UNSET
        if not isinstance(self.created_at, Unset):
            created_at = self.created_at.isoformat()

        cpu = self.cpu

        memory = self.memory

        sync_source = self.sync_source

        docs = self.docs

        crd_yaml = self.crd_yaml


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "name": name,
        })
        if version is not UNSET:
            field_dict["version"] = version
        if description is not UNSET:
            field_dict["description"] = description
        if created_at is not UNSET:
            field_dict["createdAt"] = created_at
        if cpu is not UNSET:
            field_dict["cpu"] = cpu
        if memory is not UNSET:
            field_dict["memory"] = memory
        if sync_source is not UNSET:
            field_dict["syncSource"] = sync_source
        if docs is not UNSET:
            field_dict["docs"] = docs
        if crd_yaml is not UNSET:
            field_dict["crdYaml"] = crd_yaml

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        name = d.pop("name")

        version = d.pop("version", UNSET)

        description = d.pop("description", UNSET)

        _created_at = d.pop("createdAt", UNSET)
        created_at: datetime.datetime | Unset
        if isinstance(_created_at,  Unset):
            created_at = UNSET
        else:
            created_at = datetime.datetime.fromisoformat(_created_at)




        cpu = d.pop("cpu", UNSET)

        memory = d.pop("memory", UNSET)

        sync_source = d.pop("syncSource", UNSET)

        docs = d.pop("docs", UNSET)

        crd_yaml = d.pop("crdYaml", UNSET)

        sandbox_template = cls(
            name=name,
            version=version,
            description=description,
            created_at=created_at,
            cpu=cpu,
            memory=memory,
            sync_source=sync_source,
            docs=docs,
            crd_yaml=crd_yaml,
        )


        sandbox_template.additional_properties = d
        return sandbox_template

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
