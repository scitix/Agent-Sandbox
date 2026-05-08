.PHONY: fmt code-formatter build

fmt:
	uv run --extra dev ruff format agentbox

code-formatter: fmt

build:
	uv run --with build python -m build --wheel
