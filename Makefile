BINARY := brizz-code
BUILD_DIR := build
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.PHONY: build run clean test fmt install lint coverage deps vet

build:
	go build -v -ldflags "-s -w -X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY) ./cmd/brizz-code

run:
	go run ./cmd/brizz-code

clean:
	rm -rf $(BUILD_DIR)
	go clean

test:
	go test -race -v ./...

fmt:
	go fmt ./...

lint:
	golangci-lint run ./...

coverage:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out

deps:
	go mod download

vet:
	go vet ./...

install: build
	cp $(BUILD_DIR)/$(BINARY) ~/.local/bin/
