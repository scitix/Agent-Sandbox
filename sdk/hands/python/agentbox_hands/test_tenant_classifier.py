"""Unit tests for the topic-switch classifier.

Every test here was written against the workspace-fs module, because the classifier
used to live inside it. They moved together: none of them exercises the file API.

Runnable with the standard library alone (no pytest):

    cd oss/assistant && python -m unittest proxy_daemon.test_fs

classifier.py has only light deps (fastapi / pydantic / stdlib), so — unlike the
sandbox-proxy daemon — nothing needs stubbing before import.
"""
import io
import json
import os
import tempfile
import time
import unittest
import urllib.error
from unittest import mock

from . import _tenant_classifier as classifier


def _axes(obj: bool, goal: bool, conf: float = 0.9) -> str:
    return json.dumps(
        {"objectCarriesOver": obj, "goalCarriesOver": goal, "confidence": conf}
    )


class ParseVerdictTests(unittest.TestCase):
    def test_new_topic_needs_both_axes_to_change(self):
        self.assertTrue(classifier._parse_verdict(_axes(False, False))["isNewTopic"])

    def test_either_axis_carrying_over_is_a_continuation(self):
        for obj, goal in ((True, True), (True, False), (False, True)):
            with self.subTest(object=obj, goal=goal):
                self.assertFalse(classifier._parse_verdict(_axes(obj, goal))["isNewTopic"])

    def test_axes_override_a_self_contradicting_verdict(self):
        """Models do report "the object carries over" and "new topic" together.
        The conjunction is arithmetic, so the axes win over the model's own call."""
        out = classifier._parse_verdict(
            '{"objectCarriesOver": true, "goalCarriesOver": false,'
            ' "isNewTopic": true, "confidence": 0.8}'
        )
        self.assertFalse(out["isNewTopic"])

    def test_falls_back_to_is_new_topic_when_axes_absent(self):
        out = classifier._parse_verdict('{"isNewTopic": true, "confidence": 0.9}')
        self.assertTrue(out["isNewTopic"])
        self.assertEqual(out["confidence"], 0.9)

    def test_fenced_and_prose_are_sliced(self):
        out = classifier._parse_verdict("Sure:\n```json\n" + _axes(True, True, 0.2) + "\n```")
        self.assertFalse(out["isNewTopic"])
        self.assertEqual(out["confidence"], 0.2)

    def test_confidence_clamped_to_unit_range(self):
        self.assertEqual(classifier._parse_verdict(_axes(False, False, 5))["confidence"], 1.0)

    def test_rationale_is_kept_but_bounded(self):
        out = classifier._parse_verdict(
            '{"objectCarriesOver": true, "goalCarriesOver": true,'
            ' "confidence": 0.5, "rationale": "' + "x" * 500 + '"}'
        )
        self.assertEqual(len(out["rationale"]), 200)

    def test_garbage_fails_safe_to_continuation(self):
        self.assertEqual(classifier._parse_verdict("not json at all"), classifier._FAIL_SAFE_VERDICT)


def _write_cfg(obj: dict) -> str:
    f = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
    json.dump(obj, f)
    f.close()
    return f.name


_FULL_CFG = {
    "model": "scitix/deepseek-ai/DeepSeek-V4-Flash",
    "provider": {
        "scitix": {
            "options": {"baseURL": "https://api.example/v1/", "apiKey": "sk-secret"},
            "models": {"deepseek-ai/DeepSeek-V4-Flash": {"name": "Flash"}},
        }
    },
}


class LoadProviderTests(unittest.TestCase):
    def test_reads_first_provider_with_creds(self):
        path = _write_cfg(_FULL_CFG)
        with mock.patch.object(classifier, "OPENCODE_CONFIG_PATH", path):
            prov = classifier._load_llm_provider()
        assert prov is not None
        self.assertEqual(prov["baseURL"], "https://api.example/v1")  # trailing / stripped
        self.assertEqual(prov["apiKey"], "sk-secret")
        # top-level "scitix/<id>" → strip only the provider segment.
        self.assertEqual(prov["defaultModel"], "deepseek-ai/DeepSeek-V4-Flash")

    def test_missing_file_returns_none(self):
        with mock.patch.object(classifier, "OPENCODE_CONFIG_PATH", "/no/such/opencode.json"):
            self.assertIsNone(classifier._load_llm_provider())

    def test_provider_without_creds_returns_none(self):
        path = _write_cfg({"provider": {"p": {"options": {}}}})
        with mock.patch.object(classifier, "OPENCODE_CONFIG_PATH", path):
            self.assertIsNone(classifier._load_llm_provider())


