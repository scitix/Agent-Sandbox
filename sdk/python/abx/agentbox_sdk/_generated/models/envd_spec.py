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






T = TypeVar("T", bound="EnvdSpec")



@_attrs_define
class EnvdSpec:
    """ Settings for the sandbox agent (envd) that serves the E2B API inside every
    sandbox Pod.

        Attributes:
            verbose (bool | Unset): Write one structured line per API call envd serves to the container's stdout —
                for a command, the command itself, its arguments, working directory and
                environment. It is how a deployment gets a record of what agents actually ran
                inside a sandbox, since a command's output otherwise only travels back to its
                caller. Defaults to on; a GET always reports the value in force.

                Two costs. A PTY session sends one API call per keystroke and each is logged
                with its whole request, so an interactive terminal produces a lot of lines and
                pays a serialization cost on a hot path. And environment variable VALUES appear
                in the log — anything secret belongs in a Secret the sandbox reads, or in the
                egress credential injection path, not in a plain environment variable.
     """

    verbose: bool | Unset = UNSET





    def to_dict(self) -> dict[str, Any]:
        verbose = self.verbose


        field_dict: dict[str, Any] = {}

        field_dict.update({
        })
        if verbose is not UNSET:
            field_dict["verbose"] = verbose

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        verbose = d.pop("verbose", UNSET)

        envd_spec = cls(
            verbose=verbose,
        )

        return envd_spec

