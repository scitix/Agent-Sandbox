.PHONY: fmt code-formatter build

fmt:
	uv run --extra dev ruff format agent_sandbox_harbor

code-formatter: fmt

build:
	uv run --with build python -m build --wheel
