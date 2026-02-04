.PHONY: build test lint clean run

BINARY_NAME=kube-sentry-events
GO=go

build:
	$(GO) build -o $(BINARY_NAME) ./cmd/kube-sentry-events

test:
	$(GO) test -v ./...

test-coverage:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run

clean:
	rm -f $(BINARY_NAME) coverage.out coverage.html

run: build
	./$(BINARY_NAME)

# Download dependencies
deps:
	$(GO) mod download
	$(GO) mod tidy

# Build for multiple architectures
build-all:
	GOOS=linux GOARCH=amd64 $(GO) build -o $(BINARY_NAME)-linux-amd64 ./cmd/kube-sentry-events
	GOOS=linux GOARCH=arm64 $(GO) build -o $(BINARY_NAME)-linux-arm64 ./cmd/kube-sentry-events
	GOOS=darwin GOARCH=amd64 $(GO) build -o $(BINARY_NAME)-darwin-amd64 ./cmd/kube-sentry-events
	GOOS=darwin GOARCH=arm64 $(GO) build -o $(BINARY_NAME)-darwin-arm64 ./cmd/kube-sentry-events
