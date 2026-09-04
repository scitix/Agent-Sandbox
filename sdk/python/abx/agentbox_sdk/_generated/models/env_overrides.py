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

from ..models.env_overrides_pod_creation_image_policy import EnvOverridesPodCreationImagePolicy
from ..types import UNSET, Unset
from typing import cast

if TYPE_CHECKING:
  from ..models.env_update_strategy import EnvUpdateStrategy
  from ..models.env_volume_mount import EnvVolumeMount
  from ..models.gateway_spec import GatewaySpec
  from ..models.image_pull_secret_input import ImagePullSecretInput





T = TypeVar("T", bound="EnvOverrides")



@_attrs_define
class EnvOverrides:
    """ SandboxTemplate fields this Env replaces uniformly for every member Pool. The Env represents a single class of
    sandbox runtime, so image, image policy, default timeouts and image-pull credentials are expected to be shared; per-
    Pool variation lives on each EnvClusterMember.

        Attributes:
            image (str | Unset): Override the main container (containers[0]) image of the rendered Template. Applied before
                any per-Member overrides.
            pod_creation_image_policy (EnvOverridesPodCreationImagePolicy | Unset): Mirrored onto every member Pool's
                spec.podCreationImagePolicy.
            default_startup_timeout (str | Unset): Mirrored onto every member Pool's spec.defaultStartupTimeout. Duration
                string, e.g. '5m'.
            default_idle_timeout (str | Unset): Mirrored onto every member Pool's spec.defaultIdleTimeout. Duration string,
                e.g. '30m'.
            image_pull_secret (ImagePullSecretInput | Unset):
            image_pull_secret_configured (bool | Unset): Server-set on GET: true when the ips-{envName} Secret exists in the
                Env's namespace. Write attempts via PATCH are ignored.
            gateway (GatewaySpec | Unset): Egress gateway switch. Enabling it adds a transparent proxy sidecar and an
                iptables redirect to every sandbox Pod of the environment; the rules it
                enforces are supplied per sandbox on the create call.
            update_strategy (EnvUpdateStrategy | Unset): Automatic rollout policy for member Pools when their rendered idle-
                Pod identity (Template edit, image / gateway override) changes. Rollout mode is always Recreate: stale idle Pods
                are rebuilt; claimed (Running/Starting) Pods are never disrupted and roll after returning to Idle.
            volumes (list[EnvVolumeMount] | Unset): Mount existing PersistentVolumeClaims from this Env's namespace into the
                sandbox container. The claim must already exist and be Bound; the server
                never creates or deletes a PVC. Discover mountable claims with GET /volumes.

                Mounts are fixed at Pod creation — Kubernetes forbids mutating spec.volumes
                on a live Pod — so editing this list rolls the member Pools' idle Pods.
                Sandboxes already running keep their previous mounts until they are
                returned. In-place image upgrades never touch volumes.
     """

    image: str | Unset = UNSET
    pod_creation_image_policy: EnvOverridesPodCreationImagePolicy | Unset = UNSET
    default_startup_timeout: str | Unset = UNSET
    default_idle_timeout: str | Unset = UNSET
    image_pull_secret: ImagePullSecretInput | Unset = UNSET
    image_pull_secret_configured: bool | Unset = UNSET
    gateway: GatewaySpec | Unset = UNSET
    update_strategy: EnvUpdateStrategy | Unset = UNSET
    volumes: list[EnvVolumeMount] | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        from ..models.env_update_strategy import EnvUpdateStrategy # noqa: PLC0415
        from ..models.env_volume_mount import EnvVolumeMount # noqa: PLC0415
        from ..models.gateway_spec import GatewaySpec # noqa: PLC0415
        from ..models.image_pull_secret_input import ImagePullSecretInput # noqa: PLC0415
        image = self.image

        pod_creation_image_policy: str | Unset = UNSET
        if not isinstance(self.pod_creation_image_policy, Unset):
            pod_creation_image_policy = self.pod_creation_image_policy.value


        default_startup_timeout = self.default_startup_timeout

        default_idle_timeout = self.default_idle_timeout

        image_pull_secret: dict[str, Any] | Unset = UNSET
        if not isinstance(self.image_pull_secret, Unset):
            image_pull_secret = self.image_pull_secret.to_dict()

        image_pull_secret_configured = self.image_pull_secret_configured

        gateway: dict[str, Any] | Unset = UNSET
        if not isinstance(self.gateway, Unset):
            gateway = self.gateway.to_dict()

        update_strategy: dict[str, Any] | Unset = UNSET
        if not isinstance(self.update_strategy, Unset):
            update_strategy = self.update_strategy.to_dict()

        volumes: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.volumes, Unset):
            volumes = []
            for volumes_item_data in self.volumes:
                volumes_item = volumes_item_data.to_dict()
                volumes.append(volumes_item)




        field_dict: dict[str, Any] = {}

        field_dict.update({
        })
        if image is not UNSET:
            field_dict["image"] = image
        if pod_creation_image_policy is not UNSET:
            field_dict["podCreationImagePolicy"] = pod_creation_image_policy
        if default_startup_timeout is not UNSET:
            field_dict["defaultStartupTimeout"] = default_startup_timeout
        if default_idle_timeout is not UNSET:
            field_dict["defaultIdleTimeout"] = default_idle_timeout
        if image_pull_secret is not UNSET:
            field_dict["imagePullSecret"] = image_pull_secret
        if image_pull_secret_configured is not UNSET:
            field_dict["imagePullSecretConfigured"] = image_pull_secret_configured
        if gateway is not UNSET:
            field_dict["gateway"] = gateway
        if update_strategy is not UNSET:
            field_dict["updateStrategy"] = update_strategy
        if volumes is not UNSET:
            field_dict["volumes"] = volumes

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.env_update_strategy import EnvUpdateStrategy # noqa: PLC0415
        from ..models.env_volume_mount import EnvVolumeMount # noqa: PLC0415
        from ..models.gateway_spec import GatewaySpec # noqa: PLC0415
        from ..models.image_pull_secret_input import ImagePullSecretInput # noqa: PLC0415
        d = dict(src_dict)
        image = d.pop("image", UNSET)

        _pod_creation_image_policy = d.pop("podCreationImagePolicy", UNSET)
        pod_creation_image_policy: EnvOverridesPodCreationImagePolicy | Unset
        if isinstance(_pod_creation_image_policy,  Unset):
            pod_creation_image_policy = UNSET
        else:
            pod_creation_image_policy = EnvOverridesPodCreationImagePolicy(_pod_creation_image_policy)




        default_startup_timeout = d.pop("defaultStartupTimeout", UNSET)

        default_idle_timeout = d.pop("defaultIdleTimeout", UNSET)

        _image_pull_secret = d.pop("imagePullSecret", UNSET)
        image_pull_secret: ImagePullSecretInput | Unset
        if isinstance(_image_pull_secret,  Unset):
            image_pull_secret = UNSET
        else:
            image_pull_secret = ImagePullSecretInput.from_dict(_image_pull_secret)




        image_pull_secret_configured = d.pop("imagePullSecretConfigured", UNSET)

        _gateway = d.pop("gateway", UNSET)
        gateway: GatewaySpec | Unset
        if isinstance(_gateway,  Unset):
            gateway = UNSET
        else:
            gateway = GatewaySpec.from_dict(_gateway)




        _update_strategy = d.pop("updateStrategy", UNSET)
        update_strategy: EnvUpdateStrategy | Unset
        if isinstance(_update_strategy,  Unset):
            update_strategy = UNSET
        else:
            update_strategy = EnvUpdateStrategy.from_dict(_update_strategy)




        _volumes = d.pop("volumes", UNSET)
        volumes: list[EnvVolumeMount] | Unset = UNSET
        if _volumes is not UNSET:
            volumes = []
            for volumes_item_data in _volumes:
                volumes_item = EnvVolumeMount.from_dict(volumes_item_data)



                volumes.append(volumes_item)


        env_overrides = cls(
            image=image,
            pod_creation_image_policy=pod_creation_image_policy,
            default_startup_timeout=default_startup_timeout,
            default_idle_timeout=default_idle_timeout,
            image_pull_secret=image_pull_secret,
            image_pull_secret_configured=image_pull_secret_configured,
            gateway=gateway,
            update_strategy=update_strategy,
            volumes=volumes,
        )

        return env_overrides

