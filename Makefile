## agents-statusline dev tasks.
## Go is the primary language; JS/TS (npm shim, build script, pi extension) is linted with Biome.

GO_LINT      := golangci-lint
BIOME        := ./node_modules/.bin/biome

.PHONY: lint lint-go lint-js fmt fmt-go fmt-js vet test build check clean install-tools

## Install dev tooling (golangci-lint via brew, biome via npm).
install-tools:
	@command -v $(GO_LINT) >/dev/null 2>&1 || brew install golangci-lint
	@[ -x $(BIOME) ] || npm install

## Lint everything (Go + JS/TS).
lint: lint-go lint-js
lint-go:
	$(GO_LINT) run ./...
lint-js:
	@[ -x $(BIOME) ] || $(MAKE) install-tools
	$(BIOME) check npm/ scripts/

## Format.
fmt: fmt-go fmt-js
fmt-go:
	gofmt -w .
	goimports -w -local github.com/callmemorgan/agents-statusline . 2>/dev/null || true
fmt-js:
	@[ -x $(BIOME) ] || $(MAKE) install-tools
	$(BIOME) format --write npm/ scripts/

## Go vet + tests.
vet:
	go vet ./...
test:
	go test ./...

## Build the binary.
build:
	go build -o agents-statusline ./cmd/agents-statusline

## Full pre-commit gate: lint + vet + test.
check: lint vet test
