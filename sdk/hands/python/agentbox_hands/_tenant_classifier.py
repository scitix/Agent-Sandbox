"""Topic-switch classifier — tenant-specific, and not part of the hands contract.

This does not belong in a package whose job is confining an agent's file and shell
tools to a sandbox. It is here because it shipped inside the same FastAPI app as the
workspace file API and was carried across whole rather than half-removed: it answers
a product question (has the user changed the subject, so the UI can offer to split
the conversation), it needs a model provider and an observability endpoint that
nothing else in this package needs, and its system prompt is written for one
domain — Kubernetes scheduling — so it is not reusable even in principle.

It is kept out of the file API's own module and mounted only when a deployment asks
for it, so the default surface carries no model configuration. A deployment that
wants it calls `include_router(router)`; the rest never load this module.

Pending removal to whichever service owns the conversation.
"""

from __future__ import annotations

import json
import os
import re
import threading
import time
import urllib.error
import urllib.request
import uuid
from datetime import datetime, timezone
from typing import Optional

from fastapi import APIRouter, HTTPException
from pydantic import BaseModel

router = APIRouter()

# --- Topic-switch classifier (optional) ------------------------------------
# A tiny binary check: does a new chat message START A NEW TOPIC vs continue the
# current conversation? Reuses opencode's OWN model config (opencode.json in this
# pod) so NOTHING extra is configured — no provider, no key, no model, no Helm
# value: it just uses the deployment's existing provider + default (first) model,
# and the API key never leaves the pod. DEFAULTS ON so it works out of the box;
# the frontend user setting (on by default, per-user opt-out in the composer's
# tools menu) decides whether a given user's sends reach /classify at all.
# Operators can hard-disable the whole feature with HANDS_CLASSIFIER_ENABLED=0.
CLASSIFIER_ENABLED = os.environ.get(
    "HANDS_CLASSIFIER_ENABLED", "1"
).lower() not in ("0", "false", "no", "off")
# Fixed model-id override (opencode model id, e.g.
# "deepseek-ai/DeepSeek-V4-Flash"). When set it WINS over the model the frontend
# passes (the user's active chat pick): the classifier is a cheap fixed-cost
# check and must not inherit whatever expensive/reasoning model the user happens
# to be chatting with. Empty → the frontend's model, else the deployment default.
# Prefer pinning a NON-REASONING model: a reasoning model spends the whole
# completion budget on its chain of thought and returns empty content (see
# CLASSIFIER_MAX_TOKENS).
CLASSIFIER_MODEL = os.environ.get("HANDS_CLASSIFIER_MODEL", "").strip()
# The opencode config the assistant reads (provider baseURL + apiKey + models).
OPENCODE_CONFIG_PATH = os.environ.get(
    "OPENCODE_CONFIG_PATH", "/home/opencode/.config/opencode/opencode.json"
)
def _classifier_timeout_s() -> float:
    try:
        return float(os.environ.get("HANDS_CLASSIFIER_TIMEOUT_S", "") or 20)
    except (TypeError, ValueError):
        return 20.0


def _classifier_max_tokens() -> int:
    try:
        return max(16, int(os.environ.get("HANDS_CLASSIFIER_MAX_TOKENS", "") or 512))
    except (TypeError, ValueError):
        return 512


# The verdict itself is ~16 tokens, but a REASONING model emits its chain of
# thought first and only then the answer — too small a cap truncates it mid-
# thought (finish_reason=length, empty content) and every message silently reads
# as a continuation. The budget is a cap, not a spend: a non-reasoning model
# still stops at ~16 tokens. Keep it roomy so the check works on both.
CLASSIFIER_MAX_TOKENS = _classifier_max_tokens()
# Bounded well under the dashboard nginx `proxy_read_timeout` on /assistant-fs/.
# Measured p50 ~1.3s on a pinned non-reasoning model, with occasional multi-second
# gateway spikes — too tight a budget turns a spike into a silent "continuation".
CLASSIFIER_TIMEOUT_S = _classifier_timeout_s()


def _classifier_max_context() -> int:
    try:
        return max(
            1000, int(os.environ.get("HANDS_CLASSIFIER_MAX_CONTEXT_CHARS", "") or 200_000)
        )
    except (TypeError, ValueError):
        return 200_000


# How much conversation we are willing to ship per check. The models in play have
# very large context windows, and the verdict is only as good as the history it
# can see: truncating mid-conversation is what makes the subject drop out and a
# follow-up read as a fresh question. Generous by default, still bounded so one
# pathological session cannot post a megabyte per keystroke.
MAX_CLASSIFY_CONTEXT = _classifier_max_context()

# Bumped whenever the prompt below changes, so LangFuse can compare verdict
# quality across revisions instead of silently mixing them.
CLASSIFIER_PROMPT_VERSION = "v5-dual-axis"

