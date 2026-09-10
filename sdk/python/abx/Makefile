.PHONY: gen lint test fmt help

help:
	@echo "Targets:"
	@echo "  gen   - Regenerate from openapi.yaml (delegates to the root)"
	@echo "  lint  - Run ruff + mypy"
	@echo "  test  - Run unit tests"
	@echo "  fmt   - Run ruff format"

gen: ## Regenerate everything this package derives from the OpenAPI spec.
	@# Delegates rather than repeating the recipe. This target used to hold its
	@# own copy, which drifted: a spec path one directory too deep and an
	@# output path from a previous layout, so it deleted _generated and then
	@# failed to replace it. One recipe, in the Makefile that is actually run.
	$(MAKE) -C ../../.. gen-all-api

lint:
	uv run ruff check agentbox_sdk tests
	uv run mypy agentbox_sdk --ignore-missing-imports

fmt:
	uv run ruff format agentbox_sdk tests

.PHONY: code-formatter
code-formatter: fmt

test:
	uv run pytest tests/unit/ -v
