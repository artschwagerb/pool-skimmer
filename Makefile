VERSION ?= dev
IMAGE ?= pool-skimmer:local
GO ?= go
GO_VERSION ?= 1.27.1

.DEFAULT_GOAL := build

.PHONY: build install image test dist help

build:
	$(GO) build -trimpath -ldflags="-X main.version=$(VERSION)" -o pool-skimmer ./cmd/pool-skimmer

install:
	$(GO) install -trimpath -ldflags="-X main.version=$(VERSION)" ./cmd/pool-skimmer

image:
	docker build --build-arg GO_VERSION=$(GO_VERSION) --build-arg VERSION=$(VERSION) --tag $(IMAGE) .

test:
	docker build --build-arg GO_VERSION=$(GO_VERSION) --target test .

dist:
	VERSION=$(VERSION) GO_VERSION=$(GO_VERSION) sh scripts/build-release.sh

help:
	@echo "make build              Build ./pool-skimmer with the local Go toolchain"
	@echo "make install            Install pool-skimmer with the local Go toolchain"
	@echo "make test               Run all tests in Docker"
	@echo "make image              Build $(IMAGE)"
	@echo "make dist VERSION=x.y.z Build Linux and macOS release binaries"