class ClassifyTests(unittest.TestCase):
    def test_disabled_reports_disabled(self):
        with mock.patch.object(classifier, "CLASSIFIER_ENABLED", False):
            out = classifier.classify(classifier.ClassifyRequest(newInput="hello"))
        self.assertEqual(out, {"enabled": False})

    def test_no_provider_reports_disabled(self):
        with mock.patch.object(classifier, "CLASSIFIER_ENABLED", True), mock.patch.object(
            classifier, "OPENCODE_CONFIG_PATH", "/no/such/opencode.json"
        ):
            out = classifier.classify(classifier.ClassifyRequest(newInput="hello"))
        self.assertEqual(out, {"enabled": False})

    def test_enabled_parses_model_verdict(self):
        path = _write_cfg(_FULL_CFG)
        body = json.dumps(
            {
                "choices": [
                    {"message": {"content": '{"isNewTopic": true, "confidence": 0.82}'}}
                ]
            }
        ).encode("utf-8")
        with mock.patch.object(classifier, "CLASSIFIER_ENABLED", True), mock.patch.object(
            classifier, "OPENCODE_CONFIG_PATH", path
        ), mock.patch(
            "urllib.request.urlopen", return_value=io.BytesIO(body)
        ):
            out = classifier.classify(
                classifier.ClassifyRequest(context="prev turns", newInput="unrelated thing")
            )
        self.assertTrue(out["enabled"])
        self.assertTrue(out["isNewTopic"])
        self.assertEqual(out["confidence"], 0.82)
        self.assertTrue(out["traceId"])

    def test_reasoning_only_reply_still_yields_a_verdict(self):
        """A reasoning model that answers inside its chain of thought (empty
        `content`, e.g. truncated at the token cap) must not read as a silent
        continuation."""
        path = _write_cfg(_FULL_CFG)
        body = json.dumps(
            {
                "choices": [
                    {
                        "finish_reason": "length",
                        "message": {
                            "content": "",
                            "reasoning_content": 'Topics differ, so {"isNewTopic": true, "confidence": 0.9}',
                        },
                    }
                ]
            }
        ).encode("utf-8")
        with mock.patch.object(classifier, "CLASSIFIER_ENABLED", True), mock.patch.object(
            classifier, "OPENCODE_CONFIG_PATH", path
        ), mock.patch("urllib.request.urlopen", return_value=io.BytesIO(body)):
            out = classifier.classify(classifier.ClassifyRequest(newInput="unrelated thing"))
        self.assertTrue(out["isNewTopic"])
        self.assertEqual(out["confidence"], 0.9)

    def test_pinned_model_overrides_the_callers_pick(self):
        """The pin is a cost ceiling: the classifier must not inherit whatever
        model the user happens to be chatting with."""
        path = _write_cfg(_FULL_CFG)
        body = json.dumps(
            {"choices": [{"message": {"content": '{"isNewTopic": false}'}}]}
        ).encode("utf-8")
        with mock.patch.object(classifier, "CLASSIFIER_ENABLED", True), mock.patch.object(
            classifier, "OPENCODE_CONFIG_PATH", path
        ), mock.patch.object(classifier, "CLASSIFIER_MODEL", "pinned-cheap"), mock.patch(
            "urllib.request.urlopen", return_value=io.BytesIO(body)
        ) as urlopen:
            classifier.classify(
                classifier.ClassifyRequest(newInput="hello", model="expensive-reasoner")
            )
        sent = json.loads(urlopen.call_args.args[0].data.decode("utf-8"))
        self.assertEqual(sent["model"], "pinned-cheap")
        self.assertEqual(sent["max_tokens"], classifier.CLASSIFIER_MAX_TOKENS)

    def test_upstream_error_fails_safe(self):
        path = _write_cfg(_FULL_CFG)
        with mock.patch.object(classifier, "CLASSIFIER_ENABLED", True), mock.patch.object(
            classifier, "OPENCODE_CONFIG_PATH", path
        ), mock.patch(
            "urllib.request.urlopen", side_effect=urllib.error.URLError("boom")
        ):
            out = classifier.classify(classifier.ClassifyRequest(newInput="whatever"))
        self.assertTrue(out["enabled"])
        self.assertFalse(out["isNewTopic"])
        self.assertEqual(out["confidence"], 0.0)

    def test_langfuse_reporting_is_off_without_keys(self):
        """Telemetry is opt-in by environment and must never reach the network
        (nor the verdict) when the deployment has no Langfuse keys."""
        path = _write_cfg(_FULL_CFG)
        body = json.dumps(
            {"choices": [{"message": {"content": _axes(False, False)}}]}
        ).encode("utf-8")
        with mock.patch.object(classifier, "CLASSIFIER_ENABLED", True), mock.patch.object(
            classifier, "OPENCODE_CONFIG_PATH", path
        ), mock.patch.object(classifier, "LANGFUSE_BASEURL", ""), mock.patch.object(
            classifier, "_langfuse_post"
        ) as post, mock.patch(
            "urllib.request.urlopen", return_value=io.BytesIO(body)
        ):
            out = classifier.classify(classifier.ClassifyRequest(newInput="unrelated"))
        post.assert_not_called()
        self.assertTrue(out["isNewTopic"])

    def test_langfuse_batch_carries_session_and_user(self):
        """The classifier shares opencode's identity so its trace lands on the
        conversation's own timeline; name + tag keep it separately filterable."""
        path = _write_cfg(_FULL_CFG)
        body = json.dumps(
            {
                "choices": [{"message": {"content": _axes(False, False)}}],
                "usage": {"prompt_tokens": 120, "completion_tokens": 28, "total_tokens": 148},
            }
        ).encode("utf-8")
        sent = []
        with mock.patch.object(classifier, "CLASSIFIER_ENABLED", True), mock.patch.object(
            classifier, "OPENCODE_CONFIG_PATH", path
        ), mock.patch.object(classifier, "LANGFUSE_BASEURL", "https://lf.example"
        ), mock.patch.object(classifier, "LANGFUSE_PUBLIC_KEY", "pk"), mock.patch.object(
            classifier, "LANGFUSE_SECRET_KEY", "sk"
        ), mock.patch.object(
            classifier, "_langfuse_post", side_effect=lambda ev: sent.extend(ev)
        ), mock.patch("urllib.request.urlopen", return_value=io.BytesIO(body)):
            out = classifier.classify(
                classifier.ClassifyRequest(
                    newInput="unrelated", sessionID="ses_abc", userKey="alice"
                )
            )
        # The reporter runs on a daemon thread; give it a moment to land.
        for _ in range(50):
            if sent:
                break
            time.sleep(0.01)
        kinds = {e["type"]: e["body"] for e in sent}
        trace = kinds["trace-create"]
        self.assertEqual(trace["id"], out["traceId"])
        self.assertEqual(trace["sessionId"], "ses_abc")
        self.assertEqual(trace["userId"], "alice")
        self.assertEqual(trace["name"], "topic-classifier")
        self.assertIn("classifier", trace["tags"])
        gen = kinds["generation-create"]
        self.assertEqual(gen["traceId"], out["traceId"])
        self.assertEqual(gen["usageDetails"], {"input": 120, "output": 28, "total": 148})


