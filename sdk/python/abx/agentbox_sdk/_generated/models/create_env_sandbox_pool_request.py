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
  from ..models.create_env_sandbox_pool_request_annotations import CreateEnvSandboxPoolRequestAnnotations
  from ..models.create_env_sandbox_pool_request_labels import CreateEnvSandboxPoolRequestLabels
  from ..models.resource_requirements import ResourceRequirements





T = TypeVar("T", bound="CreateEnvSandboxPoolRequest")



@_attrs_define
class CreateEnvSandboxPoolRequest:
    """ Add a member SandboxPool to an Env. The server derives:
      - `name`         = "{envName}-{resourceKey}[-{quotaShort}]"
      - `scalingGroup` = `resourceKey` (e.g. "2c8Gi")

    where `resourceKey` is `instancetype.DeriveResourceKey(effective resources)` and
    `quotaShort` (when a quota label is supplied) is `quotaProvider.DeriveShortName(quotaID)`.
    Members in the same `scalingGroup` share an autoscaling policy.

    Sizing accepts three shapes:
      - `instanceType` (+ optional `multiplier`) alone → the Pod is sized to the full
        `instanceType × multiplier` envelope (default `multiplier` = 1).
      - `instanceType` (+ `multiplier`) AND `inlineResources` together → `instanceType ×
        multiplier` is the reservation/billing envelope, while `inlineResources` is the
        actual (possibly rounded-down) Pod request. Every dimension of `inlineResources`
        must be ≤ the envelope (round down allowed, round up rejected with 400); the
        reservation still charges quota for the whole instance.
      - `inlineResources` alone (catalog disabled or no `instanceType`) → explicit
        per-Pool resource requests/limits.
    `scalingGroup` / pool name are derived from the effective Pod request (the rounded-down
    `inlineResources` when supplied, else the full envelope), so the name reflects the Pod's
    real size and Pools downsized differently land in distinct scaling groups.

        Attributes:
            instance_type (str | Unset): InstanceType catalog entry. Required when the catalog is enabled and
                inlineResources is not supplied. May be combined with inlineResources to reserve a whole instance while running
                a smaller (rounded-down) Pod.
            multiplier (int | Unset): Multiplier applied to the InstanceType base resources to form the reservation
                envelope. Defaults to 1.
            inline_resources (ResourceRequirements | Unset): Subset of Kubernetes corev1.ResourceRequirements used for per-
                Pool resource sizing on EnvClusterMember.inlineResources.
            replicas (int | Unset): Initial replica count. Autoscaling, once enabled on this scalingGroup, owns subsequent
                changes.
            min_replicas (int | Unset): Lower bound on this pool's replicas, enforced as a per-member scale-down floor by
                the Env autoscaler.
            max_replicas (int | Unset): Upper bound on this pool's replicas, enforced when the Env autoscaler distributes
                scale-up delta.
            labels (CreateEnvSandboxPoolRequestLabels | Unset): Labels stamped onto this member's SandboxPool. Use for
                plugin-driven metadata such as quota.scitix.ai/url (parsed by the server to derive the pool-name suffix).
            annotations (CreateEnvSandboxPoolRequestAnnotations | Unset): Annotations stamped onto this member's
                SandboxPool.
     """

    instance_type: str | Unset = UNSET
    multiplier: int | Unset = UNSET
    inline_resources: ResourceRequirements | Unset = UNSET
    replicas: int | Unset = UNSET
    min_replicas: int | Unset = UNSET
    max_replicas: int | Unset = UNSET
    labels: CreateEnvSandboxPoolRequestLabels | Unset = UNSET
    annotations: CreateEnvSandboxPoolRequestAnnotations | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.create_env_sandbox_pool_request_annotations import CreateEnvSandboxPoolRequestAnnotations
        from ..models.create_env_sandbox_pool_request_labels import CreateEnvSandboxPoolRequestLabels
        from ..models.resource_requirements import ResourceRequirements
        instance_type = self.instance_type

        multiplier = self.multiplier

        inline_resources: dict[str, Any] | Unset = UNSET
        if not isinstance(self.inline_resources, Unset):
            inline_resources = self.inline_resources.to_dict()

        replicas = self.replicas

        min_replicas = self.min_replicas

        max_replicas = self.max_replicas

        labels: dict[str, Any] | Unset = UNSET
        if not isinstance(self.labels, Unset):
            labels = self.labels.to_dict()

        annotations: dict[str, Any] | Unset = UNSET
        if not isinstance(self.annotations, Unset):
            annotations = self.annotations.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if instance_type is not UNSET:
            field_dict["instanceType"] = instance_type
        if multiplier is not UNSET:
            field_dict["multiplier"] = multiplier
        if inline_resources is not UNSET:
            field_dict["inlineResources"] = inline_resources
        if replicas is not UNSET:
            field_dict["replicas"] = replicas
        if min_replicas is not UNSET:
            field_dict["minReplicas"] = min_replicas
        if max_replicas is not UNSET:
            field_dict["maxReplicas"] = max_replicas
        if labels is not UNSET:
            field_dict["labels"] = labels
        if annotations is not UNSET:
            field_dict["annotations"] = annotations

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.create_env_sandbox_pool_request_annotations import CreateEnvSandboxPoolRequestAnnotations
        from ..models.create_env_sandbox_pool_request_labels import CreateEnvSandboxPoolRequestLabels
        from ..models.resource_requirements import ResourceRequirements
        d = dict(src_dict)
        instance_type = d.pop("instanceType", UNSET)

        multiplier = d.pop("multiplier", UNSET)

        _inline_resources = d.pop("inlineResources", UNSET)
        inline_resources: ResourceRequirements | Unset
        if isinstance(_inline_resources,  Unset):
            inline_resources = UNSET
        else:
            inline_resources = ResourceRequirements.from_dict(_inline_resources)




        replicas = d.pop("replicas", UNSET)

        min_replicas = d.pop("minReplicas", UNSET)

        max_replicas = d.pop("maxReplicas", UNSET)

        _labels = d.pop("labels", UNSET)
        labels: CreateEnvSandboxPoolRequestLabels | Unset
        if isinstance(_labels,  Unset):
            labels = UNSET
        else:
            labels = CreateEnvSandboxPoolRequestLabels.from_dict(_labels)




        _annotations = d.pop("annotations", UNSET)
        annotations: CreateEnvSandboxPoolRequestAnnotations | Unset
        if isinstance(_annotations,  Unset):
            annotations = UNSET
        else:
            annotations = CreateEnvSandboxPoolRequestAnnotations.from_dict(_annotations)




        create_env_sandbox_pool_request = cls(
            instance_type=instance_type,
            multiplier=multiplier,
            inline_resources=inline_resources,
            replicas=replicas,
            min_replicas=min_replicas,
            max_replicas=max_replicas,
            labels=labels,
            annotations=annotations,
        )


        create_env_sandbox_pool_request.additional_properties = d
        return create_env_sandbox_pool_request

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