# The two axes are perceptual judgements a small model does well. The AND rule
# over them is arithmetic, and models DO get it wrong (reporting "the object
# carries over" and "new topic" in the same breath), so the verdict is derived
# in code from the two booleans rather than trusted from the reply.
CLASSIFIER_SYSTEM_PROMPT = """You detect topic boundaries in a Kubernetes scheduling assistant, so the UI can offer to move a genuinely new question into its own session.

You are given the conversation exactly as the user sees it: their messages, the assistant's replies, and the tool calls it issued (names and arguments). Judge the new message on two axes.

OBJECT -- the concrete thing under investigation: a pod / workload / node / cluster / team / quota / reservation. It CARRIES OVER when the new message:
- refers to it by name, by pronoun, or by ellipsis. Users write in subject-dropping languages, so a bare "why?", "and the fix?", "from another angle", or "what about the other one" is a follow-up on the current object, not a new one
- asks the next step on it: root cause, fix, YAML, logs, an export, a re-run
- generalises the same question without naming a different object ("other clusters", "any others like it")
- asks about something that first appeared IN THE ANSWER or in a tool call: the node it landed on, its podgroup, its cluster, its quota

GOAL -- the line of enquiry: why is this unschedulable, what is wrong with these nodes, how much quota is left. Read the ANSWER, not only the questions: any cause, constraint, or next step the assistant already surfaced belongs to the current goal. If the diagnosis said the workload was blocked by a quota, then asking about that quota CONTINUES the goal. If it named a taint, asking about that taint continues the goal. Rephrasing, trying another hypothesis for the same symptom, or pointing the SAME enquiry at a different object all keep the goal.

A new topic requires BOTH axes to change: a different object AND a different line of enquiry. If either carries over, it is a continuation.

Sharing a domain is not sharing an axis: every message here is about Kubernetes scheduling, so that tells you nothing.

Reply with ONLY this JSON, no prose, no code fences:
{"objectCarriesOver":boolean,"goalCarriesOver":boolean,"isNewTopic":boolean,"confidence":0..1,"rationale":"at most 14 words"}"""


class ClassifyRequest(BaseModel):
    # The conversation as the user sees it: their messages, the assistant's
    # replies, and the tool calls it issued (names + arguments, no results, no
    # chain of thought). Capped at MAX_CLASSIFY_CONTEXT. Empty on a first message.
    context: str = ""
    # The message the user just sent.
    newInput: str
    # opencode model id to use (the user's active pick). Only consulted when no
    # HANDS_CLASSIFIER_MODEL pin is configured.
    model: Optional[str] = None
    # Observability only: the opencode session and per-user directory this check
    # belongs to, so the LangFuse trace lands beside the conversation's own.
    sessionID: Optional[str] = None
    userKey: Optional[str] = None


def _load_llm_provider() -> Optional[dict]:
    """Reuse opencode's model config: read opencode.json and return the first
    OpenAI-compatible provider's {baseURL, apiKey, models, defaultModel}. Robust
    to the provider name (chart uses "scitix", an existingSecret may differ) and
    to both credential sources (inline apiKey / external Secret). None when the
    file or the baseURL/apiKey are missing."""
    try:
        with open(OPENCODE_CONFIG_PATH, "r", encoding="utf-8") as f:
            cfg = json.load(f)
    except (OSError, ValueError):
        return None
    providers = cfg.get("provider") or {}
    for _pid, p in providers.items():
        opts = (p or {}).get("options") or {}
        base = opts.get("baseURL")
        key = opts.get("apiKey")
        if base and key:
            models = list((p.get("models") or {}).keys())
            # Top-level "model" is "<providerId>/<modelId>"; the modelId itself may
            # contain "/", so strip only the first segment.
            top = cfg.get("model") or ""
            top_model = top.split("/", 1)[1] if "/" in top else ""
            return {
                "baseURL": base.rstrip("/"),
                "apiKey": key,
                "models": models,
                "defaultModel": top_model or (models[0] if models else ""),
            }
    return None


_FAIL_SAFE_VERDICT = {
    "objectCarriesOver": True,
    "goalCarriesOver": True,
    "isNewTopic": False,
    "confidence": 0.0,
    "rationale": "",
}


