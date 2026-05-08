OPENAPI_SPEC  := ../../../../pkg/openapi/native/openapi.yaml
GEN_OUT       := agentbox_sdk/_generated
GEN_CONFIG    := openapi-gen-config.yaml
GEN_TMP       := /tmp/agentbox_sdk_gen

.PHONY: gen lint test fmt help

help:
	@echo "Targets:"
	@echo "  gen   - Generate _generated/ from openapi.yaml"
	@echo "  lint  - Run ruff + mypy"
	@echo "  test  - Run unit tests"
	@echo "  fmt   - Run ruff format"

gen:
	@echo "==> Generating $(GEN_OUT) from $(OPENAPI_SPEC)"
	rm -rf $(GEN_TMP)
	uvx openapi-python-client generate \
		--path $(OPENAPI_SPEC) \
		--config $(GEN_CONFIG) \
		--output-path $(GEN_TMP) \
		--overwrite
	@# Move just the inner package (not the whole project wrapper)
	rm -rf $(GEN_OUT)
	mkdir -p agentbox_sdk
	mv $(GEN_TMP)/agentbox_sdk/_generated $(GEN_OUT)
	rm -rf $(GEN_TMP)
	@echo "==> Done. Generated files in $(GEN_OUT)"

lint:
	uv run ruff check agentbox_sdk tests
	uv run mypy agentbox_sdk --ignore-missing-imports

fmt:
	uv run ruff format agentbox_sdk tests

.PHONY: code-formatter
code-formatter: fmt

test:
	uv run pytest tests/unit/ -v
