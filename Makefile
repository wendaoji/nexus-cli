BIN     := dist/nexus-cli
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test vet tidy verify clean sync-spec

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/nexus-cli

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

verify: build
	scripts/verify.sh

# 从 Nexus 拉取新 spec、重生成机械层、校验 overlay
sync-spec:
	scripts/sync-spec.sh

clean:
	rm -rf dist
