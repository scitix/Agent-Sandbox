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
  from ..models.image_pull_secret_input import ImagePullSecretInput
  from ..models.sandbox_network_policy import SandboxNetworkPolicy





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
            network_policy (SandboxNetworkPolicy | Unset): Sandbox egress network policy, enforced by an in-Pod transparent
                proxy sidecar (supports domain matching, which the cluster CNIs cannot). Allowlist / default-deny semantics.
     """

    image: str | Unset = UNSET
    pod_creation_image_policy: EnvOverridesPodCreationImagePolicy | Unset = UNSET
    default_startup_timeout: str | Unset = UNSET
    default_idle_timeout: str | Unset = UNSET
    image_pull_secret: ImagePullSecretInput | Unset = UNSET
    image_pull_secret_configured: bool | Unset = UNSET
    network_policy: SandboxNetworkPolicy | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        from ..models.image_pull_secret_input import ImagePullSecretInput
        from ..models.sandbox_network_policy import SandboxNetworkPolicy
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

        network_policy: dict[str, Any] | Unset = UNSET
        if not isinstance(self.network_policy, Unset):
            network_policy = self.network_policy.to_dict()


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
        if network_policy is not UNSET:
            field_dict["networkPolicy"] = network_policy

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.image_pull_secret_input import ImagePullSecretInput
        from ..models.sandbox_network_policy import SandboxNetworkPolicy
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

        _network_policy = d.pop("networkPolicy", UNSET)
        network_policy: SandboxNetworkPolicy | Unset
        if isinstance(_network_policy,  Unset):
            network_policy = UNSET
        else:
            network_policy = SandboxNetworkPolicy.from_dict(_network_policy)




        env_overrides = cls(
            image=image,
            pod_creation_image_policy=pod_creation_image_policy,
            default_startup_timeout=default_startup_timeout,
            default_idle_timeout=default_idle_timeout,
            image_pull_secret=image_pull_secret,
            image_pull_secret_configured=image_pull_secret_configured,
            network_policy=network_policy,
        )

        return env_overrides

