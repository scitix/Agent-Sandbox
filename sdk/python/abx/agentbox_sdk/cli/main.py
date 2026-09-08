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
`abx` — the AgentBox platform CLI.

Grammar, three tokens deep:

    abx --help                          what can I look at
    abx <resource> --help               its filters and sub-resources
    abx <resource>                      list
    abx <resource> <id>                 one item (brief + sections + hints)
    abx <resource> <id> <section>       a sub-resource list
    abx envs create --name N --template T
    abx pools create --env E --instance-type IT --replicas N
    abx whoami                          who this key authenticates as
    abx agent-context                   the whole CLI's shape, as JSON

Every result trails `view:` / `sections:` / `hint:`, so a caller can chain
without guessing ids. See render.footer.
"""

from __future__ import annotations

import os
import sys
from collections.abc import Sequence
from typing import Any

import agentbox_sdk.cli.kinds  # noqa: F401  (registers every resource)
from agentbox_sdk.cli import dispatch as D
from agentbox_sdk.cli import render as R
from agentbox_sdk.cli.context import (
    ApiError,
    Context,
    assert_cluster_served,
    env_default,
)
from agentbox_sdk.cli.parser import (
    Param,
    UsageError,
    flag_of,
    parse,
    parse_filters,
)
from agentbox_sdk.cli.registry import all_kinds, kind_of_token
from agentbox_sdk.cli.types import NextAction, ResourceKind

# --------------------------------------------------------------------------
# Parameter vocabulary. Declared once; composed per command shape below.
# --------------------------------------------------------------------------
HELP = Param("help", "bool", "Show usage for this command.", aliases=("h",))
ENDPOINT = Param(
    "endpoint", "string", "AgentBox native API base URL.", aliases=("e",)
)
API_KEY = Param("apiKey", "string", "AgentBox API key.", aliases=("k",))
CLUSTER = Param("cluster", "string", "Cluster id this command addresses.")
AS_TEAM = Param("asTeam", "string", "Act as this team (admin keys only).")
AS_USER = Param("asUser", "string", "Act as this user (admin keys only).")
AUTH_SCHEME = Param(
    "authScheme",
    "string",
    "How to present the credential: api-key (a cluster's own API, the default) "
    "or bearer (the dashboard BFF, which routes to every cluster).",
)
HOST_HEADER = Param(
    "hostHeader",
    "string",
    "Host header to send, when --endpoint is an address behind a host-routed "
    "ingress (env AGENTBOX_HOST_HEADER).",
)

FILTER = Param(
    "filter",
    "string[]",
    "Narrow by a declared key: key=value, repeatable, values comma-joined. "
    "An undeclared key fails the command.",
    value_hint="<key=value>",
)
SEARCH = Param("search", "string", "Free-text match across every column.")
LIMIT = Param("limit", "int", "Max rows to print.")
OFFSET = Param("offset", "int", "Skip this many rows.")

FORMAT = Param("format", "string", "Output format: table | json | csv.")
AS_JSON = Param("json", "bool", "Shorthand for --format json.")
AS_CSV = Param("csv", "bool", "Shorthand for --format csv.")
SCHEMA = Param(
    "schema",
    "bool",
    "Describe the SHAPE of --format json (jq paths + types) instead of the "
    "data. Ask for this before piping a large payload into jq.",
)

ENV = Param("env", "string", "Owning environment name.")
NAME = Param("name", "string", "Name to create.")
TEMPLATE = Param("template", "string", "SandboxTemplate to bind.")
TEMPLATE_VERSION = Param(
    "templateVersion", "string", "Pin the template version."
)
MODE = Param("mode", "string", "WarmPool (default) or OnDemandJob.")
GATEWAY = Param(
    "gateway",
    "bool",
    "Enable the egress gateway sidecar on this env's sandboxes.",
)

INSTANCE_TYPE = Param(
    "instanceType", "string", "Instance type from the catalog."
)
MULTIPLIER = Param(
    "multiplier", "int", "Instance-type multiplier (reservation envelope)."
)
REPLICAS = Param("replicas", "int", "Initial replica count.")
MIN_REPLICAS = Param("minReplicas", "int", "Scale-down floor.")
MAX_REPLICAS = Param("maxReplicas", "int", "Scale-up ceiling.")
QUOTA = Param("quota", "string", "Quota URL to charge the reservation against.")

GLOBAL = (
    HELP,
    ENDPOINT,
    API_KEY,
    CLUSTER,
    AS_TEAM,
    AS_USER,
    AUTH_SCHEME,
    HOST_HEADER,
)
LIST_PARAMS = (
    *GLOBAL,
    FILTER,
    SEARCH,
    LIMIT,
    OFFSET,
    FORMAT,
    AS_JSON,
    AS_CSV,
    SCHEMA,
    ENV,
)
GET_PARAMS = (*GLOBAL, FORMAT, AS_JSON, SCHEMA, ENV)
SECTION_PARAMS = (
    *GLOBAL,
    FILTER,
    SEARCH,
    LIMIT,
    OFFSET,
    FORMAT,
    AS_JSON,
    AS_CSV,
    SCHEMA,
    ENV,
)

ENV_CREATE = (
    *GLOBAL,
    NAME,
    TEMPLATE,
    TEMPLATE_VERSION,
    MODE,
    GATEWAY,
    FORMAT,
    AS_JSON,
)
POOL_CREATE = (
    *GLOBAL,
    ENV,
    INSTANCE_TYPE,
    MULTIPLIER,
    REPLICAS,
    MIN_REPLICAS,
    MAX_REPLICAS,
    QUOTA,
    FORMAT,
    AS_JSON,
)

WRITE_VERBS = ("create",)


class Result:
    """What one command produced. `text` is already rendered."""

    def __init__(self, text: str, error: bool = False) -> None:
        self.text = text
        self.error = error


# --------------------------------------------------------------------------
# Help
# --------------------------------------------------------------------------
def _flag_help(params: Sequence[Param]) -> list[str]:
    rows = []
    for p in params:
        spelling = flag_of(p.name)
        if p.aliases:
            spelling += ", " + ", ".join(flag_of(a) for a in p.aliases)
        if p.kind != "bool":
            spelling += " " + (p.value_hint or "<value>")
        rows.append((spelling, p.describe))
    w = max(len(r[0]) for r in rows) if rows else 0
    return [f"  {s.ljust(w)}  {d}" for s, d in rows]


def root_help() -> str:
    out = [
        "abx — the AgentBox platform CLI.",
        "",
        "Platform concepts (environments, pools, templates, quotas) live here.",
        "Sandboxes themselves are created and driven with the E2B SDK.",
        "",
        "Usage:",
        "  abx <resource> [--filter k=v] [--cluster c]",
        "  abx <resource> <id> [<section>]",
        "  abx envs create --name N --template T",
        "  abx pools create --env E --instance-type IT --replicas N",
        "",
        "Resources:",
    ]
    kinds = sorted(all_kinds(), key=lambda k: k.plural)
    w = max(len(k.plural) for k in kinds)
    for k in kinds:
        out.append(f"  {k.plural.ljust(w)}  {k.label}")
    out += [
        "",
        "Other commands:",
        "  whoami          who this key authenticates as (= auth whoami)",
        "  agent-context   the whole CLI's shape, as JSON",
        "",
        "Global flags:",
    ]
    out += _flag_help(GLOBAL)
    out += [
        "",
        "A suggested first pass: `abx templates` -> `abx envs create` -> "
        "`abx quotas` -> `abx pools create`.",
    ]
    return "\n".join(out)


def kind_help(ctx: Context | None, kind: ResourceKind) -> str:
    out = [f"{kind.plural} — {kind.label}", "", kind.describe, "", "Usage:"]
    if kind.can_list:
        out.append(f"  abx {kind.plural} [--filter k=v]")
    if kind.can_get:
        out.append(f"  abx {kind.plural} <{kind.id_hint}>")
    for s in kind.sections:
        out.append(f"  abx {kind.plural} <{kind.id_hint}> {s.id}")
    if kind.kind == "env":
        out.append("  abx envs create --name N --template T [--gateway]")
    if kind.kind == "pool":
        out.append(
            "  abx pools create --env E --instance-type IT "
            "--replicas N [--quota URL]"
        )

    if kind.columns:
        out += ["", "Columns (these are also the --filter keys):"]
        out.append("  " + ", ".join(c.id for c in kind.columns))
    if kind.filters:
        out += ["", "Filters:"]
        rows = [
            (f.key + (f"={'|'.join(f.values)}" if f.values else ""), f.describe)
            for f in kind.filters
        ]
        w = max(len(r[0]) for r in rows)
        out += [f"  {k.ljust(w)}  {d}" for k, d in rows]
    if kind.sections:
        out += ["", "Sub-resources:"]
        w = max(len(s.id) for s in kind.sections)
        out += [f"  {s.id.ljust(w)}  {s.title}" for s in kind.sections]

    # Live candidate values, when a key is available. Cheaper for the caller
    # than guessing and re-running, and it is the reason to prefer help over a
    # guess at all.
    if ctx and kind.can_list:
        try:
            rows = kind.fetch_list(ctx)  # type: ignore[misc]
            keys = [f.key for f in kind.filters] or [c.id for c in kind.columns]
            facet_lines = D.facets(rows, kind.columns, keys)
            if facet_lines:
                out += ["", f"Live values in this cluster ({len(rows)} rows):"]
                out += facet_lines
        except Exception:
            # Help must work without a reachable API.
            pass

    out += ["", "Flags:"]
    out += _flag_help(LIST_PARAMS if kind.can_list else GET_PARAMS)
    return "\n".join(out)


def agent_context() -> str:
    payload: dict[str, Any] = {
        "schemaVersion": "1",
        "tool": "abx",
        "outputFormats": ["table", "json", "csv", "schema"],
        "note": (
            "Platform concepts only. Sandboxes are created with the E2B SDK "
            "(python: `from e2b import Sandbox` after "
            "`from agent_sandbox_e2b import patch_e2b; patch_e2b()`)."
        ),
        "resources": [],
    }
    for k in sorted(all_kinds(), key=lambda x: x.plural):
        payload["resources"].append(
            {
                "resource": k.plural,
                "kind": k.kind,
                "label": k.label,
                "describe": k.describe,
                "capabilities": {
                    "list": k.can_list,
                    "get": k.can_get,
                    "sections": [s.id for s in k.sections],
                },
                "idHint": k.id_hint,
                "columns": [c.id for c in k.columns],
                "filters": [
                    {
                        "key": f.key,
                        "describe": f.describe,
                        "values": list(f.values or ()),
                    }
                    for f in k.filters
                ],
            }
        )
    return R.render_json(payload)


# --------------------------------------------------------------------------
# Shape execution
# --------------------------------------------------------------------------
def _fmt(values: dict[str, Any]) -> str:
    if values.get("json"):
        return "json"
    if values.get("csv"):
        return "csv"
    f = values.get("format")
    if f in (None, ""):
        return "table"
    if f not in ("table", "json", "csv"):
        raise UsageError(f"--format expects table|json|csv, got {f!r}")
    return str(f)


def _schema_of(data: Any) -> str:
    """
    Describe the shape of a payload: jq path + type, inferred from the bytes.

    Inferred rather than declared because a good half of what --format json
    emits is assembled by this CLI (a projected section, a brief with fields
    stripped), so a schema written against the server's spec would describe
    something the caller never receives.
    """
    paths: dict[str, set] = {}

    def walk(node: Any, path: str) -> None:
        if isinstance(node, dict):
            for k, v in node.items():
                walk(v, f"{path}.{k}")
        elif isinstance(node, list):
            paths.setdefault(path + "[]", set()).add("array")
            for v in node[:20]:
                walk(v, path + "[]")
        else:
            t = {
                bool: "boolean",
                int: "number",
                float: "number",
                str: "string",
            }.get(type(node), "null" if node is None else type(node).__name__)
            paths.setdefault(path or ".", set()).add(t)

    walk(data, "")
    if not paths:
        return "(empty output — no shape to describe)"
    w = max(len(p) for p in paths)
    return "\n".join(
        f"{p.ljust(w)}  {'|'.join(sorted(t))}" for p, t in sorted(paths.items())
    )


def run_list(
    ctx: Context, kind: ResourceKind, values: dict[str, Any]
) -> Result:
    if not kind.can_list:
        raise UsageError(f"`abx {kind.plural}` cannot be listed")
    raw_filters = parse_filters(values.get("filter") or [])
    filters = D.validate_filters(kind, raw_filters)

    rows = kind.fetch_list(ctx)  # type: ignore[misc]
    total_fetched = len(rows)
    matched = D.apply_filters(rows, kind.columns, filters)
    if values.get("search"):
        matched = D.search_rows(matched, kind.columns, str(values["search"]))
    shown, start, matched_n = D.page(
        matched, values.get("limit"), values.get("offset")
    )

    fmt = _fmt(values)
    if values.get("schema"):
        return Result(_schema_of({"items": shown}))
    if fmt == "json":
        return Result(R.render_json({"items": shown, "total": matched_n}))
    if fmt == "csv":
        return Result(R.render_csv(kind.columns, shown))

    head = f"{kind.kind}"
    if ctx.cluster:
        head += f" · {ctx.cluster}"
    if getattr(ctx, "env", None) and kind.kind == "pool":
        head += f" · env={ctx.env}"
    head += f" · {matched_n} total"
    if shown and matched_n > len(shown):
        head += f" · rows {start + 1}–{start + len(shown)}"

    out = [head]
    if filters:
        out.append(
            "filters: "
            + " ".join(f"{k}={','.join(v)}" for k, v in filters.items())
        )
    out.append("")
    body = R.table(kind.columns, shown)
    if body:
        out += body
    else:
        out.append("(no rows matched)")
        if total_fetched:
            keys = [f.key for f in kind.filters] or [c.id for c in kind.columns]
            facet_lines = D.facets(rows, kind.columns, keys)
            if facet_lines:
                out += ["", f"available filter values ({total_fetched} rows):"]
                out += facet_lines

    if matched_n > start + len(shown):
        out.append(
            f"… {matched_n - start - len(shown)} more — re-run with "
            f"--offset {start + len(shown)} (keep your other flags)"
        )

    hints = list(kind.next_list(ctx, shown)) if kind.next_list else []
    hints.append(
        NextAction(
            cmd=f"abx {kind.plural} --help"
            + (f" --cluster {ctx.cluster}" if ctx.cluster else ""),
            reason="filters, columns and live values",
        )
    )
    view = kind.view_list(ctx, filters) if kind.view_list else None
    trailer = R.footer(
        view=view, ui_mode=ctx.ui_mode, web_base=ctx.web_base, hints=hints
    )
    if trailer:
        out += ["", *trailer]
    return Result("\n".join(out))


def run_get(
    ctx: Context, kind: ResourceKind, ident: str, values: dict[str, Any]
) -> Result:
    if not kind.can_get:
        raise UsageError(f"`abx {kind.plural}` has no detail view")
    body = kind.fetch_get(ctx, ident)  # type: ignore[misc]

    fmt = _fmt(values)
    if values.get("schema"):
        return Result(_schema_of(body))
    if fmt == "json":
        return Result(R.render_json(body))

    head = f"{kind.kind}"
    if ctx.cluster:
        head += f" · {ctx.cluster}"
    head += f" · {ident}"
    out = [head, "", *R.yaml_lite(body)]

    sections = [{"id": s.id, "title": s.title} for s in kind.sections]
    section_open = (
        f"abx {kind.plural} {ident} <section>"
        + (f" --cluster {ctx.cluster}" if ctx.cluster else "")
        if sections
        else ""
    )
    hints = list(kind.next_get(ctx, ident, body)) if kind.next_get else []
    view = kind.view_get(ctx, ident) if kind.view_get else None
    trailer = R.footer(
        view=view,
        ui_mode=ctx.ui_mode,
        web_base=ctx.web_base,
        sections=sections,
        section_open=section_open,
        hints=hints,
    )
    if trailer:
        out += ["", *trailer]
    return Result("\n".join(out))


def run_section(
    ctx: Context,
    kind: ResourceKind,
    ident: str,
    sec_id: str,
    values: dict[str, Any],
) -> Result:
    section = next((s for s in kind.sections if s.id == sec_id), None)
    if section is None:
        known = ", ".join(s.id for s in kind.sections) or "(none)"
        raise UsageError(
            f'unknown sub-resource "{sec_id}" for {kind.plural}. '
            f"It has: {known}.",
            [f"abx {kind.plural} {ident}"],
        )
    raw_filters = parse_filters(values.get("filter") or [])
    filters = D.validate_filters(
        kind,
        raw_filters,
        columns=section.columns,
        where=f'sub-resource "{sec_id}" of {kind.plural}',
    )
    rows = section.fetch(ctx, ident)
    matched = D.apply_filters(rows, section.columns, filters)
    if values.get("search"):
        matched = D.search_rows(matched, section.columns, str(values["search"]))
    shown, start, matched_n = D.page(
        matched, values.get("limit"), values.get("offset")
    )

    fmt = _fmt(values)
    if values.get("schema"):
        return Result(_schema_of({"items": shown}))
    if fmt == "json":
        return Result(R.render_json({"items": shown, "total": matched_n}))
    if fmt == "csv":
        return Result(R.render_csv(section.columns, shown))

    out = [
        f"{kind.kind}"
        + (f" · {ctx.cluster}" if ctx.cluster else "")
        + f" · {ident}",
        "",
        f"[{section.id}] {section.title} · {matched_n} total",
    ]
    body = R.table(section.columns, shown)
    out += body if body else ["(no rows)"]
    if matched_n > start + len(shown):
        out.append(
            f"… {matched_n - start - len(shown)} more — re-run with "
            f"--offset {start + len(shown)}"
        )
    sections = [{"id": s.id, "title": s.title} for s in kind.sections]
    trailer = R.footer(
        view=kind.view_get(ctx, ident) if kind.view_get else None,
        ui_mode=ctx.ui_mode,
        web_base=ctx.web_base,
        sections=sections,
        section_open=f"abx {kind.plural} {ident} <section>"
        + (f" --cluster {ctx.cluster}" if ctx.cluster else ""),
    )
    if trailer:
        out += ["", *trailer]
    return Result("\n".join(out))


# --------------------------------------------------------------------------
# Writes
# --------------------------------------------------------------------------
def create_env(ctx: Context, values: dict[str, Any]) -> Result:
    name = values.get("name")
    template = values.get("template")
    missing = [
        f for f, v in (("--name", name), ("--template", template)) if not v
    ]
    if missing:
        raise UsageError(
            f"envs create needs {' and '.join(missing)}",
            [
                "abx templates"
                + (f" --cluster {ctx.cluster}" if ctx.cluster else "")
            ],
        )
    body: dict[str, Any] = {
        "name": name,
        "templateRef": {"name": template},
    }
    if values.get("templateVersion"):
        body["templateRef"]["version"] = values["templateVersion"]
    if values.get("mode"):
        body["mode"] = values["mode"]
    if values.get("gateway"):
        # The gateway is an Env-level switch because it changes the Pod spec.
        # Credential-injection rules are per sandbox and are refused outright
        # against an env without it, so this is what makes them possible later.
        body["overrides"] = {"gateway": {"enabled": True}}

    payload = ctx.post_json("/envs", body)
    if _fmt(values) == "json":
        return Result(R.render_json(payload))
    out = [f"created env {name} from template {template}", ""]
    out += R.yaml_lite(
        payload if isinstance(payload, dict) else {"result": payload}
    )
    hints = [
        NextAction(
            cmd=f"abx quotas{_cl(ctx)}",
            reason="what this team may charge against",
        ),
        NextAction(
            cmd=f"abx instancetypes{_cl(ctx)}",
            reason="pick a size for the pool",
        ),
        NextAction(
            cmd=f"abx pools create --env {name} "
            f"--instance-type <it> --replicas 1{_cl(ctx)}",
            reason="add the warm pool that actually holds Pods",
        ),
    ]
    trailer = R.footer(
        view={
            "page": "env_detail",
            "cluster": ctx.cluster,
            "params": {"name": name},
        },
        ui_mode=ctx.ui_mode,
        web_base=ctx.web_base,
        hints=hints,
    )
    return Result("\n".join([*out, "", *trailer]))


def create_pool(ctx: Context, values: dict[str, Any]) -> Result:
    env = values.get("env")
    if not env:
        raise UsageError(
            "pools create needs --env <name>", [f"abx envs{_cl(ctx)}"]
        )
    it = values.get("instanceType")
    if not it:
        raise UsageError(
            "pools create needs --instance-type <name>",
            [f"abx instancetypes{_cl(ctx)}"],
        )
    body: dict[str, Any] = {"instanceType": it}
    for key, field in (
        ("multiplier", "multiplier"),
        ("replicas", "replicas"),
        ("minReplicas", "minReplicas"),
        ("maxReplicas", "maxReplicas"),
    ):
        if values.get(key) is not None:
            body[field] = values[key]
    if values.get("quota"):
        # The quota is selected by a label the server parses, not by a field.
        body["labels"] = {"quota.scitix.ai/url": values["quota"]}

    payload = ctx.post_json(f"/envs/{env}/sandboxpools", body)
    if _fmt(values) == "json":
        return Result(R.render_json(payload))
    out = [f"created pool in env {env} (instanceType={it})", ""]
    out += R.yaml_lite(
        payload if isinstance(payload, dict) else {"result": payload}
    )
    hints = [
        NextAction(
            cmd=f"abx envs {env} pools{_cl(ctx)}", reason="watch it come up"
        ),
        NextAction(
            cmd=f"abx envs {env}{_cl(ctx)}",
            reason="the environment's overall state",
        ),
    ]
    trailer = R.footer(
        view={
            "page": "env_detail",
            "cluster": ctx.cluster,
            "params": {"name": env},
        },
        ui_mode=ctx.ui_mode,
        web_base=ctx.web_base,
        hints=hints,
    )
    return Result("\n".join([*out, "", *trailer]))


def _cl(ctx: Context) -> str:
    return f" --cluster {ctx.cluster}" if ctx.cluster else ""


_WHOAMI_HELP = """abx whoami (= abx auth whoami)
— who this key authenticates as.

