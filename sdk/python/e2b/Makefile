.PHONY: fmt code-formatter build

fmt:
	uv run --extra dev ruff format agent_sandbox_e2b

code-formatter: fmt

build:
	uv run --with build python -m build --wheel
