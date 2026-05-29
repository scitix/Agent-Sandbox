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
from dateutil.parser import isoparse
from typing import cast
import datetime

if TYPE_CHECKING:
  from ..models.pool_template_overrides import PoolTemplateOverrides
  from ..models.sandbox_pool_spec import SandboxPoolSpec
  from ..models.sandbox_pool_status import SandboxPoolStatus





T = TypeVar("T", bound="SandboxPool")



@_attrs_define
class SandboxPool:
    """ 
        Example:
            {'name': 'poolname', 'namespace': 't-team-user', 'cpu': '1', 'memory': '16Gi', 'team': 'team', 'user': 'user',
                'spec': {'replicas': 2}, 'status': {'phase': 'Ready', 'idleReplicas': 1, 'unavailableIdleReplicas': 0,
                'runningReplicas': 1, 'startingReplicas': 0, 'stoppingReplicas': 0, 'failedReplicas': 0}}

        Attributes:
            name (str): Name of the SandboxPool (RFC 1123 DNS label).
            namespace (str): Kubernetes namespace the pool is deployed in.
            spec (SandboxPoolSpec):
            status (SandboxPoolStatus):
            cpu (str | Unset): CPU resource per pod in the pool (Kubernetes resource quantity, e.g. "1", "100m").
            memory (str | Unset): Memory resource per pod in the pool (Kubernetes resource quantity, e.g. "16Gi", "512Mi").
            team (str | Unset): Team label of the pool owner (from CRD label)
            user (str | Unset): User label of the pool owner (from CRD label)
            scaling_group (str | Unset): Scaling group this pool belongs to (from the agentbox.navix.sh/scaling-group
                label). Members in the same group share an autoscaling policy. Empty when the pool is excluded from autoscaling.
            template_version (str | Unset): Version of the source SandboxTemplate at last sync (from annotation)
            overrides (PoolTemplateOverrides | Unset): Legacy pool-create overrides. Image-only — per-Pool resource sizing
                flows through EnvClusterMember.{instanceType,multiplier,inlineResources} now.
            spec_yaml (str | Unset): Full EmbeddedSandboxTemplate (idleImage, runtimes, reservation, template) serialized as
                YAML for diff comparison.
            created_at (datetime.datetime | Unset): Creation time of the pool (from metadata.creationTimestamp)
            owning_env (str | Unset): Name of the SandboxEnv that owns this pool (resolved from OwnerReferences). Empty when
                the pool has not been adopted by an Env yet — typical during a brief window after pool creation before the
                PoolAdoptionReconciler runs.
     """

    name: str
    namespace: str
    spec: SandboxPoolSpec
    status: SandboxPoolStatus
    cpu: str | Unset = UNSET
    memory: str | Unset = UNSET
    team: str | Unset = UNSET
    user: str | Unset = UNSET
    scaling_group: str | Unset = UNSET
    template_version: str | Unset = UNSET
    overrides: PoolTemplateOverrides | Unset = UNSET
    spec_yaml: str | Unset = UNSET
    created_at: datetime.datetime | Unset = UNSET
    owning_env: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.pool_template_overrides import PoolTemplateOverrides
        from ..models.sandbox_pool_spec import SandboxPoolSpec
        from ..models.sandbox_pool_status import SandboxPoolStatus
        name = self.name

        namespace = self.namespace

        spec = self.spec.to_dict()

        status = self.status.to_dict()

        cpu = self.cpu

        memory = self.memory

        team = self.team

        user = self.user

        scaling_group = self.scaling_group

        template_version = self.template_version

        overrides: dict[str, Any] | Unset = UNSET
        if not isinstance(self.overrides, Unset):
            overrides = self.overrides.to_dict()

        spec_yaml = self.spec_yaml

        created_at: str | Unset = UNSET
        if not isinstance(self.created_at, Unset):
            created_at = self.created_at.isoformat()

        owning_env = self.owning_env


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "name": name,
            "namespace": namespace,
            "spec": spec,
            "status": status,
        })
        if cpu is not UNSET:
            field_dict["cpu"] = cpu
        if memory is not UNSET:
            field_dict["memory"] = memory
        if team is not UNSET:
            field_dict["team"] = team
        if user is not UNSET:
            field_dict["user"] = user
        if scaling_group is not UNSET:
            field_dict["scalingGroup"] = scaling_group
        if template_version is not UNSET:
            field_dict["templateVersion"] = template_version
        if overrides is not UNSET:
            field_dict["overrides"] = overrides
        if spec_yaml is not UNSET:
            field_dict["specYaml"] = spec_yaml
        if created_at is not UNSET:
            field_dict["createdAt"] = created_at
        if owning_env is not UNSET:
            field_dict["owningEnv"] = owning_env

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.pool_template_overrides import PoolTemplateOverrides
        from ..models.sandbox_pool_spec import SandboxPoolSpec
        from ..models.sandbox_pool_status import SandboxPoolStatus
        d = dict(src_dict)
        name = d.pop("name")

        namespace = d.pop("namespace")

        spec = SandboxPoolSpec.from_dict(d.pop("spec"))




        status = SandboxPoolStatus.from_dict(d.pop("status"))




        cpu = d.pop("cpu", UNSET)

        memory = d.pop("memory", UNSET)

        team = d.pop("team", UNSET)

        user = d.pop("user", UNSET)

        scaling_group = d.pop("scalingGroup", UNSET)

        template_version = d.pop("templateVersion", UNSET)

        _overrides = d.pop("overrides", UNSET)
        overrides: PoolTemplateOverrides | Unset
        if isinstance(_overrides,  Unset):
            overrides = UNSET
        else:
            overrides = PoolTemplateOverrides.from_dict(_overrides)




        spec_yaml = d.pop("specYaml", UNSET)

        _created_at = d.pop("createdAt", UNSET)
        created_at: datetime.datetime | Unset
        if isinstance(_created_at,  Unset):
            created_at = UNSET
        else:
            created_at = isoparse(_created_at)




        owning_env = d.pop("owningEnv", UNSET)

        sandbox_pool = cls(
            name=name,
            namespace=namespace,
            spec=spec,
            status=status,
            cpu=cpu,
            memory=memory,
            team=team,
            user=user,
            scaling_group=scaling_group,
            template_version=template_version,
            overrides=overrides,
            spec_yaml=spec_yaml,
            created_at=created_at,
            owning_env=owning_env,
        )


        sandbox_pool.additional_properties = d
        return sandbox_pool

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
