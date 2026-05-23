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
  from ..models.create_sandbox_pool_request_annotations import CreateSandboxPoolRequestAnnotations
  from ..models.create_sandbox_pool_request_labels import CreateSandboxPoolRequestLabels
  from ..models.image_pull_secret_input import ImagePullSecretInput
  from ..models.pool_template_overrides import PoolTemplateOverrides
  from ..models.sandbox_pool_spec import SandboxPoolSpec





T = TypeVar("T", bound="CreateSandboxPoolRequest")



@_attrs_define
class CreateSandboxPoolRequest:
    """ 
        Attributes:
            name (str): RFC 1123 DNS label (letter-start): lowercase letters, digits, hyphens; start with a letter, end with
                alphanumeric
            template_name (str | Unset):
            replicas (int | Unset):
            labels (CreateSandboxPoolRequestLabels | Unset):
            annotations (CreateSandboxPoolRequestAnnotations | Unset):
            spec (SandboxPoolSpec | Unset):
            quota_url (str | Unset): Deprecated: pass quota URL via labels["quota.scitix.ai/url"] instead.
            overrides (PoolTemplateOverrides | Unset): Persisted pool-level overrides applied on top of the referenced
                template and re-applied during template sync.
            image_pull_secret (ImagePullSecretInput | Unset):
     """

    name: str
    template_name: str | Unset = UNSET
    replicas: int | Unset = UNSET
    labels: CreateSandboxPoolRequestLabels | Unset = UNSET
    annotations: CreateSandboxPoolRequestAnnotations | Unset = UNSET
    spec: SandboxPoolSpec | Unset = UNSET
    quota_url: str | Unset = UNSET
    overrides: PoolTemplateOverrides | Unset = UNSET
    image_pull_secret: ImagePullSecretInput | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.create_sandbox_pool_request_annotations import CreateSandboxPoolRequestAnnotations
        from ..models.create_sandbox_pool_request_labels import CreateSandboxPoolRequestLabels
        from ..models.image_pull_secret_input import ImagePullSecretInput
        from ..models.pool_template_overrides import PoolTemplateOverrides
        from ..models.sandbox_pool_spec import SandboxPoolSpec
        name = self.name

        template_name = self.template_name

        replicas = self.replicas

        labels: dict[str, Any] | Unset = UNSET
        if not isinstance(self.labels, Unset):
            labels = self.labels.to_dict()

        annotations: dict[str, Any] | Unset = UNSET
        if not isinstance(self.annotations, Unset):
            annotations = self.annotations.to_dict()

        spec: dict[str, Any] | Unset = UNSET
        if not isinstance(self.spec, Unset):
            spec = self.spec.to_dict()

        quota_url = self.quota_url

        overrides: dict[str, Any] | Unset = UNSET
        if not isinstance(self.overrides, Unset):
            overrides = self.overrides.to_dict()

        image_pull_secret: dict[str, Any] | Unset = UNSET
        if not isinstance(self.image_pull_secret, Unset):
            image_pull_secret = self.image_pull_secret.to_dict()


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "name": name,
        })
        if template_name is not UNSET:
            field_dict["templateName"] = template_name
        if replicas is not UNSET:
            field_dict["replicas"] = replicas
        if labels is not UNSET:
            field_dict["labels"] = labels
        if annotations is not UNSET:
            field_dict["annotations"] = annotations
        if spec is not UNSET:
            field_dict["spec"] = spec
        if quota_url is not UNSET:
            field_dict["quotaUrl"] = quota_url
        if overrides is not UNSET:
            field_dict["overrides"] = overrides
        if image_pull_secret is not UNSET:
            field_dict["imagePullSecret"] = image_pull_secret

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.create_sandbox_pool_request_annotations import CreateSandboxPoolRequestAnnotations
        from ..models.create_sandbox_pool_request_labels import CreateSandboxPoolRequestLabels
        from ..models.image_pull_secret_input import ImagePullSecretInput
        from ..models.pool_template_overrides import PoolTemplateOverrides
        from ..models.sandbox_pool_spec import SandboxPoolSpec
        d = dict(src_dict)
        name = d.pop("name")

        template_name = d.pop("templateName", UNSET)

        replicas = d.pop("replicas", UNSET)

        _labels = d.pop("labels", UNSET)
        labels: CreateSandboxPoolRequestLabels | Unset
        if isinstance(_labels,  Unset):
            labels = UNSET
        else:
            labels = CreateSandboxPoolRequestLabels.from_dict(_labels)




        _annotations = d.pop("annotations", UNSET)
        annotations: CreateSandboxPoolRequestAnnotations | Unset
        if isinstance(_annotations,  Unset):
            annotations = UNSET
        else:
            annotations = CreateSandboxPoolRequestAnnotations.from_dict(_annotations)




        _spec = d.pop("spec", UNSET)
        spec: SandboxPoolSpec | Unset
        if isinstance(_spec,  Unset):
            spec = UNSET
        else:
            spec = SandboxPoolSpec.from_dict(_spec)




        quota_url = d.pop("quotaUrl", UNSET)

        _overrides = d.pop("overrides", UNSET)
        overrides: PoolTemplateOverrides | Unset
        if isinstance(_overrides,  Unset):
            overrides = UNSET
        else:
            overrides = PoolTemplateOverrides.from_dict(_overrides)




        _image_pull_secret = d.pop("imagePullSecret", UNSET)
        image_pull_secret: ImagePullSecretInput | Unset
        if isinstance(_image_pull_secret,  Unset):
            image_pull_secret = UNSET
        else:
            image_pull_secret = ImagePullSecretInput.from_dict(_image_pull_secret)




        create_sandbox_pool_request = cls(
            name=name,
            template_name=template_name,
            replicas=replicas,
            labels=labels,
            annotations=annotations,
            spec=spec,
            quota_url=quota_url,
            overrides=overrides,
            image_pull_secret=image_pull_secret,
        )


        create_sandbox_pool_request.additional_properties = d
        return create_sandbox_pool_request

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
