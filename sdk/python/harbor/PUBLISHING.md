# Publishing `agent-sandbox-harbor` to PyPI

This package is published from the [`scitix/agent-sandbox`](https://github.com/scitix/agent-sandbox)
repository via the
[`sdk-python-harbor-publish.yml`](../../.github/workflows/sdk-python-harbor-publish.yml)
GitHub Actions workflow.

Publishing uses **PyPI Trusted Publishers** (OIDC). No long-lived PyPI tokens are stored in
the repository.

---

## One-time setup on PyPI

Do this once, before the first release.

### 1. Reserve the project name

If `agent-sandbox-harbor` has never been published, the first successful CI run will create
the project on PyPI automatically using the **pending publisher** flow. Add a *pending*
publisher first:

1. Sign in to https://pypi.org as the user/org that owns `agent-sandbox-e2b`
   (so the new package lives in the same namespace).
2. Go to your account → **Your projects** → **Publishing** → **Pending publishers**
   (or directly: https://pypi.org/manage/account/publishing/).
3. Click **"Add a new pending publisher"** and fill in:

   | Field | Value |
   |---|---|
   | PyPI Project Name | `agent-sandbox-harbor` |
   | Owner | `scitix` |
   | Repository name | `agent-sandbox` |
   | Workflow name | `sdk-python-harbor-publish.yml` |
   | Environment name | `pypi` |

4. Save. The publisher is now "pending" until the first push of a matching tag.

> The `Environment name` value (`pypi`) must match the
> `environment: name: pypi` block in the publish job. Don't change one without the other.

### 2. (Alternative) Configure a Trusted Publisher on an existing project

If `agent-sandbox-harbor` already exists on PyPI (e.g. someone else created it for you),
add the same Trusted Publisher entry directly on the project page:

1. Go to `https://pypi.org/manage/project/agent-sandbox-harbor/settings/publishing/`.
2. **Add a new publisher** with the four fields above (Owner / Repo / Workflow /
   Environment).

This is the same flow used for `agent-sandbox-e2b`; see how that project is configured
for reference.

---

## Cutting a release

The workflow triggers on **tags shaped `harbor-v<MAJOR>.<MINOR>.<PATCH>`** — a different
prefix from the e2b SDK's `v*.*.*` tags, so the two workflows never collide.

1. Make sure the desired commit is on `main` (or the release branch). The workflow runs
   against `github.ref`, which is the tag, so it will pick up whatever the tag points at.

2. Decide the next version number. The CI step **"Stamp version into `__init__.py`"** writes
   the tag-derived version into `agent_sandbox_harbor/__init__.py` **inside the CI
   workspace** before building; you don't need to bump `__version__` in `main` manually,
   but **do** keep it in sync (matching the most recent published version) so reading the
   source tree is not confusing.

3. Tag & push:

   ```bash
   # Example: cut version 0.0.3
   git tag harbor-v0.0.3
   git push origin harbor-v0.0.3
   ```

4. Watch the run at
   https://github.com/scitix/agent-sandbox/actions/workflows/sdk-python-harbor-publish.yml.
   The `build` job:
   - Resolves version `0.0.3` from `harbor-v0.0.3`.
   - Stamps it into `__init__.py`.
   - Installs `harbor[e2b]` plus the local source.
   - Runs the **compatibility check** — confirms that `harbor.environments.e2b.E2BEnvironment`
     still has every method we subclass (`_does_template_exist`, `_create_template`,
     `_create_sandbox`, `start`, `exec`, `upload_file`, …). **If Harbor refactored and
     broke a method we depend on, the publish fails here**, before any wheel ships.
   - Records the verified `harbor` version and stamps a `<=<version>` upper bound into the
     `[project.optional-dependencies] harbor = [...]` block.
   - Builds the wheel + sdist.

5. The `publish` job runs only if `build` succeeded. It downloads the artifact and
   uploads via `pypa/gh-action-pypi-publish@release/v1` with OIDC. No token in scope.

6. Confirm at https://pypi.org/project/agent-sandbox-harbor/0.0.3/.

---

## Tagging cheat-sheet

| Action | Command |
|---|---|
| First release | `git tag harbor-v0.0.3 && git push origin harbor-v0.0.3` |
| Subsequent release | `git tag harbor-v0.0.4 && git push origin harbor-v0.0.4` |
| Delete a bad tag locally + remotely | `git tag -d harbor-v0.0.3 && git push --delete origin harbor-v0.0.3` |
| Move a tag | delete then re-create (force-tagging is discouraged) |

> **Yanking a release.** If a published version is broken, log in to PyPI and "yank" it
> through the project settings page; CI won't do this for you. Yanking hides the version
> from `pip install agent-sandbox-harbor` (default-latest) but leaves it pinnable.

---

## Local pre-flight (optional but recommended)

Before tagging, sanity-check the wheel builds and imports cleanly:

```bash
cd sdk/python/harbor

# Build
uv venv .build-venv
uv run --with build --python .build-venv/bin/python python -m build --wheel
ls dist/                               # should show agent_sandbox_harbor-<version>-py3-none-any.whl

# Install + import smoke test in a throwaway venv
uv venv .smoke
uv pip install --python .smoke/bin/python "harbor[e2b]" dist/agent_sandbox_harbor-*.whl
.smoke/bin/python -c "
import agent_sandbox_harbor
from agent_sandbox_harbor import AgentSandboxEnvironment
from harbor.environments.e2b import E2BEnvironment
assert issubclass(AgentSandboxEnvironment, E2BEnvironment)
print('OK', agent_sandbox_harbor.__version__)
"

# Clean up
rm -rf .build-venv .smoke build dist agent_sandbox_harbor.egg-info
```

If the smoke test passes locally, the CI compatibility check will almost certainly pass
too — the same install path is exercised.

---

## Workflow internals (one-page summary)

```
.github/workflows/sdk-python-harbor-publish.yml
├── on: push tags 'harbor-v*.*.*'
├── jobs.build
│   ├── Resolve version from tag (strip 'harbor-v' prefix)
│   ├── sed __version__ into agent_sandbox_harbor/__init__.py
│   ├── uv pip install ".[harbor]" + harbor[e2b]
│   ├── Inline Python compat check:
│   │     - import agent_sandbox_harbor (triggers patch_e2b)
│   │     - assert hasattr(E2BEnvironment, method) for every method we depend on
│   │     - assert issubclass(AgentSandboxEnvironment, E2BEnvironment)
│   │     - assert AgentSandboxEnvironment.type() == EnvironmentType.E2B
│   ├── Stamp `harbor>=0.7.0,<=<resolved>` upper bound into pyproject.toml
│   ├── python -m build (wheel + sdist)
│   └── upload-artifact 'python-harbor-dist'
└── jobs.publish (needs: build)
    ├── environment: pypi   # MUST match Trusted Publisher config on PyPI
    ├── permissions: id-token: write
    ├── download-artifact 'python-harbor-dist'
    └── pypa/gh-action-pypi-publish@release/v1   # OIDC, no token
```

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| Workflow run skipped on `harbor-v0.0.3` tag push | Tag pushed to a fork, or workflow file not on the tag commit | Push the tag to `scitix/agent-sandbox` directly; ensure the workflow file is committed before the tag. |
| `build` step fails at the compat check | Harbor renamed / removed one of the methods we subclass | Update `environment.py` to track the new method names; bump the local lower-bound on `harbor` in `pyproject.toml`. |
| `publish` step fails with `permission denied` | Trusted Publisher config missing or `environment:` name mismatched | Double-check the four fields under PyPI's *publishing* settings; the environment name must be exactly `pypi`. |
| Published version is correct but `pip install` resolves an older one | PyPI caching | Wait 1–2 min, or use `pip install --no-cache-dir`. |
