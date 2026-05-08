# Contributing to Agent Sandbox

Thank you for your interest in contributing to Agent Sandbox! We welcome bug reports, feature requests, documentation improvements, and code contributions.

---

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Ways to Contribute](#ways-to-contribute)
- [Developer Certificate of Origin (DCO)](#developer-certificate-of-origin-dco)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Commit Guidelines](#commit-guidelines)
- [Submitting a Pull Request](#submitting-a-pull-request)
- [Review Process](#review-process)

---

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](https://www.contributor-covenant.org/version/2/1/code_of_conduct/). By participating you agree to uphold these standards. Report unacceptable behavior to the project maintainers.

---

## Ways to Contribute

- **Bug reports** — open a GitHub Issue with reproduction steps, expected vs. actual behavior, and relevant logs
- **Feature requests** — open a GitHub Issue describing the problem you want to solve and your proposed solution
- **Documentation** — fix typos, improve clarity, or add missing documentation
- **Code** — fix bugs, implement features, or improve test coverage (always linked to an open Issue)

---

## Developer Certificate of Origin (DCO)

All contributions must be accompanied by a **`Signed-off-by`** line in every commit message. This certifies that you wrote the contribution or have the right to contribute it under the project license, in accordance with the [Developer Certificate of Origin version 1.1](https://developercertificate.org/).

```
Developer Certificate of Origin
Version 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I have the
    right to submit it under the open source license indicated in the file; or

(b) The contribution is based upon previous work that, to the best of my
    knowledge, is covered under an appropriate open source license and I have
    the right under that license to submit that work with modifications,
    whether created in whole or in part by me, under the same open source
    license (unless I am permitted to submit under a different license), as
    indicated in the file; or

(c) The contribution was provided directly to me by some other person who
    certified (a), (b) or (c) and I have not modified it.

(d) I understand and agree that this project and the contribution are public
    and that a record of the contribution (including all personal information
    I submit with it, including my sign-off) is maintained indefinitely and
    may be redistributed consistent with this project or the open source
    license(s) involved.
```

### How to sign off

Pass `-s` (or `--signoff`) to `git commit`:

```bash
git commit -s -m "feat(sandbox): add autoscaler cooldown period"
```

This appends the following line automatically:

```
Signed-off-by: Your Name <your@email.com>
```

> **Note on AI-generated code**: Only a human can certify the DCO. If you used an AI assistant to generate part of a commit, you are personally responsible for reviewing that code and ensuring it complies with the license terms before signing off. The `Signed-off-by` tag represents your certification — AI tools cannot sign off on your behalf.

### DCO enforcement

A [GitHub Actions workflow](.github/workflows/dco.yml) checks every pull request commit for a valid `Signed-off-by` line. Pull requests that fail this check will not be merged.

If you forgot to sign off on one or more commits, you can retroactively fix them:

```bash
# Fix the last N commits (replace 3 with the actual count)
git rebase --signoff HEAD~3
git push --force-with-lease
```

---

## Getting Started

### Prerequisites

| Tool | Minimum version |
|------|----------------|
| Go | 1.25 |
| `make` | any recent version |
| Docker | 20.10+ |
| kubectl | 1.26+ |
| Kind | 0.20+ (for E2E tests) |

### Fork and clone

```bash
# 1. Fork https://github.com/scitix/agent-sandbox on GitHub
# 2. Clone your fork
git clone https://github.com/<your-username>/agent-sandbox.git
cd agent-sandbox

# 3. Add the upstream remote
git remote add upstream https://github.com/scitix/agent-sandbox.git
```

### Build

```bash
make build          # Build all binaries
make build-installer  # Regenerate installer/k8s/install.yaml
```

### Run unit tests

```bash
make test           # No cluster required; uses envtest
```

### Run E2E tests

```bash
# Start a local Kind cluster first
kind create cluster

make test-e2e
```

---

## Development Workflow

1. **Sync with upstream** before starting any work:

   ```bash
   git fetch upstream
   git rebase upstream/main
   ```

2. **Create a feature branch** from `main` (never commit directly to `main`):

   ```bash
   git checkout -b feat/my-feature
   ```

3. **Make changes**, keeping each commit atomic and focused (see [Commit Guidelines](#commit-guidelines)).

4. **Run codegen** if you modified API types or the OpenAPI spec:

   ```bash
   make manifests generate   # after changing api/v1alpha1/
   make gen-all-api          # after changing pkg/openapi/native/openapi.yaml
   make build-installer      # always regenerate before opening a PR
   ```

5. **Run tests and lint** before pushing:

   ```bash
   make test
   make lint-fix
   ```

6. **Push and open a PR** (see [Submitting a Pull Request](#submitting-a-pull-request)).

---

## Commit Guidelines

### One logical change per commit

Each commit should represent a single, coherent change. Mix-and-match commits (e.g., "fix bug + add feature + update docs") make review and bisecting harder.

- Separate functional changes from refactoring
- Separate generated file updates from hand-written changes
- If a change spans multiple packages, group the commits in a logical order

### Commit message format

```
<scope>: <short imperative summary>

<optional body — explain WHY, not what>

Signed-off-by: Your Name <your@email.com>
```

**Rules:**

- **First line**: `<scope>: <summary>` — 72 characters max, imperative mood, no trailing period
- **Scope**: use the component or package name (e.g., `sandbox`, `extproc`, `wsproxy`, `api`, `controller`, `ci`, `docs`)
- **Body**: wrap at 72 characters; explain motivation, not mechanics
- **`Signed-off-by`** line is **mandatory** (see [DCO](#developer-certificate-of-origin-dco))

**Good examples:**

```
controller: respect SandboxPool deletion timestamp during reconcile

The reconciler was ignoring DeletionTimestamp and would re-create Pods
for a pool that was being deleted, causing a reconcile storm.

Signed-off-by: Alice Smith <alice@example.com>
```

```
api: add runtimeVersion field to Sandbox response

Signed-off-by: Bob Jones <bob@example.com>
```

**Bad examples:**

```
fix stuff                          ← no scope, vague
Updated the sandbox controller.    ← past tense, trailing period
WIP: half-done feature             ← do not commit WIP to a PR
```

---

## Submitting a Pull Request

1. Ensure all commits are signed off (`Signed-off-by` present on every commit).
2. Run `make test lint-fix` and verify it passes locally.
3. If you changed CRD types or the OpenAPI spec, run the appropriate codegen commands and include the generated files in your PR.
4. Open a PR against the `main` branch with:
   - A clear title following the commit message format
   - A description explaining **what** changed and **why**
   - A reference to the related Issue (`Closes #123`)
   - A brief test plan describing how you verified the change

### PR checklist

- [ ] All commits include `Signed-off-by`
- [ ] `make test` passes
- [ ] `make lint-fix` passes (no new lint errors)
- [ ] Generated files are up to date (`make manifests generate gen-all-api build-installer` as applicable)
- [ ] New functionality is covered by tests
- [ ] Documentation is updated if behavior changes

---

## Review Process

- At least one maintainer approval is required to merge.
- The DCO check and CI (build + tests + lint) must pass.
- Reviewers may request changes; please address feedback by adding new commits — do not force-push during review unless asked.
- Once approved, a maintainer will merge using **squash-and-merge** or **rebase-and-merge** depending on the PR.

---

## Need Help?

- Browse existing [Issues](https://github.com/scitix/agent-sandbox/issues) and [Discussions](https://github.com/scitix/agent-sandbox/discussions)
- Open a new Issue if you encounter a bug or have a question
- For architectural questions, open a Discussion before writing code

We appreciate every contribution, large or small. Thank you!
