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

"""The resource registry: one entry per kind, filled by side-effect import."""

from __future__ import annotations

from agentbox_sdk.cli.types import ResourceKind

_REGISTRY: dict[str, ResourceKind] = {}


def register_kind(spec: ResourceKind) -> None:
    """
    Register one resource.

    A duplicate raises rather than overwriting: two definitions of a kind means
    one of them is silently unreachable, and which one wins would depend on
    import order.
    """
    if spec.kind in _REGISTRY:
        raise RuntimeError(f"duplicate resource kind: {spec.kind}")
    _REGISTRY[spec.kind] = spec


def get_kind(kind: str) -> ResourceKind | None:
    return _REGISTRY.get(kind)


def all_kinds() -> list[ResourceKind]:
    return list(_REGISTRY.values())


def kind_of_token(token: str) -> ResourceKind | None:
    """Resolve a CLI token (plural or singular) to its kind."""
    for k in _REGISTRY.values():
        if token in (k.plural, k.kind):
            return k
    return None
