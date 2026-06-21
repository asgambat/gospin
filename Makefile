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
run: ## Run the application
	clear
	go run cmd/server/main.go

.PHONY: watch
watch: ## Run the application and watch for changes with air to automatically rebuild
	clear
	air

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: staticcheck
staticcheck: ## Run staticcheck
	staticcheck ./...

.PHONY: golangci-lint
golangci-lint: ## Run golangci-lint (comprehensive linter suite)
	golangci-lint run ./...

.PHONY: lint
lint: vet staticcheck golangci-lint ## Run all linters (go vet + staticcheck + golangci-lint)

.PHONY: check-updates
check-updates: ## Check for direct dependency updates
	go list -u -m -f '{{if not .Indirect}}{{.}}{{end}}' all | grep "\["

.PHONY: css
css: ## Build and minify Tailwind CSS
	./tailwindcss -i tailwind.css -o public/static/main.css -m

.PHONY: build
build: vet test ## Build and compile the application binary (runs vet + test first)
	go build -o ./.build/main ./cmd/server

.PHONY: docker_build
docker_build: ## Build docker image
#	docker build -f Dockerfile --platform $(OS_SYSNAME)/$(OS_MACHINE) --build-arg BUILDPLATFORM=$(OS_SYSNAME)/$(OS_MACHINE) --build-arg opts="CGO_ENABLED=0 GOOS=$(OS_SYSNAME) GOARCH=$(OS_MACHINE)" -t bassista/gospin:latest . --progress plain --no-cache
	docker build -f Dockerfile --platform linux/arm64 --build-arg BUILDPLATFORM=linux/arm64 --build-arg opts="CGO_ENABLED=0 GOOS=linux GOARCH=arm64" -t bassista/gospin:latest . --progress plain --no-cache

.PHONY: docker_push
docker_push: ## Push docker image
	docker push bassista/gospin:latest
