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

from ..models.sandbox_pool_status_phase import SandboxPoolStatusPhase
from ..types import UNSET, Unset
from typing import cast

if TYPE_CHECKING:
  from ..models.pool_auto_scaling_status import PoolAutoScalingStatus





T = TypeVar("T", bound="SandboxPoolStatus")



@_attrs_define
class SandboxPoolStatus:
    """ 
        Attributes:
            phase (SandboxPoolStatusPhase | Unset): High-level pool health summary. Pending=no pods; Ready=all healthy idle
                pods present; ScalingUp/ScalingDown=replica count changing; Degraded=has unavailable idle or failed pods;
                Terminating=pool is being deleted.
            idle_replicas (int | Unset): Number of pods in Idle phase (ready to be claimed).
            unavailable_idle_replicas (int | Unset): Number of Idle pods whose PodReady condition is False (e.g. Pending,
                CrashLoopBackOff). These are counted in idleReplicas but cannot accept requests. Non-zero value triggers
                Degraded phase.
            running_replicas (int | Unset): Number of pods actively serving a sandbox workload.
            starting_replicas (int | Unset): Number of pods currently starting up.
            stopping_replicas (int | Unset): Number of pods transitioning from Running to Idle (in-place upgrade in
                progress).
            failed_replicas (int | Unset): Number of pods in a Failed state.
            pending_requests (int | Unset): Throttled mirror of the in-process PoolScheduler queue depth. Patched every ~3s
                when the queue length changes by at least 20% or crosses the 0/>0 boundary. Useful for Dashboard observability —
                the Env autoscaler reads the live in-process Snapshot instead.
            autoscaling (PoolAutoScalingStatus | Unset): Per-Pool autoscaler decision state. Sole writer is the SandboxPool
                reconciler running the autoscaling decision pipeline.
     """

    phase: SandboxPoolStatusPhase | Unset = UNSET
    idle_replicas: int | Unset = UNSET
    unavailable_idle_replicas: int | Unset = UNSET
    running_replicas: int | Unset = UNSET
    starting_replicas: int | Unset = UNSET
    stopping_replicas: int | Unset = UNSET
    failed_replicas: int | Unset = UNSET
    pending_requests: int | Unset = UNSET
    autoscaling: PoolAutoScalingStatus | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.pool_auto_scaling_status import PoolAutoScalingStatus
        phase: str | Unset = UNSET
        if not isinstance(self.phase, Unset):
            phase = self.phase.value


        idle_replicas = self.idle_replicas

        unavailable_idle_replicas = self.unavailable_idle_replicas

        running_replicas = self.running_replicas

        starting_replicas = self.starting_replicas

        stopping_replicas = self.stopping_replicas

        failed_replicas = self.failed_replicas

        pending_requests = self.pending_requests

        autoscaling: dict[str, Any] | Unset = UNSET
        if not isinstance(self.autoscaling, Unset):
            autoscaling = self.autoscaling.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if phase is not UNSET:
            field_dict["phase"] = phase
        if idle_replicas is not UNSET:
            field_dict["idleReplicas"] = idle_replicas
        if unavailable_idle_replicas is not UNSET:
            field_dict["unavailableIdleReplicas"] = unavailable_idle_replicas
        if running_replicas is not UNSET:
            field_dict["runningReplicas"] = running_replicas
        if starting_replicas is not UNSET:
            field_dict["startingReplicas"] = starting_replicas
        if stopping_replicas is not UNSET:
            field_dict["stoppingReplicas"] = stopping_replicas
        if failed_replicas is not UNSET:
            field_dict["failedReplicas"] = failed_replicas
        if pending_requests is not UNSET:
            field_dict["pendingRequests"] = pending_requests
        if autoscaling is not UNSET:
            field_dict["autoscaling"] = autoscaling

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.pool_auto_scaling_status import PoolAutoScalingStatus
        d = dict(src_dict)
        _phase = d.pop("phase", UNSET)
        phase: SandboxPoolStatusPhase | Unset
        if isinstance(_phase,  Unset):
            phase = UNSET
        else:
            phase = SandboxPoolStatusPhase(_phase)




        idle_replicas = d.pop("idleReplicas", UNSET)

        unavailable_idle_replicas = d.pop("unavailableIdleReplicas", UNSET)

        running_replicas = d.pop("runningReplicas", UNSET)

        starting_replicas = d.pop("startingReplicas", UNSET)

        stopping_replicas = d.pop("stoppingReplicas", UNSET)

        failed_replicas = d.pop("failedReplicas", UNSET)

        pending_requests = d.pop("pendingRequests", UNSET)

        _autoscaling = d.pop("autoscaling", UNSET)
        autoscaling: PoolAutoScalingStatus | Unset
        if isinstance(_autoscaling,  Unset):
            autoscaling = UNSET
        else:
            autoscaling = PoolAutoScalingStatus.from_dict(_autoscaling)




        sandbox_pool_status = cls(
            phase=phase,
            idle_replicas=idle_replicas,
            unavailable_idle_replicas=unavailable_idle_replicas,
            running_replicas=running_replicas,
            starting_replicas=starting_replicas,
            stopping_replicas=stopping_replicas,
            failed_replicas=failed_replicas,
            pending_requests=pending_requests,
            autoscaling=autoscaling,
        )


        sandbox_pool_status.additional_properties = d
        return sandbox_pool_status

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