Prints the role, user and team the credential resolves to. Everything else the
CLI shows is scoped to that identity: `quotas` is your quota, `envs` and `pools`
are the ones in your namespace. Run it first when you need to know whose view
you are looking at."""


def run_whoami(ctx: Context, values: dict[str, Any]) -> Result:
    payload = ctx.get_json("/auth/whoami")
    if _fmt(values) == "json":
        return Result(R.render_json(payload))
    return Result("\n".join(R.yaml_lite(payload)))


# --------------------------------------------------------------------------
# Entry
# --------------------------------------------------------------------------
def _build_ctx(values: dict[str, Any]) -> Context:
    endpoint = str(values.get("endpoint") or env_default("AGENTBOX_ENDPOINT"))
    api_key = str(values.get("apiKey") or env_default("AGENTBOX_API_KEY"))
    if not endpoint:
        raise UsageError(
            "no endpoint: pass --endpoint or set AGENTBOX_ENDPOINT",
        )
    if not api_key:
        raise UsageError("no API key: pass --api-key or set AGENTBOX_API_KEY")
    ctx = Context(
        endpoint=endpoint,
        api_key=api_key,
        cluster=str(values.get("cluster") or env_default("ABX_CLUSTER"))
        or None,
        impersonate_team=str(values.get("asTeam") or env_default("ABX_AS_TEAM"))
        or None,
        impersonate_user=str(values.get("asUser") or env_default("ABX_AS_USER"))
        or None,
        auth_scheme=(
            str(values.get("authScheme") or env_default("AGENTBOX_AUTH_SCHEME"))
            or "api-key"
        ),
        ui_mode=env_default("ABX_UI_MODE", fallback="url"),
        web_base=env_default("ABX_WEB_BASE") or None,
        host_header=str(
            values.get("hostHeader") or env_default("AGENTBOX_HOST_HEADER")
        )
        or None,
    )
    ctx.env = values.get("env")  # type: ignore[attr-defined]
    return ctx


def _params_for(argv: Sequence[str]) -> Sequence[Param]:
    """
    The union of every shape's parameters, for the first parse.

    The exact shape is not known until the positionals are read, so this parse
    is permissive across shapes but still strict about existence: an
    undeclared flag fails here, before its value can be read as an id.
    """
    return tuple(
        dict.fromkeys(
            (
                *LIST_PARAMS,
                *GET_PARAMS,
                *SECTION_PARAMS,
                *ENV_CREATE,
                *POOL_CREATE,
            )
        )
    )


def run(argv: Sequence[str]) -> Result:
    argv = list(argv)
    if argv and argv[0] == "abx":
        argv = argv[1:]  # so a copy-pasted hint runs verbatim

    parsed = parse(argv, _params_for(argv))
    values = parsed.values
    positionals = parsed.positionals
    want_help = bool(values.get("help"))

    if not positionals:
        return Result(root_help())

    first = positionals[0]

    if first == "agent-context":
        return Result(agent_context())

    # Both spellings. `abx whoami` is what a person reaches for and what the
    # runtime prompt tells the agent to run; `abx auth whoami` groups it with a
    # namespace that may grow other subcommands.
    if first == "whoami":
        if want_help:
            return Result(_WHOAMI_HELP)
        return run_whoami(_build_ctx(values), values)

    if first == "auth":
        sub = positionals[1] if len(positionals) > 1 else "whoami"
        if sub != "whoami":
            raise UsageError(
                f'unknown auth subcommand "{sub}". Only `whoami` exists.'
            )
        if want_help:
            return Result(_WHOAMI_HELP)
        return run_whoami(_build_ctx(values), values)

    kind = kind_of_token(first)
    if kind is None:
        known = ", ".join(sorted(k.plural for k in all_kinds()))
        raise UsageError(
            f'unknown resource "{first}". Resources: {known}.', ["abx --help"]
        )

    rest = positionals[1:]
    verb = rest[0] if rest and rest[0] in WRITE_VERBS else None

    if want_help and verb is None:
        # Cluster-scoped help reaches the API for live values; it must still
        # render without one.
        try:
            ctx = _build_ctx(values)
        except UsageError:
            ctx = None
        return Result(kind_help(ctx, kind))

    ctx = _build_ctx(values)
    # Before any read or write: a --cluster this endpoint cannot answer for is
    # refused rather than silently answered from the local one.
    assert_cluster_served(ctx)

    if verb == "create":
        if kind.kind == "env":
            return create_env(ctx, values)
        if kind.kind == "pool":
            return create_pool(ctx, values)
        raise UsageError(f"`abx {kind.plural} create` is not supported")

    if len(rest) == 0:
        return run_list(ctx, kind, values)
    if len(rest) == 1:
        return run_get(ctx, kind, rest[0], values)
    if len(rest) == 2:
        return run_section(ctx, kind, rest[0], rest[1], values)
    raise UsageError(
        "the grammar is three tokens deep: <resource> <id> <section>",
        [f"abx {kind.plural} {rest[0]}"],
    )


def main(argv: Sequence[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    try:
        result = run(args)
        text = result.text
        code = 1 if result.error else 0
    except UsageError as e:
        lines = [f"error: {e}"]
        if e.hints:
            lines += ["hint:"] + [f"  {h}" for h in e.hints]
        text, code = "\n".join(lines), 2
    except ApiError as e:
        # Relayed verbatim: the server's wording is what the caller can act on.
        text, code = f"error: {e}", 1
    except KeyboardInterrupt:
        return 130

    _write_out(text)
    return code


def _write_out(text: str) -> None:
    """
    Write, then flush before returning.

    sys.stdout is block-buffered when it is a pipe, so output not yet drained
    at interpreter exit is lost. CPython's own shutdown normally flushes it —
    the flush here is what makes that independent of how the process ends, and
    of a buffering policy the CLI does not control. A silently truncated table
    is the failure this guards: it reads as "that is all there is" rather than
    as an error, so a caller (a person or an agent) reports the short answer as
    the whole one.
    """
    try:
        sys.stdout.write(text + "\n")
        sys.stdout.flush()
    except BrokenPipeError:
        # `abx ... | head` closes the pipe early. Devnull the fd so the
        # interpreter's own shutdown flush cannot re-raise on it.
        try:
            devnull = os.open(os.devnull, os.O_WRONLY)
            os.dup2(devnull, sys.stdout.fileno())
        except Exception:
            pass


if __name__ == "__main__":
    sys.exit(main())
