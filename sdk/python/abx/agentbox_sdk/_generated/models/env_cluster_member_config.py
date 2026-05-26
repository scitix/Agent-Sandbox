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
  from ..models.env_cluster_member_config_annotations import EnvClusterMemberConfigAnnotations
  from ..models.env_cluster_member_config_labels import EnvClusterMemberConfigLabels
  from ..models.resource_requirements import ResourceRequirements





T = TypeVar("T", bound="EnvClusterMemberConfig")



@_attrs_define
class EnvClusterMemberConfig:
    """ User-declared intent for one Env member. Plugins do not mutate this —
    it stays equal to whatever the caller supplied at AddMember /
    UpdateMember time, so it remains a faithful description of the
    request shape across the member's lifetime.

        Attributes:
            labels (EnvClusterMemberConfigLabels | Unset): User-supplied SandboxPool metadata.labels (e.g.
                quota.scitix.ai/url). Plugins may consume these for routing decisions; their output lands on the server-internal
                member.metadata, not here.
            annotations (EnvClusterMemberConfigAnnotations | Unset): User-supplied SandboxPool metadata.annotations. Same
                propagation as labels.
            instance_type (str | Unset): Optional InstanceType catalog entry referenced by this member.
            multiplier (int | Unset): Multiplier applied to the InstanceType base resources for this member.
            inline_resources (ResourceRequirements | Unset): Subset of Kubernetes corev1.ResourceRequirements used for per-
                Pool resource sizing on EnvClusterMember.inlineResources.
            scaling_group (str | Unset): ScalingGroup name (typically derived from the effective resources, e.g. '1c4Gi').
                Members in the same group share autoscaling policy.
            replicas (int | Unset): Initial replica count for this member Pool. Autoscaling owns subsequent changes — the
                Reconciler does not force this value back on later reconciles.
            max_replicas (int | Unset): Upper bound on this member's spec.replicas. Enforced by the Env autoscaler when
                distributing scale-up delta.
            priority (int | Unset): Canonical preference: lower wins. Used for routing tiebreaks and as the default for
                scaleUpPriority / scaleDownPriority when those are unset.
            scale_up_priority (int | Unset): Scale-up order within the scaling group — lower scaled first. When omitted,
                falls back to priority. Same-value tiebreak by name.
            scale_down_priority (int | Unset): Scale-down order within the scaling group — lower shrunk first. When omitted,
                falls back to priority.
     """

    labels: EnvClusterMemberConfigLabels | Unset = UNSET
    annotations: EnvClusterMemberConfigAnnotations | Unset = UNSET
    instance_type: str | Unset = UNSET
    multiplier: int | Unset = UNSET
    inline_resources: ResourceRequirements | Unset = UNSET
    scaling_group: str | Unset = UNSET
    replicas: int | Unset = UNSET
    max_replicas: int | Unset = UNSET
    priority: int | Unset = UNSET
    scale_up_priority: int | Unset = UNSET
    scale_down_priority: int | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.env_cluster_member_config_annotations import EnvClusterMemberConfigAnnotations
        from ..models.env_cluster_member_config_labels import EnvClusterMemberConfigLabels
        from ..models.resource_requirements import ResourceRequirements
        labels: dict[str, Any] | Unset = UNSET
        if not isinstance(self.labels, Unset):
            labels = self.labels.to_dict()

        annotations: dict[str, Any] | Unset = UNSET
        if not isinstance(self.annotations, Unset):
            annotations = self.annotations.to_dict()

        instance_type = self.instance_type

        multiplier = self.multiplier

        inline_resources: dict[str, Any] | Unset = UNSET
        if not isinstance(self.inline_resources, Unset):
            inline_resources = self.inline_resources.to_dict()

        scaling_group = self.scaling_group

        replicas = self.replicas

        max_replicas = self.max_replicas

        priority = self.priority

        scale_up_priority = self.scale_up_priority

        scale_down_priority = self.scale_down_priority


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
        })
        if labels is not UNSET:
            field_dict["labels"] = labels
        if annotations is not UNSET:
            field_dict["annotations"] = annotations
        if instance_type is not UNSET:
            field_dict["instanceType"] = instance_type
        if multiplier is not UNSET:
            field_dict["multiplier"] = multiplier
        if inline_resources is not UNSET:
            field_dict["inlineResources"] = inline_resources
        if scaling_group is not UNSET:
            field_dict["scalingGroup"] = scaling_group
        if replicas is not UNSET:
            field_dict["replicas"] = replicas
        if max_replicas is not UNSET:
            field_dict["maxReplicas"] = max_replicas
        if priority is not UNSET:
            field_dict["priority"] = priority
        if scale_up_priority is not UNSET:
            field_dict["scaleUpPriority"] = scale_up_priority
        if scale_down_priority is not UNSET:
            field_dict["scaleDownPriority"] = scale_down_priority

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.env_cluster_member_config_annotations import EnvClusterMemberConfigAnnotations
        from ..models.env_cluster_member_config_labels import EnvClusterMemberConfigLabels
        from ..models.resource_requirements import ResourceRequirements
        d = dict(src_dict)
        _labels = d.pop("labels", UNSET)
        labels: EnvClusterMemberConfigLabels | Unset
        if isinstance(_labels,  Unset):
            labels = UNSET
        else:
            labels = EnvClusterMemberConfigLabels.from_dict(_labels)




        _annotations = d.pop("annotations", UNSET)
        annotations: EnvClusterMemberConfigAnnotations | Unset
        if isinstance(_annotations,  Unset):
            annotations = UNSET
        else:
            annotations = EnvClusterMemberConfigAnnotations.from_dict(_annotations)




        instance_type = d.pop("instanceType", UNSET)

        multiplier = d.pop("multiplier", UNSET)

        _inline_resources = d.pop("inlineResources", UNSET)
        inline_resources: ResourceRequirements | Unset
        if isinstance(_inline_resources,  Unset):
            inline_resources = UNSET
        else:
            inline_resources = ResourceRequirements.from_dict(_inline_resources)




        scaling_group = d.pop("scalingGroup", UNSET)

        replicas = d.pop("replicas", UNSET)

        max_replicas = d.pop("maxReplicas", UNSET)

        priority = d.pop("priority", UNSET)

        scale_up_priority = d.pop("scaleUpPriority", UNSET)

        scale_down_priority = d.pop("scaleDownPriority", UNSET)

        env_cluster_member_config = cls(
            labels=labels,
            annotations=annotations,
            instance_type=instance_type,
            multiplier=multiplier,
            inline_resources=inline_resources,
            scaling_group=scaling_group,
            replicas=replicas,
            max_replicas=max_replicas,
            priority=priority,
            scale_up_priority=scale_up_priority,
            scale_down_priority=scale_down_priority,
        )


        env_cluster_member_config.additional_properties = d
        return env_cluster_member_config

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
