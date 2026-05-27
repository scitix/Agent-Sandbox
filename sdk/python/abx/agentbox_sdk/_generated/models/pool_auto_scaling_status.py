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

from ..models.pool_auto_scaling_status_last_scale_up_attempt_result import PoolAutoScalingStatusLastScaleUpAttemptResult
from ..types import UNSET, Unset
from dateutil.parser import isoparse
from typing import cast
import datetime






T = TypeVar("T", bound="PoolAutoScalingStatus")



@_attrs_define
class PoolAutoScalingStatus:
    """ Per-Pool autoscaler decision state. Sole writer is the SandboxPool reconciler running the autoscaling decision
    pipeline.

        Attributes:
            last_scale_up_time (datetime.datetime | Unset): Most recent wall-clock time spec.replicas grew (probe accepted
                at least one additional replica). Drives the success cooldown gate (scaleUpPolicy.cooldownSeconds).
            last_scale_down_time (datetime.datetime | Unset): Most recent wall-clock time spec.replicas shrank by one.
                Drives scaleDownPolicy.stabilizationSeconds.
            idle_zero_since (datetime.datetime | Unset): When the Pool's idle replica count first hit zero in the current
                continuous-zero window; cleared when idle > 0. Drives the proactive scaleUpPolicy.idleThresholdSeconds trigger.
            last_scale_up_attempt_time (datetime.datetime | Unset): Most recent wall-clock time the admission probe ran for
                a scale-up attempt, regardless of outcome. Combined with lastScaleUpAttemptResult and
                scaleUpPolicy.saturationCooldownSeconds drives the saturation cooldown.
            last_scale_up_attempt_result (PoolAutoScalingStatusLastScaleUpAttemptResult | Unset): Outcome of the most recent
                admission probe. Enough = probe accepted the full target; JustRight = partial admission (reserved for finer-
                grained reporting); Insufficient = cluster has no headroom; Failed = invalid spec or internal probe error.
            scale_up_error_message (str | Unset): Short single-line description of the most recent non-Enough probe result.
                Empty when lastScaleUpAttemptResult is Enough.
            observed_generation (int | Unset): metadata.generation observed when the autoscaler last wrote this block.
                Clients use it to confirm status freshness relative to spec.
     """

    last_scale_up_time: datetime.datetime | Unset = UNSET
    last_scale_down_time: datetime.datetime | Unset = UNSET
    idle_zero_since: datetime.datetime | Unset = UNSET
    last_scale_up_attempt_time: datetime.datetime | Unset = UNSET
    last_scale_up_attempt_result: PoolAutoScalingStatusLastScaleUpAttemptResult | Unset = UNSET
    scale_up_error_message: str | Unset = UNSET
    observed_generation: int | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        last_scale_up_time: str | Unset = UNSET
        if not isinstance(self.last_scale_up_time, Unset):
            last_scale_up_time = self.last_scale_up_time.isoformat()

        last_scale_down_time: str | Unset = UNSET
        if not isinstance(self.last_scale_down_time, Unset):
            last_scale_down_time = self.last_scale_down_time.isoformat()

        idle_zero_since: str | Unset = UNSET
        if not isinstance(self.idle_zero_since, Unset):
            idle_zero_since = self.idle_zero_since.isoformat()

        last_scale_up_attempt_time: str | Unset = UNSET
        if not isinstance(self.last_scale_up_attempt_time, Unset):
            last_scale_up_attempt_time = self.last_scale_up_attempt_time.isoformat()

        last_scale_up_attempt_result: str | Unset = UNSET
        if not isinstance(self.last_scale_up_attempt_result, Unset):
            last_scale_up_attempt_result = self.last_scale_up_attempt_result.value


        scale_up_error_message = self.scale_up_error_message

        observed_generation = self.observed_generation


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if last_scale_up_time is not UNSET:
            field_dict["lastScaleUpTime"] = last_scale_up_time
        if last_scale_down_time is not UNSET:
            field_dict["lastScaleDownTime"] = last_scale_down_time
        if idle_zero_since is not UNSET:
            field_dict["idleZeroSince"] = idle_zero_since
        if last_scale_up_attempt_time is not UNSET:
            field_dict["lastScaleUpAttemptTime"] = last_scale_up_attempt_time
        if last_scale_up_attempt_result is not UNSET:
            field_dict["lastScaleUpAttemptResult"] = last_scale_up_attempt_result
        if scale_up_error_message is not UNSET:
            field_dict["scaleUpErrorMessage"] = scale_up_error_message
        if observed_generation is not UNSET:
            field_dict["observedGeneration"] = observed_generation

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        _last_scale_up_time = d.pop("lastScaleUpTime", UNSET)
        last_scale_up_time: datetime.datetime | Unset
        if isinstance(_last_scale_up_time,  Unset):
            last_scale_up_time = UNSET
        else:
            last_scale_up_time = isoparse(_last_scale_up_time)




        _last_scale_down_time = d.pop("lastScaleDownTime", UNSET)
        last_scale_down_time: datetime.datetime | Unset
        if isinstance(_last_scale_down_time,  Unset):
            last_scale_down_time = UNSET
        else:
            last_scale_down_time = isoparse(_last_scale_down_time)




        _idle_zero_since = d.pop("idleZeroSince", UNSET)
        idle_zero_since: datetime.datetime | Unset
        if isinstance(_idle_zero_since,  Unset):
            idle_zero_since = UNSET
        else:
            idle_zero_since = isoparse(_idle_zero_since)




        _last_scale_up_attempt_time = d.pop("lastScaleUpAttemptTime", UNSET)
        last_scale_up_attempt_time: datetime.datetime | Unset
        if isinstance(_last_scale_up_attempt_time,  Unset):
            last_scale_up_attempt_time = UNSET
        else:
            last_scale_up_attempt_time = isoparse(_last_scale_up_attempt_time)




        _last_scale_up_attempt_result = d.pop("lastScaleUpAttemptResult", UNSET)
        last_scale_up_attempt_result: PoolAutoScalingStatusLastScaleUpAttemptResult | Unset
        if isinstance(_last_scale_up_attempt_result,  Unset):
            last_scale_up_attempt_result = UNSET
        else:
            last_scale_up_attempt_result = PoolAutoScalingStatusLastScaleUpAttemptResult(_last_scale_up_attempt_result)




        scale_up_error_message = d.pop("scaleUpErrorMessage", UNSET)

        observed_generation = d.pop("observedGeneration", UNSET)

        pool_auto_scaling_status = cls(
            last_scale_up_time=last_scale_up_time,
            last_scale_down_time=last_scale_down_time,
            idle_zero_since=idle_zero_since,
            last_scale_up_attempt_time=last_scale_up_attempt_time,
            last_scale_up_attempt_result=last_scale_up_attempt_result,
            scale_up_error_message=scale_up_error_message,
            observed_generation=observed_generation,
        )


        pool_auto_scaling_status.additional_properties = d
        return pool_auto_scaling_status

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