def _parse_verdict(content: str) -> dict:
    """Pull the two axes out of the model's reply defensively (tolerate code fences
    and surrounding prose by slicing the outermost JSON object).

    The verdict is DERIVED here rather than read from the reply: a new topic needs
    both axes to change, and models do contradict themselves on that conjunction
    (reporting the object carries over and still calling it a new topic). Only when
    neither axis is present do we fall back to the model's own `isNewTopic`."""
    text = (content or "").strip()
    start, end = text.find("{"), text.rfind("}")
    if start != -1 and end > start:
        text = text[start : end + 1]
    try:
        obj = json.loads(text)
    except ValueError:
        return dict(_FAIL_SAFE_VERDICT)
    try:
        conf = max(0.0, min(1.0, float(obj.get("confidence", 0.0))))
    except (TypeError, ValueError):
        conf = 0.0
    has_axes = "objectCarriesOver" in obj or "goalCarriesOver" in obj
    obj_over = bool(obj.get("objectCarriesOver", True))
    goal_over = bool(obj.get("goalCarriesOver", True))
    is_new = (
        (not obj_over and not goal_over) if has_axes else bool(obj.get("isNewTopic"))
    )
    rationale = obj.get("rationale")
    return {
        "objectCarriesOver": obj_over,
        "goalCarriesOver": goal_over,
        "isNewTopic": is_new,
        "confidence": conf,
        "rationale": str(rationale)[:200] if isinstance(rationale, str) else "",
    }


# --- LangFuse reporting (optional, fire-and-forget) -------------------------
# The classifier calls the provider directly rather than going through opencode,
# so it is invisible to the opencode observability plugin. Report it ourselves,
# reusing the LANGFUSE_* env the plugin already needs — no extra configuration.
# Identity is deliberately SHARED with opencode (same userId, same sessionId) so
# a run shows up on the conversation's own timeline and per-user quality can be
# attributed; a distinct trace name + tag keeps it separately filterable.
LANGFUSE_BASEURL = os.environ.get("LANGFUSE_BASEURL", "").strip().rstrip("/")
LANGFUSE_PUBLIC_KEY = os.environ.get("LANGFUSE_PUBLIC_KEY", "").strip()
LANGFUSE_SECRET_KEY = os.environ.get("LANGFUSE_SECRET_KEY", "").strip()
LANGFUSE_ENVIRONMENT = os.environ.get("LANGFUSE_ENVIRONMENT", "").strip()
LANGFUSE_TIMEOUT_S = 5.0


def _langfuse_configured() -> bool:
    return bool(LANGFUSE_BASEURL and LANGFUSE_PUBLIC_KEY and LANGFUSE_SECRET_KEY)


def _now_iso() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def _langfuse_post(events: list) -> None:
    """POST one ingestion batch. Swallows everything: telemetry must never affect
    the verdict, and this already runs off the request thread."""
    payload = json.dumps({"batch": events}).encode("utf-8")
    token = base64.b64encode(
        f"{LANGFUSE_PUBLIC_KEY}:{LANGFUSE_SECRET_KEY}".encode("utf-8")
    ).decode("ascii")
    request = urllib.request.Request(
        f"{LANGFUSE_BASEURL}/api/public/ingestion",
        data=payload,
        method="POST",
        headers={"content-type": "application/json", "authorization": f"Basic {token}"},
    )
    try:
        with urllib.request.urlopen(request, timeout=LANGFUSE_TIMEOUT_S):
            pass
    except Exception:  # noqa: BLE001 - telemetry is strictly best-effort
        pass


def _report_to_langfuse(
    trace_id: str,
    req: ClassifyRequest,
    model: str,
    context: str,
    raw_output: str,
    verdict: dict,
    usage: dict,
    started: str,
    ended: str,
    latency_ms: int,
    error: Optional[str] = None,
) -> None:
    """Emit one trace + one generation for a single classify call."""
    if not _langfuse_configured():
        return
    metadata = {
        "component": "topic-classifier",
        "producer": "agentbox-workspace-fs",
        "promptVersion": CLASSIFIER_PROMPT_VERSION,
        "objectCarriesOver": verdict.get("objectCarriesOver"),
        "goalCarriesOver": verdict.get("goalCarriesOver"),
        "confidence": verdict.get("confidence"),
        "rationale": verdict.get("rationale"),
        "pinnedModel": bool(CLASSIFIER_MODEL),
        "contextChars": len(context),
        "latencyMs": latency_ms,
    }
    if error:
        metadata["error"] = error
    trace_body = {
        "id": trace_id,
        "name": "topic-classifier",
        "input": {"context": context, "newInput": req.newInput},
        "output": verdict,
        "metadata": metadata,
        "tags": ["classifier", "topic-switch"],
        "timestamp": started,
    }
    if req.sessionID:
        trace_body["sessionId"] = req.sessionID
    if req.userKey:
        trace_body["userId"] = req.userKey
    if LANGFUSE_ENVIRONMENT:
        trace_body["environment"] = LANGFUSE_ENVIRONMENT
    generation_body = {
        "id": str(uuid.uuid4()),
        "traceId": trace_id,
        "name": "topic-classifier-call",
        "model": model,
        "input": [
            {"role": "system", "content": CLASSIFIER_SYSTEM_PROMPT},
            {"role": "user", "content": _classifier_user_message(context, req.newInput)},
        ],
        "output": raw_output,
        "startTime": started,
        "endTime": ended,
        "metadata": metadata,
        "modelParameters": {"temperature": 0, "max_tokens": CLASSIFIER_MAX_TOKENS},
    }
    if usage:
        generation_body["usageDetails"] = usage
    if error:
        generation_body["level"] = "ERROR"
        generation_body["statusMessage"] = error
    events = [
        {
            "id": str(uuid.uuid4()),
            "type": "trace-create",
            "timestamp": started,
            "body": trace_body,
        },
        {
            "id": str(uuid.uuid4()),
            "type": "generation-create",
            "timestamp": started,
            "body": generation_body,
        },
    ]
    threading.Thread(target=_langfuse_post, args=(events,), daemon=True).start()


