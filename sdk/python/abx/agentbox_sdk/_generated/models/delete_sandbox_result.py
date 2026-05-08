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







T = TypeVar("T", bound="DeleteSandboxResult")



@_attrs_define
class DeleteSandboxResult:
    """ 
        Attributes:
            sandbox_id (str): Sandbox identifier. Single-cluster: bare UUID v7; cross-cluster: `{clusterID}.{uuid}`
                composite (dot-separated).
                NOT a strict RFC 4122 UUID — treat as opaque.
                 Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.
            namespace (str): Kubernetes namespace the sandbox belonged to.
            pool_name (str): Name of the SandboxPool the sandbox was allocated from.
            pod_name (str): Name of the Kubernetes Pod that backed the sandbox.
            status (str): Final status of the delete operation (e.g. Stopping).
     """

    sandbox_id: str
    namespace: str
    pool_name: str
    pod_name: str
    status: str
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        sandbox_id = self.sandbox_id

        namespace = self.namespace

        pool_name = self.pool_name

        pod_name = self.pod_name

        status = self.status


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "sandboxId": sandbox_id,
            "namespace": namespace,
            "poolName": pool_name,
            "podName": pod_name,
            "status": status,
        })

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        sandbox_id = d.pop("sandboxId")

        namespace = d.pop("namespace")

        pool_name = d.pop("poolName")

        pod_name = d.pop("podName")

        status = d.pop("status")

        delete_sandbox_result = cls(
            sandbox_id=sandbox_id,
            namespace=namespace,
            pool_name=pool_name,
            pod_name=pod_name,
            status=status,
        )


        delete_sandbox_result.additional_properties = d
        return delete_sandbox_result

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
