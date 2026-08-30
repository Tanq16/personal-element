.PHONY: help clean build build-for build-all docker-build docker-push version

# =============================================================================
# Variables
# =============================================================================
APP_NAME    := element-agent
DOCKER_USER ?= example
MODULE      := element-agent

VERSION ?= dev-build
GOOS    ?= $(shell go env GOOS)
GOARCH  ?= $(shell go env GOARCH)

CYAN  := \033[0;36m
GREEN := \033[0;32m
NC    := \033[0m

# =============================================================================
# Help
# =============================================================================
help: ## Show this help
	@echo "$(CYAN)Available targets:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-20s$(NC) %s\n", $$1, $$2}'

.DEFAULT_GOAL := help

# =============================================================================
# Build
# =============================================================================
clean: ## Remove built binaries
	@rm -f $(APP_NAME) $(APP_NAME)-*
	@echo "$(GREEN)Cleaned$(NC)"

build: ## Build for the current platform
	@go build -ldflags="-s -w -X '$(MODULE)/cmd.AppVersion=$(VERSION)'" -o $(APP_NAME) .
	@echo "$(GREEN)Built: ./$(APP_NAME)$(NC)"

build-for: ## Build for a specific GOOS/GOARCH
	@CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
	  -ldflags="-s -w -X '$(MODULE)/cmd.AppVersion=$(VERSION)'" \
	  -o $(APP_NAME)-$(GOOS)-$(GOARCH) .
	@echo "$(GREEN)Built: ./$(APP_NAME)-$(GOOS)-$(GOARCH)$(NC)"

build-all: ## Build every platform binary
	@$(MAKE) build-for GOOS=linux  GOARCH=amd64
	@$(MAKE) build-for GOOS=linux  GOARCH=arm64
	@$(MAKE) build-for GOOS=darwin GOARCH=amd64
	@$(MAKE) build-for GOOS=darwin GOARCH=arm64

# =============================================================================
# Docker
# =============================================================================
docker-build: ## Build the container image for this machine
	@docker build --build-arg VERSION=$(VERSION) -t $(DOCKER_USER)/$(APP_NAME):$(VERSION) .
	@docker tag $(DOCKER_USER)/$(APP_NAME):$(VERSION) $(DOCKER_USER)/$(APP_NAME):latest

# buildx cannot load a multi-platform result into the local daemon, so the
# manifest is built and pushed in one step rather than built then pushed.
docker-push: ## Build linux/amd64 and linux/arm64 and push one manifest
	@docker buildx build --platform linux/amd64,linux/arm64 \
	  --build-arg VERSION=$(VERSION) \
	  -t $(DOCKER_USER)/$(APP_NAME):$(VERSION) \
	  -t $(DOCKER_USER)/$(APP_NAME):latest \
	  --push .

# =============================================================================
# Version
# =============================================================================
version: ## Print the next version, derived from the last commit message
	@LATEST_TAG=$$(git tag --sort=-v:refname | head -n1 || echo "0.0.0"); \
	LATEST_TAG=$${LATEST_TAG#v}; \
	MAJOR=$$(echo "$$LATEST_TAG" | cut -d. -f1); \
	MINOR=$$(echo "$$LATEST_TAG" | cut -d. -f2); \
	PATCH=$$(echo "$$LATEST_TAG" | cut -d. -f3); \
	MAJOR=$${MAJOR:-0}; MINOR=$${MINOR:-0}; PATCH=$${PATCH:-0}; \
	COMMIT_MSG="$$(git log -1 --pretty=%B)"; \
	if echo "$$COMMIT_MSG" | grep -q "\[major-release\]"; then \
		MAJOR=$$((MAJOR + 1)); MINOR=0; PATCH=0; \
	elif echo "$$COMMIT_MSG" | grep -q "\[minor-release\]"; then \
		MINOR=$$((MINOR + 1)); PATCH=0; \
	else \
		PATCH=$$((PATCH + 1)); \
	fi; \
	echo "v$${MAJOR}.$${MINOR}.$${PATCH}"
