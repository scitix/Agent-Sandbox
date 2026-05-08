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
  from ..models.runtime_readiness_probe import RuntimeReadinessProbe





T = TypeVar("T", bound="Runtime")



@_attrs_define
class Runtime:
    """ 
        Attributes:
            name (str):
            port (int | Unset):
            protocol (str | Unset):
            description (str | Unset): Human-readable description of this runtime
            log_dir (str | Unset): Path to the runtime log file inside the container (e.g. /tmp/envd.log)
            readiness_probe (RuntimeReadinessProbe | Unset): Lightweight readiness probe — only httpGet is supported.
                Advanced fields (initialDelaySeconds, periodSeconds, etc.) use Kubernetes defaults.
     """

    name: str
    port: int | Unset = UNSET
    protocol: str | Unset = UNSET
    description: str | Unset = UNSET
    log_dir: str | Unset = UNSET
    readiness_probe: RuntimeReadinessProbe | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        from ..models.runtime_readiness_probe import RuntimeReadinessProbe
        name = self.name

        port = self.port

        protocol = self.protocol

        description = self.description

        log_dir = self.log_dir

        readiness_probe: dict[str, Any] | Unset = UNSET
        if not isinstance(self.readiness_probe, Unset):
            readiness_probe = self.readiness_probe.to_dict()


        field_dict: dict[str, Any] = {}

        field_dict.update({
            "name": name,
        })
        if port is not UNSET:
            field_dict["port"] = port
        if protocol is not UNSET:
            field_dict["protocol"] = protocol
        if description is not UNSET:
            field_dict["description"] = description
        if log_dir is not UNSET:
            field_dict["logDir"] = log_dir
        if readiness_probe is not UNSET:
            field_dict["readinessProbe"] = readiness_probe

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.runtime_readiness_probe import RuntimeReadinessProbe
        d = dict(src_dict)
        name = d.pop("name")

        port = d.pop("port", UNSET)

        protocol = d.pop("protocol", UNSET)

        description = d.pop("description", UNSET)

        log_dir = d.pop("logDir", UNSET)

        _readiness_probe = d.pop("readinessProbe", UNSET)
        readiness_probe: RuntimeReadinessProbe | Unset
        if isinstance(_readiness_probe,  Unset):
            readiness_probe = UNSET
        else:
            readiness_probe = RuntimeReadinessProbe.from_dict(_readiness_probe)




        runtime = cls(
            name=name,
            port=port,
            protocol=protocol,
            description=description,
            log_dir=log_dir,
            readiness_probe=readiness_probe,
        )

        return runtime

