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






T = TypeVar("T", bound="ErrorResponse")



@_attrs_define
class ErrorResponse:
    """ Standard error envelope. Individual endpoints override the `example` per response
    code so the shape of `detail` is accurate for that specific failure mode.

        Attributes:
            error (str): Human-readable error message.
            error_code (str | Unset): Machine-readable business error code. Only present for specific business errors
                that require special handling (e.g. `API_KEY_REQUIRED` prompts the client to
                call `POST /api-keys` before retrying). Generic HTTP errors (400, 500, etc.) do
                NOT carry this field.
                 Example: API_KEY_REQUIRED.
            detail (Any | Unset): Structured context for the error. Shape depends on the failure mode. Common
                variants:
                  * Plain string — simple context ("pool not found: my-pool")
                  * `PoolStatusDetail` — returned on 409/429 from CreateSandbox to describe
                    pool replica breakdown and retry hints
                  * `{availablePools: [...]}` — returned on 404 from CreateSandbox / 400 from
                    CreateSandboxPool when the referenced resource does not exist
                  * `{availableTemplates: [...]}` — returned on 400 from CreateSandboxPool
                  * `{availableQuotaURLs: [...]}` — returned on 400 when spec.reservation is
                    set but quotaURL is missing
     """

    error: str
    error_code: str | Unset = UNSET
    detail: Any | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)





    def to_dict(self) -> dict[str, Any]:
        error = self.error

        error_code = self.error_code

        detail = self.detail


        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update({
            "error": error,
        })
        if error_code is not UNSET:
            field_dict["errorCode"] = error_code
        if detail is not UNSET:
            field_dict["detail"] = detail

        return field_dict



    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        error = d.pop("error")

        error_code = d.pop("errorCode", UNSET)

        detail = d.pop("detail", UNSET)

        error_response = cls(
            error=error,
            error_code=error_code,
            detail=detail,
        )


        error_response.additional_properties = d
        return error_response

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
