BIN := agentboard
PKG := ./cmd/agentboard
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/ayandexyz/agent-board/internal/version.Version=$(VERSION)
TARGETS := linux/amd64/x86_64 linux/arm64/arm64 darwin/amd64/x86_64 darwin/arm64/arm64 android/arm64/arm64

.DEFAULT_GOAL := help
.PHONY: help run build install dist test test-integration vet check clean

help: ## List the available targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  make %-18s %s\n", $$1, $$2}'

run: build ## Build, then start the board in this directory
	./$(BIN)

build: ## Build ./agentboard
	go build -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

install: ## Install agentboard onto your PATH (go install)
	go install -ldflags "$(LDFLAGS)" $(PKG)

dist: ## Cross-compile every release binary and checksums.txt into dist/
	rm -rf dist && mkdir -p dist
	@for t in $(TARGETS); do \
		os=$${t%%/*}; rest=$${t#*/}; arch=$${rest%%/*}; name=$${rest#*/}; \
		echo "  building agentboard-$$os-$$name"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o dist/agentboard-$$os-$$name $(PKG) || exit 1; \
	done
	cd dist && sha256sum agentboard-* > checksums.txt

test: ## Run unit tests
	go test ./...

test-integration: ## Run integration tests against real git
	go test -tags integration ./internal/orchestrator/

vet: ## Run go vet
	go vet ./...

check: vet test ## Vet and test, as before a PR

clean: ## Remove the built binary and dist/
	rm -rf $(BIN) dist
