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

"""
The declarative contract every `abx` resource is described by.

One `ResourceKind` per resource, registered once. Help text, filter validation
and the machine-readable surface are all derived from the registry, so a
resource cannot appear in one of them and be missing from another.
"""

from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass, field
from typing import Any, Callable

Row = dict[str, Any]
Ctx = Any  # cli.context.Context — avoids a circular import


@dataclass(frozen=True)
class Column:
    """
    One table column.

    `id` is BOTH the header and the `--filter` key. There is deliberately no
    separate display title: a second name would be one more thing to reconcile
    against the filter vocabulary, and the header a caller reads is the string
    they then have to type.
    """

    id: str
    get: Callable[[Row], Any]
    width: int = 0  # 0 = size to content


@dataclass(frozen=True)
class FilterSpec:
    key: str
    describe: str
    values: Sequence[str] | None = None  # enum, when the set is closed


@dataclass(frozen=True)
class NextAction:
    """A ready-to-run follow-up command, rendered into the `hint:` block."""

    cmd: str
    reason: str


@dataclass(frozen=True)
class Section:
    """A sub-resource of one item, reached as `abx <plural> <id> <section>`."""

    id: str
    title: str
    fetch: Callable[[Ctx, str], list[Row]]
    columns: Sequence[Column]


@dataclass(frozen=True)
class ResourceKind:
    kind: str  # canonical singular, e.g. "env"
    plural: str  # the CLI token, e.g. "envs"
    label: str
    describe: str

    columns: Sequence[Column] = ()
    id_of: Callable[[Row], str] | None = None
    fetch_list: Callable[[Ctx], list[Row]] | None = None
    filters: Sequence[FilterSpec] = ()

    fetch_get: Callable[[Ctx, str], Row] | None = None
    id_hint: str = "id"
    sections: Sequence[Section] = ()

    # Abstract view targets. `finalize` is the only thing that renders them,
    # so a surface change lands in one place rather than in every kind.
    view_list: Callable[[Ctx, dict[str, list[str]]], dict[str, Any]] | None = (
        None
    )
    view_get: Callable[[Ctx, str], dict[str, Any]] | None = None

    next_list: Callable[[Ctx, list[Row]], list[NextAction]] | None = None
    next_get: Callable[[Ctx, str, Row], list[NextAction]] | None = None

    extra_params: Sequence[str] = field(default_factory=tuple)

    @property
    def can_list(self) -> bool:
        return self.fetch_list is not None

    @property
    def can_get(self) -> bool:
        return self.fetch_get is not None
