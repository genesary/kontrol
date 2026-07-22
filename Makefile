# Define the Go binary and output directory
GO ?= go
OUTPUT_DIR ?= ./bin
PROJECT_NAME ?= security-hub
MAIN_FILE ?= .
DOCKERFILE ?= Containerfile
DOCKER_ENGINE ?= podman
GO_BUILD_FLAGS ?= -buildvcs=true

# Report frontend (Tailwind is only needed to edit internal/report/tailwind/input.css;
# the compiled internal/report/static/css/app.css is committed and go build never
# needs Tailwind).
TAILWIND_VERSION ?= v4.3.3
TAILWIND_OS ?= linux
TAILWIND_ARCH ?= x64
TAILWIND_BIN ?= $(OUTPUT_DIR)/tailwindcss
REPORT_DIR ?= internal/report

# Default target
.DEFAULT_GOAL := build

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download

# Build target
build: deps
	@echo "Building the binary..."
	$(GO) build $(GO_BUILD_FLAGS) -o $(OUTPUT_DIR)/$(PROJECT_NAME) $(MAIN_FILE)

# Lint target
lint: deps
	@command -v golangci-lint >/dev/null 2>&1 || { echo "Installing golangci-lint..."; go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4; }
	@echo "Running golangci-lint..."
	golangci-lint run ./...

dependency-check:
	@echo "Running dependency-check..."
	dependency-check --nvdApiKey $(NVD_API_KEY) --scan ./ --format ALL --out dependency-check/ --enableExperimental

# Test target
test: deps
	@command -v gotestsum >/dev/null 2>&1 || { echo "Installing gotestsum..."; go install gotest.tools/gotestsum@v1.13.0; }
	@mkdir -p codequality
	gotestsum --junitfile codequality/unit-tests.xml --format-icons octicons -- -coverprofile=codequality/coverage.out -covermode=atomic ./...
	@echo "Coverage report generated: codequality/coverage.html"


# Docker target
package:
	@echo "Building Docker image..."
	$(DOCKER_ENGINE) build -t $(PROJECT_NAME):dev -f $(DOCKERFILE) .

# Rebuild the report's compiled CSS from internal/report/tailwind/input.css.
# Downloads the standalone Tailwind CLI (no Node/npm required) into $(OUTPUT_DIR)
# on first run. Only needed when editing the report's styles.
frontend:
	@mkdir -p $(OUTPUT_DIR)
	@test -x $(TAILWIND_BIN) || { \
		echo "Downloading standalone Tailwind CLI $(TAILWIND_VERSION)..."; \
		curl -sLo $(TAILWIND_BIN) https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-$(TAILWIND_OS)-$(TAILWIND_ARCH); \
		chmod +x $(TAILWIND_BIN); \
	}
	@echo "Compiling report CSS..."
	$(TAILWIND_BIN) -i $(REPORT_DIR)/tailwind/input.css -o $(REPORT_DIR)/static/css/app.css --minify

# Phony targets
.PHONY: deps build lint dependency-check test package frontend
