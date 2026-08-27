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






T = TypeVar("T", bound="EnvVolumeMount")



@_attrs_define
class EnvVolumeMount:
    """ One PersistentVolumeClaim, or one subtree of it, mounted into the sandbox container.

        Attributes:
            claim_name (str): Name of an existing, Bound PersistentVolumeClaim in this Env's namespace. Cross-namespace
                mounts are not possible in Kubernetes, which is the authorisation boundary: a caller only ever reaches claims in
                the namespace their identity resolved to.
            mount_path (str): Absolute path inside the sandbox container. Must not be '/' or '/mnt', must not sit inside a
                reserved path (/proc, /sys, /dev, /etc, /var/run, /var/lib/kubelet), and must not collide with or nest against a
                path the Template already mounts.
            sub_path (str | Unset): Mount a subtree of the volume instead of its root. Relative, no '..' segments.
                Recommended when the backing PersistentVolume is exposed at its filesystem root. Note kubelet creates a missing
                subPath directory, so a mistyped value mounts an empty directory rather than failing.
            read_only (bool | Unset): Defaults to true. Enforced on the Kubernetes volume source rather than the mount, so
                no container can request read-write for the same claim. Set false only when the agent must write: sandbox code
                runs as root with passwordless sudo and can delete anything writable. Read-only is a container-runtime bind flag
                — a Template whose pod spec can reach the host mount namespace (privileged, SYS_ADMIN, Bidirectional
                propagation, hostPath) cannot enforce it, and such a combination is rejected unless an administrator has opted
                the Template out. Default: True.
     """

    claim_name: str
    mount_path: str
    sub_path: str | Unset = UNSET
    read_only: bool | Unset = True





    def to_dict(self) -> dict[str, Any]:
        claim_name = self.claim_name

        mount_path = self.mount_path

        sub_path = self.sub_path

        read_only = self.read_only


        field_dict: dict[str, Any] = {}

        field_dict.update({
            "claimName": claim_name,
            "mountPath": mount_path,
        })
        if sub_path is not UNSET:
            field_dict["subPath"] = sub_path
        if read_only is not UNSET:
            field_dict["readOnly"] = read_only

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        claim_name = d.pop("claimName")

        mount_path = d.pop("mountPath")

        sub_path = d.pop("subPath", UNSET)

        read_only = d.pop("readOnly", UNSET)

        env_volume_mount = cls(
            claim_name=claim_name,
            mount_path=mount_path,
            sub_path=sub_path,
            read_only=read_only,
        )

        return env_volume_mount

