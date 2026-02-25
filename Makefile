.PHONY: all build install test coverage lint sec check clean upgrade-deps

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

check: lint sec

clean:
	go clean -cache -i

all: lint sec build

upgrade-deps:
	go get -u ./...
	go mod tidy
	go tool gotestsum ./...

jira-list: build
	./bin/human issues list --project HUM

jira-get: build
	./bin/human issue get $(ISSUE)

jira-create: build
	./bin/human issue create --project=$(PROJECT) "$(SUMMARY)"
