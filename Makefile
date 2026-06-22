# Get the OS name in lowercase (linux, darwin)
OS_SYSNAME := $(shell uname -s | tr A-Z a-z)
# Get the machine architecture (x86_64, arm64)
OS_MACHINE := $(shell uname -m)

# If mac OS, use `macos-arm64` or `macos-x64`
ifeq ($(OS_SYSNAME),darwin)
	OS_SYSNAME = macos
	ifneq ($(OS_MACHINE),arm64)
		OS_MACHINE = x64
	endif
endif

# If Linux, use `linux-x64`
ifeq ($(OS_SYSNAME),linux)
	OS_MACHINE = x64
endif

# The appropriate Tailwind package for your OS will attempt to be automatically determined.
# If this is not working, hard-code the package you want using these options:
# https://github.com/tailwindlabs/tailwindcss/releases/latest
TAILWIND_PACKAGE = tailwindcss-$(OS_SYSNAME)-$(OS_MACHINE)

# ----------------------------------------------------------------------------
# Build-time metadata, injected into internal/version via -ldflags.
# Users see these values on the homepage footer (top-level JSON fields, never
# under "settings", so users cannot override them via homepage.yaml).
# Override any of them at the command line, e.g. `make build VERSION=v2.0.0`.
# ----------------------------------------------------------------------------
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.0.9")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GO_VERSION := $(shell go version | awk '{print $$3}')

# Fully-qualified import path of the internal version package.
# Used by -X to set the values of its mutable vars at link time.
VERSION_PKG := github.com/bassista/go_spin/internal/version

# LDFLAGS injected into the binary at compile time:
#   -s -w                          strip debug info (smaller binary)
#   -X .../Version=$(VERSION)      set the Version var
#   -X .../BuildTime=$(BUILD_TIME) set the BuildTime var
#   -X .../GitCommit=$(GIT_COMMIT) set the GitCommit var
#   -X .../GoVersion=$(GO_VERSION) set the GoVersion var
#
# IMPORTANT: `LDFLAGS` here is ONE of three duplicated sources for the same
# build metadata. Keep all three in lockstep whenever you add/remove a flag:
#   1. This LDFLAGS variable                (consumed by `make build` + `make run`)
#   2. .air.toml [build].cmd                 (consumed by `make watch`)
#   3. Dockerfile `-X -ldflags` block + ARG (consumed by `make docker_build` / CI)
#
LDFLAGS = -ldflags "\
	-s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).BuildTime=$(BUILD_TIME) \
	-X $(VERSION_PKG).GitCommit=$(GIT_COMMIT) \
	-X $(VERSION_PKG).GoVersion=$(GO_VERSION)"

.PHONY: help
help: ## Print make targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: install
install: air-install tailwind-install ## Install all dependencies

.PHONY: tailwind-install
tailwind-install: ## Install the Tailwind CSS CLI
	curl -sLo tailwindcss https://github.com/tailwindlabs/tailwindcss/releases/latest/download/$(TAILWIND_PACKAGE)
	chmod +x tailwindcss
	curl -sLO https://github.com/saadeghi/daisyui/releases/latest/download/daisyui.js
	curl -sLO https://github.com/saadeghi/daisyui/releases/latest/download/daisyui-theme.js

.PHONY: air-install
air-install: ## Install air
	go install github.com/air-verse/air@latest

.PHONY: run
run: ## Run the application (uses the same LDFLAGS as `make build` so the runtime version footer reports real metadata, no stale `.build/mai n` is left behind)
	clear
	go run $(LDFLAGS) ./cmd/server

.PHONY: watch
watch: ## Run the application and watch for changes with air (ldflags injected via `.air.toml`'s [build].cmd — keep in sync with the LDFLAGS variable above so local dev and CI builds report the same metadata)
	clear
	air

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: fmt
fmt: ## Run go fmt
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: staticcheck
staticcheck: ## Run staticcheck
	staticcheck ./...

.PHONY: golangci-lint
golangci-lint: ## Run golangci-lint (comprehensive linter suite)
	golangci-lint run --concurrency=1  ./...

.PHONY: lint
lint: fmt vet staticcheck golangci-lint ## Run all linters (go vet + staticcheck + golangci-lint)

.PHONY: check-updates
check-updates: ## Check for direct dependency updates
	go list -u -m -f '{{if not .Indirect}}{{.}}{{end}}' all | grep "\["

.PHONY: css
css: ## Build and minify Tailwind CSS
	./tailwindcss -i tailwind.css -o public/static/main.css -m

.PHONY: build
build: lint test ## Build and compile the application binary (runs lint + test first)
	go build $(LDFLAGS) -o ./.build/main ./cmd/server

.PHONY: docker_build
docker_build: ## Build docker image for current platform (local dev)
	docker buildx build -f Dockerfile \
		--load \
		-t bassista/gospin:latest \
		--progress plain .

.PHONY: docker_buildx
docker_buildx: ## Build multi-arch image and push (linux/amd64 + linux/arm64)
	docker buildx build -f Dockerfile \
		--platform linux/amd64,linux/arm64 \
		--cache-from type=registry,ref=bassista/gospin:cache \
		--cache-to type=registry,ref=bassista/gospin:cache,mode=max \
		-t bassista/gospin:latest \
		--progress plain \
		--push .

.PHONY: docker_push
docker_push: ## Push docker image
	docker push bassista/gospin:latest
