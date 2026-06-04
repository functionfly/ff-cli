PROJECT_PATH ?= ./...
BIN_DIR := $(shell go env GOPATH)/bin

.PHONY: test vet lint fmt security govulncheck gosec gosec-ci sbom ci
.DEFAULT_GOAL := test

test:
	go test -race -count=1 $(PROJECT_PATH)

vet:
	go vet $(PROJECT_PATH)

lint:
	golangci-lint run --timeout=5m

fmt:
	gofmt -s -w .
	goimports -w -local github.com/functionfly/ff-cli .

security: govulncheck gosec

govulncheck:
	$(BIN_DIR)/govulncheck ./... || true

# gosec targets only list findings; gosec-ci is the strict gate for CI.
gosec:
	$(BIN_DIR)/gosec -exclude=G499,G117 -fmt=golint ./... || true
gosec-ci:
	$(BIN_DIR)/gosec -fmt=golint ./...

sbom:
	@command -v syft >/dev/null 2>&1 || { echo "syft not installed (https://github.com/anchore/syft)"; exit 1; }
	syft $(dir $(shell go env GOMOD)):golang -o sbom.spdx.json -q

ci: test vet lint gosec-ci govulncheck
