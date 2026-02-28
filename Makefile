.PHONY: all build install test test-integration coverage lint sec secrets check clean upgrade-deps release

VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")

build:
	go tool goimports -w .
	go build -ldflags "-X main.version=dev -X main.commit=$$(git rev-parse --short HEAD) -X main.date=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o bin/human .

install:
	go install .

test:
	go tool gotestsum ./...

coverage:
	go tool gotestsum -- -coverprofile=coverage.out $$(go list ./... | grep -v /cmd/)
	go tool cover -func=coverage.out

lint:
	go vet ./...
	go tool staticcheck ./...
	go tool golangci-lint run ./...
	go tool nilaway ./...
	go tool gocyclo -over 15 .

sec:
	go tool gosec ./...
	go tool govulncheck ./...

secrets:
	go tool gitleaks git -v

test-integration: build
	go run ./cmd/integrationtest

check: lint sec secrets

clean:
	go clean -cache -i

all: lint sec build

upgrade-deps:
	go get -u ./...
	go mod tidy
	go tool gotestsum ./...

tokens:
	@find . -name '*.go' ! -path './vendor/*' -exec cat {} + | wc -w | awk '{printf "%d words (~%d tokens)\n", $$1, int($$1 * 1.3)}'

release:
	@test -z "$$(git status --porcelain)" || (echo "error: working tree is dirty" && exit 1)
	@echo "Tagging $(VERSION)..."
	git tag -a $(VERSION) -m "Release $(VERSION)"
	git push origin $(VERSION)
	go tool goreleaser release --clean
