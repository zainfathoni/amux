BUILD_OUTPUT ?= amux

.PHONY: build test

build:
	./scripts/build-amux.sh "$(BUILD_OUTPUT)"

test:
	go test ./...
