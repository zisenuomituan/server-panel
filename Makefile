GO      ?= go
LDFLAGS := -s -w

.PHONY: build release release-amd64 release-arm64 test clean

# 本机架构，开发用
build:
	CGO_ENABLED=0 $(GO) build -o bin/center ./cmd/center
	CGO_ENABLED=0 $(GO) build -o bin/vm-collect ./cmd/vm-collect

# 线上发布：两种架构各出一份，center 和 vm-collect 放一起
release: release-amd64 release-arm64

release-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/linux-amd64/center ./cmd/center
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/linux-amd64/vm-collect ./cmd/vm-collect

release-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/linux-arm64/center ./cmd/center
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/linux-arm64/vm-collect ./cmd/vm-collect

test:
	CGO_ENABLED=0 $(GO) test ./...

clean:
	rm -rf bin dist
