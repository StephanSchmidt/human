.PHONY: all build install test coverage lint sec secrets check clean upgrade-deps

build:
	go build -o bin/human ./cmd/human

install:
	go install ./cmd/human

test:
	go test ./...

coverage:
	go tool gotestsum -- -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

lint:
	go vet ./...
	go tool staticcheck ./...
	go tool golangci-lint run ./...
	go tool nilaway ./...

sec:
	go tool gosec -exclude=G704 ./...
	go tool govulncheck ./...

secrets:
	go tool gitleaks git -v

check: lint sec secrets

clean:
	go clean -cache -i

all: lint sec build

upgrade-deps:
	go get -u ./...
	go mod tidy
	go tool gotestsum ./...
