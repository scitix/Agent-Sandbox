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

"""
Pydantic v2 models for agentbox_sdk — the user-visible data layer.

These models mirror the OpenAPI schema but use snake_case (with camelCase aliases
for JSON parsing) and shield users from the generated layer's UNSET sentinel.
"""

from __future__ import annotations

from datetime import datetime
from enum import Enum
from typing import Any, Dict, Generic, List, Optional, TypeVar

from pydantic import BaseModel, ConfigDict, Field


# ---------------------------------------------------------------------------
# Config shared across all models
# ---------------------------------------------------------------------------

_MODEL_CONFIG = ConfigDict(
    populate_by_name=True,  # allow both snake_case and alias
    from_attributes=True,  # accept ORM-style objects
    extra="ignore",  # tolerate unknown fields from the API
)


# ---------------------------------------------------------------------------
# Sandbox
# ---------------------------------------------------------------------------


class SandboxStatus(str, Enum):
    PENDING = "Pending"
    STARTING = "Starting"
    RUNNING = "Running"
    STOPPING = "Stopping"
    FAILED = "Failed"
    COMPLETED = "Completed"
    CANCELED = "Canceled"


class SandboxStatusDetail(BaseModel):
    model_config = _MODEL_CONFIG

    reason: Optional[str] = None
    message: Optional[str] = None
    last_updated_time: Optional[str] = Field(None, alias="lastUpdatedTime")


class EndpointData(BaseModel):
    """Represents a single runtime endpoint returned by the Sandbox API."""

    model_config = _MODEL_CONFIG

    url: str
    log_dir: Optional[str] = Field(None, alias="logDir")


class SandboxData(BaseModel):
    model_config = _MODEL_CONFIG

    sandbox_id: str = Field(alias="sandboxId")
    namespace: str
    pool_name: str = Field(alias="poolName")
    pod_name: str = Field(alias="podName")
    status: SandboxStatus
    claimed_at: datetime = Field(alias="claimedAt")
    started_at: Optional[datetime] = Field(None, alias="startedAt")
    container_images: Dict[str, str] = Field(
        default_factory=dict, alias="containerImages"
    )
    metadata: Dict[str, str] = Field(default_factory=dict)
    endpoints: Dict[str, EndpointData] = Field(default_factory=dict)
    cpu: Optional[str] = None
    memory: Optional[str] = None
    terminated_at: Optional[datetime] = Field(None, alias="terminatedAt")
    failure_reason: Optional[str] = Field(None, alias="failureReason")
    exit_code: Optional[int] = Field(None, alias="exitCode")
    failure_message: Optional[str] = Field(None, alias="failureMessage")
    status_detail: Optional[SandboxStatusDetail] = Field(
        None, alias="statusDetail"
    )


# ---------------------------------------------------------------------------
# Sandbox Logs
# ---------------------------------------------------------------------------


class SandboxLogEntry(BaseModel):
    model_config = _MODEL_CONFIG

    container: str
    log: str
    timestamp: Optional[datetime] = None


class SandboxLogsData(BaseModel):
    model_config = _MODEL_CONFIG

    sandbox_id: str = Field(alias="sandboxId")
    namespace: str
    pod_name: Optional[str] = Field(None, alias="podName")
    captured_at: Optional[datetime] = Field(None, alias="capturedAt")
    entries: List[SandboxLogEntry] = Field(default_factory=list)
    truncated: bool = False
    total_bytes: Optional[int] = Field(None, alias="totalBytes")
    source: Optional[str] = None


# ---------------------------------------------------------------------------
# SandboxPool
# ---------------------------------------------------------------------------


class SandboxPoolSpec(BaseModel):
    model_config = _MODEL_CONFIG

    replicas: int
    min_replicas: Optional[int] = Field(None, alias="minReplicas")
    max_replicas: Optional[int] = Field(None, alias="maxReplicas")
    template_name: Optional[str] = Field(None, alias="templateName")


class SandboxPoolStatus(BaseModel):
    model_config = _MODEL_CONFIG

    idle_replicas: Optional[int] = Field(None, alias="idleReplicas")
    running_replicas: Optional[int] = Field(None, alias="runningReplicas")
    starting_replicas: Optional[int] = Field(None, alias="startingReplicas")
    stopping_replicas: Optional[int] = Field(None, alias="stoppingReplicas")
    failed_replicas: Optional[int] = Field(None, alias="failedReplicas")


class SandboxPoolData(BaseModel):
    model_config = _MODEL_CONFIG

    name: str
    namespace: str
    spec: SandboxPoolSpec
    status: SandboxPoolStatus = Field(default_factory=SandboxPoolStatus)
    cpu: Optional[str] = None
    memory: Optional[str] = None


# ---------------------------------------------------------------------------
# SandboxTemplate
# ---------------------------------------------------------------------------


class RuntimeData(BaseModel):
    model_config = _MODEL_CONFIG

    name: str
    port: Optional[int] = None
    protocol: Optional[str] = None


class SandboxTemplateSpec(BaseModel):
    model_config = _MODEL_CONFIG

    version: Optional[str] = None
    description: Optional[str] = None
    idle_image: Optional[str] = Field(None, alias="idleImage")
    template: Optional[str] = None
    runtimes: Optional[List[RuntimeData]] = None


class SandboxTemplateStatus(BaseModel):
    model_config = _MODEL_CONFIG

    ready: Optional[bool] = None
    message: Optional[str] = None


class SandboxTemplateData(BaseModel):
    model_config = _MODEL_CONFIG

    name: str
    version: Optional[str] = None
    description: Optional[str] = None
    enable_quota: Optional[bool] = Field(None, alias="enableQuota")
    spec: SandboxTemplateSpec
    labels: Optional[Dict[str, str]] = None
    annotations: Optional[Dict[str, str]] = None
    status: Optional[SandboxTemplateStatus] = None
    created_at: Optional[datetime] = Field(None, alias="createdAt")
    cpu: Optional[str] = None
    memory: Optional[str] = None


# ---------------------------------------------------------------------------
# Auth / WhoAmI
# ---------------------------------------------------------------------------


class WhoAmIData(BaseModel):
    model_config = _MODEL_CONFIG

    role: str
    user: Optional[str] = None
    team: Optional[str] = None


# ---------------------------------------------------------------------------
# Paged results (generic)
# ---------------------------------------------------------------------------

T = TypeVar("T", bound=BaseModel)


class PagedResult(BaseModel, Generic[T]):
    """Generic wrapper for paginated list responses."""

    model_config = _MODEL_CONFIG

    items: List[Any] = Field(default_factory=list)
    total: int = 0
    limit: int = 0
    offset: int = 0
