BINARY := brizz-code
BUILD_DIR := build
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.PHONY: build run clean test fmt install

build:
	go build -ldflags "-s -w -X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY) ./cmd/brizz-code

run:
	go run ./cmd/brizz-code

clean:
	rm -rf $(BUILD_DIR)
	go clean

test:
	go test -race -v ./...

fmt:
	go fmt ./...

install: build
	cp $(BUILD_DIR)/$(BINARY) ~/.local/bin/