# The dashboard appends a `<page key=… cluster=… />` marker to every message it
# sends, telling the agent which page the user is looking at. It must never reach
# the classifier: walking from the node list to a pod detail is not a change of
# subject, and the markers make both axes (object / goal) look like they moved
# when only the browser did. The frontend already strips them; this is the
# backstop for any other caller (and for a frontend that lags a deploy).
_PAGE_MARKER = re.compile(r"<page\s+[^>]*/>")


def _strip_page_markers(text: str) -> str:
    return re.sub(r"\n{3,}", "\n\n", _PAGE_MARKER.sub("", text or "")).strip()


def _classifier_user_message(context: str, new_input: str) -> str:
    return (
        f"Conversation so far:\n{_strip_page_markers(context)}"
        f"\n\nNew message:\n{_strip_page_markers(new_input)}"
    )


@router.post("/classify")
def classify(req: ClassifyRequest) -> dict:
    """Decide whether `newInput` starts a new topic vs continues the conversation.

    Fails SAFE: any missing config / provider error / parse failure returns a
    non-nagging verdict (isNewTopic false) so the classifier can never block a
    send or produce a spurious prompt. `enabled:false` tells the frontend the
    feature is off (don't even show the affordance)."""
    if not CLASSIFIER_ENABLED:
        return {"enabled": False}
    prov = _load_llm_provider()
    if not prov:
        return {"enabled": False}
    model = CLASSIFIER_MODEL or req.model or prov["defaultModel"]
    if not model:
        return {"enabled": False}
    context = (req.context or "")[:MAX_CLASSIFY_CONTEXT]
    trace_id = uuid.uuid4().hex
    payload = json.dumps(
        {
            "model": model,
            "messages": [
                {"role": "system", "content": CLASSIFIER_SYSTEM_PROMPT},
                {
                    "role": "user",
                    "content": _classifier_user_message(context, req.newInput),
                },
            ],
            "temperature": 0,
            "max_tokens": CLASSIFIER_MAX_TOKENS,
        }
    ).encode("utf-8")
    request = urllib.request.Request(
        f"{prov['baseURL']}/chat/completions",
        data=payload,
        method="POST",
        headers={
            "content-type": "application/json",
            "authorization": f"Bearer {prov['apiKey']}",
        },
    )
    started = _now_iso()
    began = time.monotonic()
    try:
        with urllib.request.urlopen(request, timeout=CLASSIFIER_TIMEOUT_S) as resp:
            body = json.loads(resp.read().decode("utf-8"))
    except (urllib.error.URLError, ValueError, OSError) as exc:
        verdict = dict(_FAIL_SAFE_VERDICT)
        _report_to_langfuse(
            trace_id, req, model, context, "", verdict, {}, started, _now_iso(),
            int((time.monotonic() - began) * 1000), error=f"{type(exc).__name__}: {exc}",
        )
        return {"enabled": True, "traceId": trace_id, **verdict}
    latency_ms = int((time.monotonic() - began) * 1000)
    try:
        message = body["choices"][0]["message"]
        # A reasoning model that ran out of budget answers inside its chain of
        # thought and leaves `content` empty; _parse_verdict slices the outermost
        # JSON object, so falling back to it recovers the verdict instead of
        # silently reading as a continuation.
        content = message.get("content") or message.get("reasoning_content") or ""
    except (KeyError, IndexError, TypeError):
        content = ""
    verdict = _parse_verdict(content)
    raw_usage = body.get("usage") or {}
    usage = {
        k: v
        for k, v in (
            ("input", raw_usage.get("prompt_tokens")),
            ("output", raw_usage.get("completion_tokens")),
            ("total", raw_usage.get("total_tokens")),
        )
        if isinstance(v, int)
    }
    _report_to_langfuse(
        trace_id, req, model, context, content, verdict, usage, started,
        _now_iso(), latency_ms,
    )
    return {"enabled": True, "traceId": trace_id, **verdict}
