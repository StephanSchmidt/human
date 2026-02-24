-include .env
export

.PHONY: all build test coverage lint sec check clean upgrade-deps

build:
	go build -o bin/human ./cmd/human

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

check: lint sec

clean:
	go clean -cache -i

all: lint sec build

upgrade-deps:
	go get -u ./...
	go mod tidy
	go tool gotestsum ./...

jira-list: build
	./bin/human --jira-key=$$($(OP) item get "Jira API Key" --fields notesPlain) issues list --project KAN

jira-get: build
	./bin/human --jira-key=$$($(OP) item get "Jira API Key" --fields notesPlain) issue get $(ISSUE)