if __name__ == "__main__":
    unittest.main()


class MountingTests(unittest.TestCase):
    """The file API must not carry model configuration unless asked to.

    The classifier reaches a model provider and an observability endpoint. Leaving
    it mounted by default would make every deployment of the file API — including
    ones with no model credentials at all — expose a route that tries to use them.
    """

    # Reachability is checked by asking the app, not by reading its route list.
    # `app.routes` is not a flat list of routes in every FastAPI version — an
    # `include_router` can appear as a wrapper carrying its children privately and
    # no `path` of its own — so introspection answers the version's route model
    # rather than the question here, which is whether a caller can reach /classify.
    def _client(self, enabled: bool):
        import importlib
        import sys

        from fastapi.testclient import TestClient

        previous = os.environ.get("HANDS_ENABLE_TENANT_CLASSIFIER")
        if enabled:
            os.environ["HANDS_ENABLE_TENANT_CLASSIFIER"] = "1"
        else:
            os.environ.pop("HANDS_ENABLE_TENANT_CLASSIFIER", None)
        self.addCleanup(self._restore, previous)
        sys.modules.pop("agentbox_hands.fs", None)
        module = importlib.import_module("agentbox_hands.fs")
        return TestClient(module.app)

    def _restore(self, previous):
        import sys

        if previous is None:
            os.environ.pop("HANDS_ENABLE_TENANT_CLASSIFIER", None)
        else:
            os.environ["HANDS_ENABLE_TENANT_CLASSIFIER"] = previous
        sys.modules.pop("agentbox_hands.fs", None)

    def test_classify_is_absent_by_default(self):
        self.assertEqual(self._client(enabled=False).post("/classify", json={}).status_code, 404)

    def test_the_file_api_is_unaffected_either_way(self):
        # An empty body is rejected as unprocessable by every one of these, which is
        # all this needs: it distinguishes "mounted" from "not there" without
        # reaching a sandbox or a model. /healthz takes no body and answers 200.
        for enabled in (False, True):
            client = self._client(enabled=enabled)
            self.assertEqual(client.get("/healthz").status_code, 200, f"enabled={enabled}")
            for route in ("/ensure", "/attach", "/attach-read", "/list", "/read-file"):
                self.assertNotEqual(
                    client.post(route, json={}).status_code,
                    404,
                    f"{route} with enabled={enabled}",
                )

    # A deployment already calling POST /classify migrates by setting one variable,
    # rather than by pinning an old version of this package.
    def test_classify_can_be_mounted_back(self):
        # 422, not 200: the body is invalid, so it never reaches a model provider —
        # which is what makes this safe to assert with no credentials configured.
        self.assertEqual(self._client(enabled=True).post("/classify", json={}).status_code, 422)
