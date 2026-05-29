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

from ..models.sandbox_status import SandboxStatus
from ..types import UNSET, Unset
from dateutil.parser import isoparse
from typing import cast
import datetime

if TYPE_CHECKING:
  from ..models.sandbox_container_images import SandboxContainerImages
  from ..models.sandbox_endpoints import SandboxEndpoints
  from ..models.sandbox_metadata import SandboxMetadata
  from ..models.sandbox_status_detail import SandboxStatusDetail





T = TypeVar("T", bound="Sandbox")



@_attrs_define
class Sandbox:
    """ 
        Example:
            {'sandboxId': '5de15c92-8fb5-440f-a9ea-7f62f734f1b9', 'namespace': 't-team-user', 'poolName': 'poolname',
                'podName': 'poolname-wxtfc', 'status': 'Completed', 'claimedAt': '2026-04-07T14:37:54Z', 'startedAt':
                '2026-04-07T14:37:55Z', 'terminatedAt': '2026-04-07T14:38:10Z', 'recycledAt': '2026-04-07T14:38:11Z',
                'durationSeconds': 15, 'cpu': '1', 'memory': '16Gi', 'team': 'team', 'user': 'user', 'containerImages':
                {'sandbox': 'docker.io/project/name:tag'}}

        Attributes:
            sandbox_id (str): Sandbox identifier. Single-cluster: bare UUID v7 (e.g.
                `5de15c92-8fb5-440f-a9ea-7f62f734f1b9`).
                Cross-cluster: `{clusterID}.{uuid}` composite (e.g. `cluster1.5de15c92-...`, dot-separated). NOT a strict RFC
                4122 UUID — treat as opaque.
                 Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.
            namespace (str): Kubernetes namespace where the sandbox pod is running.
            pool_name (str): Name of the SandboxPool this sandbox was allocated from.
            pod_name (str): Name of the Kubernetes Pod backing this sandbox.
            status (SandboxStatus): Current lifecycle phase of the sandbox.
            claimed_at (datetime.datetime): RFC 3339 timestamp when the sandbox was claimed by a user.
            env_name (str | Unset): Name of the SandboxEnv that owns the pool this sandbox was allocated from (from pod
                label agentbox.navix.sh/env).
            started_at (datetime.datetime | Unset): RFC 3339 timestamp when the sandbox transitioned to Running.
            container_images (SandboxContainerImages | Unset): Map of container name to image URI for each container in the
                sandbox pod.
            metadata (SandboxMetadata | Unset): User-defined key-value pairs attached to the sandbox for filtering and
                annotation.
            endpoints (SandboxEndpoints | Unset): Map of runtime name to its endpoint URL, populated once the sandbox is
                Running.
            cpu (str | Unset): CPU resource allocated to the sandbox (Kubernetes resource quantity, e.g. "1", "100m").
            memory (str | Unset): Memory resource allocated to the sandbox (Kubernetes resource quantity, e.g. "16Gi",
                "512Mi").
            terminated_at (datetime.datetime | Unset): RFC 3339 timestamp when the sandbox workload terminated (exit or stop
                signal).
            recycled_at (datetime.datetime | Unset): Timestamp when the sandbox completed recycling (Stopping → Idle in-
                place upgrade) and the record was written to the store. Also set for evicted/externally-deleted sandboxes to
                record when the Failed record was persisted.
            duration_seconds (int | Unset): Wall-clock duration of the sandbox in seconds. Set for Running (startedAt to
                query time), Completed, Failed, and Released states (startedAt to terminatedAt). Omitted for Starting and
                Canceled states.
            failure_reason (str | Unset): Machine-readable reason code when status is Failed.
            exit_code (int | Unset): Exit code of the main container when the sandbox terminated.
            failure_message (str | Unset): Human-readable message describing the failure cause.
            status_detail (SandboxStatusDetail | Unset):
            team (str | Unset): Team label of the sandbox owner (from pod label)
            user (str | Unset): User label of the sandbox owner (from pod label)
            node_name (str | Unset): Kubernetes node the sandbox Pod was scheduled onto.
            container_id (str | Unset): Runtime container ID (e.g. "docker://abc123") of the primary container, captured
                when the sandbox entered Running state.
     """

    sandbox_id: str
    namespace: str
    pool_name: str
    pod_name: str
    status: SandboxStatus
    claimed_at: datetime.datetime
    env_name: str | Unset = UNSET
    started_at: datetime.datetime | Unset = UNSET
    container_images: SandboxContainerImages | Unset = UNSET
    metadata: SandboxMetadata | Unset = UNSET
    endpoints: SandboxEndpoints | Unset = UNSET
    cpu: str | Unset = UNSET
    memory: str | Unset = UNSET
    terminated_at: datetime.datetime | Unset = UNSET
    recycled_at: datetime.datetime | Unset = UNSET
    duration_seconds: int | Unset = UNSET
    failure_reason: str | Unset = UNSET
    exit_code: int | Unset = UNSET
    failure_message: str | Unset = UNSET
    status_detail: SandboxStatusDetail | Unset = UNSET
    team: str | Unset = UNSET
    user: str | Unset = UNSET
    node_name: str | Unset = UNSET
    container_id: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.sandbox_container_images import SandboxContainerImages
        from ..models.sandbox_endpoints import SandboxEndpoints
        from ..models.sandbox_metadata import SandboxMetadata
        from ..models.sandbox_status_detail import SandboxStatusDetail
        sandbox_id = self.sandbox_id

        namespace = self.namespace

        pool_name = self.pool_name

        pod_name = self.pod_name

        status = self.status.value

        claimed_at = self.claimed_at.isoformat()

        env_name = self.env_name

        started_at: str | Unset = UNSET
        if not isinstance(self.started_at, Unset):
            started_at = self.started_at.isoformat()

        container_images: dict[str, Any] | Unset = UNSET
        if not isinstance(self.container_images, Unset):
            container_images = self.container_images.to_dict()

        metadata: dict[str, Any] | Unset = UNSET
        if not isinstance(self.metadata, Unset):
            metadata = self.metadata.to_dict()

        endpoints: dict[str, Any] | Unset = UNSET
        if not isinstance(self.endpoints, Unset):
            endpoints = self.endpoints.to_dict()

        cpu = self.cpu

        memory = self.memory

        terminated_at: str | Unset = UNSET
        if not isinstance(self.terminated_at, Unset):
            terminated_at = self.terminated_at.isoformat()

        recycled_at: str | Unset = UNSET
        if not isinstance(self.recycled_at, Unset):
            recycled_at = self.recycled_at.isoformat()

        duration_seconds = self.duration_seconds

        failure_reason = self.failure_reason

        exit_code = self.exit_code

        failure_message = self.failure_message

        status_detail: dict[str, Any] | Unset = UNSET
        if not isinstance(self.status_detail, Unset):
            status_detail = self.status_detail.to_dict()

        team = self.team

        user = self.user

        node_name = self.node_name

        container_id = self.container_id


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "sandboxId": sandbox_id,
            "namespace": namespace,
            "poolName": pool_name,
            "podName": pod_name,
            "status": status,
            "claimedAt": claimed_at,
        })
        if env_name is not UNSET:
            field_dict["envName"] = env_name
        if started_at is not UNSET:
            field_dict["startedAt"] = started_at
        if container_images is not UNSET:
            field_dict["containerImages"] = container_images
        if metadata is not UNSET:
            field_dict["metadata"] = metadata
        if endpoints is not UNSET:
            field_dict["endpoints"] = endpoints
        if cpu is not UNSET:
            field_dict["cpu"] = cpu
        if memory is not UNSET:
            field_dict["memory"] = memory
        if terminated_at is not UNSET:
            field_dict["terminatedAt"] = terminated_at
        if recycled_at is not UNSET:
            field_dict["recycledAt"] = recycled_at
        if duration_seconds is not UNSET:
            field_dict["durationSeconds"] = duration_seconds
        if failure_reason is not UNSET:
            field_dict["failureReason"] = failure_reason
        if exit_code is not UNSET:
            field_dict["exitCode"] = exit_code
        if failure_message is not UNSET:
            field_dict["failureMessage"] = failure_message
        if status_detail is not UNSET:
            field_dict["statusDetail"] = status_detail
        if team is not UNSET:
            field_dict["team"] = team
        if user is not UNSET:
            field_dict["user"] = user
        if node_name is not UNSET:
            field_dict["nodeName"] = node_name
        if container_id is not UNSET:
            field_dict["containerId"] = container_id

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.sandbox_container_images import SandboxContainerImages
        from ..models.sandbox_endpoints import SandboxEndpoints
        from ..models.sandbox_metadata import SandboxMetadata
        from ..models.sandbox_status_detail import SandboxStatusDetail
        d = dict(src_dict)
        sandbox_id = d.pop("sandboxId")

        namespace = d.pop("namespace")

        pool_name = d.pop("poolName")

        pod_name = d.pop("podName")

        status = SandboxStatus(d.pop("status"))




        claimed_at = isoparse(d.pop("claimedAt"))




        env_name = d.pop("envName", UNSET)

        _started_at = d.pop("startedAt", UNSET)
        started_at: datetime.datetime | Unset
        if isinstance(_started_at,  Unset):
            started_at = UNSET
        else:
            started_at = isoparse(_started_at)




        _container_images = d.pop("containerImages", UNSET)
        container_images: SandboxContainerImages | Unset
        if isinstance(_container_images,  Unset):
            container_images = UNSET
        else:
            container_images = SandboxContainerImages.from_dict(_container_images)




        _metadata = d.pop("metadata", UNSET)
        metadata: SandboxMetadata | Unset
        if isinstance(_metadata,  Unset):
            metadata = UNSET
        else:
            metadata = SandboxMetadata.from_dict(_metadata)




        _endpoints = d.pop("endpoints", UNSET)
        endpoints: SandboxEndpoints | Unset
        if isinstance(_endpoints,  Unset):
            endpoints = UNSET
        else:
            endpoints = SandboxEndpoints.from_dict(_endpoints)




        cpu = d.pop("cpu", UNSET)

        memory = d.pop("memory", UNSET)

        _terminated_at = d.pop("terminatedAt", UNSET)
        terminated_at: datetime.datetime | Unset
        if isinstance(_terminated_at,  Unset):
            terminated_at = UNSET
        else:
            terminated_at = isoparse(_terminated_at)




        _recycled_at = d.pop("recycledAt", UNSET)
        recycled_at: datetime.datetime | Unset
        if isinstance(_recycled_at,  Unset):
            recycled_at = UNSET
        else:
            recycled_at = isoparse(_recycled_at)




        duration_seconds = d.pop("durationSeconds", UNSET)

        failure_reason = d.pop("failureReason", UNSET)

        exit_code = d.pop("exitCode", UNSET)

        failure_message = d.pop("failureMessage", UNSET)

        _status_detail = d.pop("statusDetail", UNSET)
        status_detail: SandboxStatusDetail | Unset
        if isinstance(_status_detail,  Unset):
            status_detail = UNSET
        else:
            status_detail = SandboxStatusDetail.from_dict(_status_detail)




        team = d.pop("team", UNSET)

        user = d.pop("user", UNSET)

        node_name = d.pop("nodeName", UNSET)

        container_id = d.pop("containerId", UNSET)

        sandbox = cls(
            sandbox_id=sandbox_id,
            namespace=namespace,
            pool_name=pool_name,
            pod_name=pod_name,
            status=status,
            claimed_at=claimed_at,
            env_name=env_name,
            started_at=started_at,
            container_images=container_images,
            metadata=metadata,
            endpoints=endpoints,
            cpu=cpu,
            memory=memory,
            terminated_at=terminated_at,
            recycled_at=recycled_at,
            duration_seconds=duration_seconds,
            failure_reason=failure_reason,
            exit_code=exit_code,
            failure_message=failure_message,
            status_detail=status_detail,
            team=team,
            user=user,
            node_name=node_name,
            container_id=container_id,
        )


        sandbox.additional_properties = d
        return sandbox

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
