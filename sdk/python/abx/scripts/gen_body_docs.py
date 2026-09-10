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
Generate the `-f` body reference the CLI prints, from the OpenAPI spec.

`abx envs create -f` takes the API's own request body, so the only honest place
to describe that body is the schema that defines it. Written by hand, the
description is a copy — and a copy of a schema is a thing that silently stops
being true, which is the failure this file exists to make impossible.

Run through `make gen`, beside the SDK generation that reads the same spec.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any

import yaml

# Which request body backs which `abx <kind> create`.
BODIES = {
    "env": ("CreateSandboxEnvRequest", "/envs"),
    "pool": ("CreateEnvSandboxPoolRequest", "/envs/{name}/sandboxpools"),
}

# How far to expand a referenced object when documenting its fields. One level
# covers templateRef and inlineResources — the shapes people actually have to
# type — without unrolling the whole Env override tree into a help screen.
NEST_DEPTH = 1


def main(spec_path: str, out_path: str) -> int:
    spec = yaml.safe_load(Path(spec_path).read_text(encoding="utf-8"))
    schemas = spec["components"]["schemas"]

    blocks = []
    for kind, (schema_name, path) in BODIES.items():
        schema = schemas[schema_name]
        fields = _fields(schema, schemas)
        nested = {
            name: _fields(_deref(prop, schemas), schemas)
            for name, prop in (schema.get("properties") or {}).items()
            if _is_object(_deref(prop, schemas))
        }
        blocks.append(
            (kind, schema_name, path, fields, nested, _example(schema, schemas))
        )

    Path(out_path).write_text(_render(spec_path, blocks), encoding="utf-8")
    return 0


def _deref(node: Any, schemas: dict[str, Any]) -> dict[str, Any]:
    """Follow $ref / a one-element allOf to the schema with properties."""
    seen = 0
    while isinstance(node, dict) and seen < 10:
        seen += 1
        if "$ref" in node:
            node = schemas.get(node["$ref"].rsplit("/", 1)[-1], {})
            continue
        if "allOf" in node and len(node["allOf"]) == 1:
            node = node["allOf"][0]
            continue
        break
    return node if isinstance(node, dict) else {}


def _is_object(schema: dict[str, Any]) -> bool:
    return bool(schema.get("properties"))


def _type_of(prop: dict[str, Any], schemas: dict[str, Any]) -> str:
    ref = prop.get("$ref") or (
        prop["allOf"][0].get("$ref")
        if len(prop.get("allOf") or []) == 1
        else None
    )
    if ref:
        return ref.rsplit("/", 1)[-1]
    t = prop.get("type", "")
    if t == "array":
        return f"{_type_of(prop.get('items') or {}, schemas)}[]"
    if t == "object" and prop.get("additionalProperties"):
        return "map"
    if prop.get("enum"):
        return " | ".join(str(v) for v in prop["enum"])
    return t or "any"


def _fields(
    schema: dict[str, Any], schemas: dict[str, Any]
) -> list[tuple[str, str, bool, str]]:
    required = set(schema.get("required") or ())
    out = []
    for name, prop in (schema.get("properties") or {}).items():
        target = _deref(prop, schemas)
        # A property's own description wins; a bare $ref borrows the
        # referenced schema's, which is where the prose lives for those.
        describe = prop.get("description") or target.get("description") or ""
        out.append(
            (
                name,
                _type_of(prop, schemas),
                name in required,
                " ".join(describe.split()),
            )
        )
    return out


def _example(
    schema: dict[str, Any], schemas: dict[str, Any], depth: int = 0
) -> str:
    """The body to show, taken from the spec where the spec offers one.

    An `example:` on the schema is authored once, in the file that already
    defines the shape, and reaches every consumer — this help, the Swagger
    page, the generated SDK. Writing a nicer example here instead would put a
    second description of the same object in a second place, which is the whole
    thing this generator exists to avoid.

    Without one, a skeleton of the required fields: correct by construction,
    and short enough to be honest about how little it says.
    """
    if "example" in schema:
        return json.dumps(schema["example"], indent=2, ensure_ascii=False)
    return json.dumps(
        _sample(schema, schemas, depth), indent=2, ensure_ascii=False
    )


def _sample(schema: dict[str, Any], schemas: dict[str, Any], depth: int) -> Any:
    if _is_object(schema):
        required = set(schema.get("required") or ())
        props = schema.get("properties") or {}
        # Required only — except at the very top of an object that requires
        # nothing, where an empty {} would teach nothing at all.
        keys = [k for k in props if k in required] or (
            list(props)[:3] if depth == 0 else []
        )
        return {
            k: _sample(_deref(props[k], schemas), schemas, depth + 1)
            for k in keys
        }
    if schema.get("enum"):
        return schema.get("default", schema["enum"][0])
    if "default" in schema:
        return schema["default"]
    t = schema.get("type")
    if t == "integer":
        return 1
    if t == "boolean":
        return True
    if t == "array":
        return []
    if t == "object":
        return {}
    return "…"


def _render(spec_path: str, blocks: list[Any]) -> str:
    lines = [
        "# Code generated from the OpenAPI spec. DO NOT EDIT.",
        f"#   source: {Path(spec_path).name}",
        "#   regenerate: make -C sdk/python/abx gen",
        "#",
        "# The `-f` body is the API's own request. Describing it anywhere but",
        "# the schema would be describing a copy, and a copy of a schema stops",
        "# being true without anyone noticing.",
        "",
        "from __future__ import annotations",
        "",
        "from typing import Final",
        "",
        "# kind -> schema name, POST path, fields, nested tables,",
        "#         example",
        "# field = (wire name, type, required, description)",
        "BODY_DOCS: Final[dict[str, dict[str, object]]] = {",
    ]
    for kind, schema_name, path, fields, nested, example in blocks:
        lines.append(f"    {kind!r}: {{")
        lines.append(f"        'schema': {schema_name!r},")
        lines.append(f"        'path': {path!r},")
        lines.append("        'fields': (")
        for f in fields:
            lines.append(f"            {f!r},")
        lines.append("        ),")
        lines.append("        'nested': {")
        for name, sub in sorted(nested.items()):
            lines.append(f"            {name!r}: (")
            for f in sub:
                lines.append(f"                {f!r},")
            lines.append("            ),")
        lines.append("        },")
        lines.append(f"        'example': {example!r},")
        lines.append("    },")
    lines += ["}", ""]
    return "\n".join(lines)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1], sys.argv[2]))
