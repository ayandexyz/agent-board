BIN := agentboard
PKG := ./cmd/agentboard

.DEFAULT_GOAL := help
.PHONY: help run build install test test-integration vet check clean

help: ## List the available targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  make %-18s %s\n", $$1, $$2}'

run: build ## Build, then start the board in this directory
	./$(BIN)

build: ## Build ./agentboard
	go build -o $(BIN) $(PKG)

install: ## Install agentboard onto your PATH (go install)
	go install $(PKG)

test: ## Run unit tests
	go test ./...

test-integration: ## Run integration tests against real git
	go test -tags integration ./internal/orchestrator/

vet: ## Run go vet
	go vet ./...

check: vet test ## Vet and test, as before a PR

clean: ## Remove the built binary
	rm -f $(BIN)
