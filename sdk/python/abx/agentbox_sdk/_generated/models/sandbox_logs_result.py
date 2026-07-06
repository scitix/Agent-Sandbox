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

from ..models.sandbox_logs_result_source import SandboxLogsResultSource
from ..types import UNSET, Unset
from typing import cast
import datetime

if TYPE_CHECKING:
  from ..models.sandbox_log_entry import SandboxLogEntry





T = TypeVar("T", bound="SandboxLogsResult")



@_attrs_define
class SandboxLogsResult:
    """ 
        Attributes:
            sandbox_id (str): Sandbox identifier. Single-cluster: bare UUID v7; cross-cluster: `{clusterID}.{uuid}`
                composite (dot-separated).
                NOT a strict RFC 4122 UUID — treat as opaque.
                 Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.
            namespace (str): Kubernetes namespace of the sandbox.
            entries (list[SandboxLogEntry]): Ordered list of log lines (oldest first).
            truncated (bool): True when the response was truncated due to the `lines` limit or internal size cap.
            source (SandboxLogsResultSource):
            pod_name (str | Unset): Name of the Kubernetes Pod backing the sandbox.
            captured_at (datetime.datetime | Unset): RFC 3339 timestamp when the log snapshot was captured.
            total_bytes (int | Unset): Total byte size of all log entries before any truncation.
            runtime_name (str | Unset): When source=runtime, the runtime name whose log file was read
     """

    sandbox_id: str
    namespace: str
    entries: list[SandboxLogEntry]
    truncated: bool
    source: SandboxLogsResultSource
    pod_name: str | Unset = UNSET
    captured_at: datetime.datetime | Unset = UNSET
    total_bytes: int | Unset = UNSET
    runtime_name: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.sandbox_log_entry import SandboxLogEntry
        sandbox_id = self.sandbox_id

        namespace = self.namespace

        entries = []
        for entries_item_data in self.entries:
            entries_item = entries_item_data.to_dict()
            entries.append(entries_item)



        truncated = self.truncated

        source = self.source.value

        pod_name = self.pod_name

        captured_at: str | Unset = UNSET
        if not isinstance(self.captured_at, Unset):
            captured_at = self.captured_at.isoformat()

        total_bytes = self.total_bytes

        runtime_name = self.runtime_name


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "sandboxId": sandbox_id,
            "namespace": namespace,
            "entries": entries,
            "truncated": truncated,
            "source": source,
        })
        if pod_name is not UNSET:
            field_dict["podName"] = pod_name
        if captured_at is not UNSET:
            field_dict["capturedAt"] = captured_at
        if total_bytes is not UNSET:
            field_dict["totalBytes"] = total_bytes
        if runtime_name is not UNSET:
            field_dict["runtimeName"] = runtime_name

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.sandbox_log_entry import SandboxLogEntry
        d = dict(src_dict)
        sandbox_id = d.pop("sandboxId")

        namespace = d.pop("namespace")

        entries = []
        _entries = d.pop("entries")
        for entries_item_data in (_entries):
            entries_item = SandboxLogEntry.from_dict(entries_item_data)



            entries.append(entries_item)


        truncated = d.pop("truncated")

        source = SandboxLogsResultSource(d.pop("source"))




        pod_name = d.pop("podName", UNSET)

        _captured_at = d.pop("capturedAt", UNSET)
        captured_at: datetime.datetime | Unset
        if isinstance(_captured_at,  Unset):
            captured_at = UNSET
        else:
            captured_at = datetime.datetime.fromisoformat(_captured_at)




        total_bytes = d.pop("totalBytes", UNSET)

        runtime_name = d.pop("runtimeName", UNSET)

        sandbox_logs_result = cls(
            sandbox_id=sandbox_id,
            namespace=namespace,
            entries=entries,
            truncated=truncated,
            source=source,
            pod_name=pod_name,
            captured_at=captured_at,
            total_bytes=total_bytes,
            runtime_name=runtime_name,
        )


        sandbox_logs_result.additional_properties = d
        return sandbox_logs_result

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
