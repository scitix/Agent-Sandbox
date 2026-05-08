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
  from ..models.create_sandbox_request_annotations import CreateSandboxRequestAnnotations
  from ..models.create_sandbox_request_container_images import CreateSandboxRequestContainerImages
  from ..models.create_sandbox_request_labels import CreateSandboxRequestLabels
  from ..models.create_sandbox_request_metadata import CreateSandboxRequestMetadata





T = TypeVar("T", bound="CreateSandboxRequest")



@_attrs_define
class CreateSandboxRequest:
    """ 
        Attributes:
            pool_name (str): Name of the SandboxPool to allocate the sandbox from. Example: my-pool.
            image (str | Unset): Override the main container image for this sandbox. Replaces the pool's default image.
                Example: docker.io/myorg/myimage:latest.
            container_images (CreateSandboxRequestContainerImages | Unset): Per-container image overrides keyed by container
                name. Takes precedence over `image`.
            labels (CreateSandboxRequestLabels | Unset): Kubernetes labels to apply to the sandbox pod.
            annotations (CreateSandboxRequestAnnotations | Unset): Kubernetes annotations to apply to the sandbox pod.
            metadata (CreateSandboxRequestMetadata | Unset): User-defined key-value metadata for filtering and annotation.
                Available on the sandbox object after creation.
            startup_timeout (str | Unset): Maximum duration to wait for the sandbox to reach Running status before marking
                it as Failed. Duration string, e.g. '30s', '2m'. Example: 2m.
            idle_timeout (str | Unset): Duration of inactivity after which the sandbox is automatically stopped. Duration
                string, e.g. '5m', '1h'. Use '0' to disable. Example: 30m.
     """

    pool_name: str
    image: str | Unset = UNSET
    container_images: CreateSandboxRequestContainerImages | Unset = UNSET
    labels: CreateSandboxRequestLabels | Unset = UNSET
    annotations: CreateSandboxRequestAnnotations | Unset = UNSET
    metadata: CreateSandboxRequestMetadata | Unset = UNSET
    startup_timeout: str | Unset = UNSET
    idle_timeout: str | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        from ..models.create_sandbox_request_annotations import CreateSandboxRequestAnnotations
        from ..models.create_sandbox_request_container_images import CreateSandboxRequestContainerImages
        from ..models.create_sandbox_request_labels import CreateSandboxRequestLabels
        from ..models.create_sandbox_request_metadata import CreateSandboxRequestMetadata
        pool_name = self.pool_name

        image = self.image

        container_images: dict[str, Any] | Unset = UNSET
        if not isinstance(self.container_images, Unset):
            container_images = self.container_images.to_dict()

        labels: dict[str, Any] | Unset = UNSET
        if not isinstance(self.labels, Unset):
            labels = self.labels.to_dict()

        annotations: dict[str, Any] | Unset = UNSET
        if not isinstance(self.annotations, Unset):
            annotations = self.annotations.to_dict()

        metadata: dict[str, Any] | Unset = UNSET
        if not isinstance(self.metadata, Unset):
            metadata = self.metadata.to_dict()

        startup_timeout = self.startup_timeout

        idle_timeout = self.idle_timeout


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "poolName": pool_name,
        })
        if image is not UNSET:
            field_dict["image"] = image
        if container_images is not UNSET:
            field_dict["containerImages"] = container_images
        if labels is not UNSET:
            field_dict["labels"] = labels
        if annotations is not UNSET:
            field_dict["annotations"] = annotations
        if metadata is not UNSET:
            field_dict["metadata"] = metadata
        if startup_timeout is not UNSET:
            field_dict["startupTimeout"] = startup_timeout
        if idle_timeout is not UNSET:
            field_dict["idleTimeout"] = idle_timeout

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.create_sandbox_request_annotations import CreateSandboxRequestAnnotations
        from ..models.create_sandbox_request_container_images import CreateSandboxRequestContainerImages
        from ..models.create_sandbox_request_labels import CreateSandboxRequestLabels
        from ..models.create_sandbox_request_metadata import CreateSandboxRequestMetadata
        d = dict(src_dict)
        pool_name = d.pop("poolName")

        image = d.pop("image", UNSET)

        _container_images = d.pop("containerImages", UNSET)
        container_images: CreateSandboxRequestContainerImages | Unset
        if isinstance(_container_images,  Unset):
            container_images = UNSET
        else:
            container_images = CreateSandboxRequestContainerImages.from_dict(_container_images)




        _labels = d.pop("labels", UNSET)
        labels: CreateSandboxRequestLabels | Unset
        if isinstance(_labels,  Unset):
            labels = UNSET
        else:
            labels = CreateSandboxRequestLabels.from_dict(_labels)




        _annotations = d.pop("annotations", UNSET)
        annotations: CreateSandboxRequestAnnotations | Unset
        if isinstance(_annotations,  Unset):
            annotations = UNSET
        else:
            annotations = CreateSandboxRequestAnnotations.from_dict(_annotations)




        _metadata = d.pop("metadata", UNSET)
        metadata: CreateSandboxRequestMetadata | Unset
        if isinstance(_metadata,  Unset):
            metadata = UNSET
        else:
            metadata = CreateSandboxRequestMetadata.from_dict(_metadata)




        startup_timeout = d.pop("startupTimeout", UNSET)

        idle_timeout = d.pop("idleTimeout", UNSET)

        create_sandbox_request = cls(
            pool_name=pool_name,
            image=image,
            container_images=container_images,
            labels=labels,
            annotations=annotations,
            metadata=metadata,
            startup_timeout=startup_timeout,
            idle_timeout=idle_timeout,
        )


        create_sandbox_request.additional_properties = d
        return create_sandbox_request

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
