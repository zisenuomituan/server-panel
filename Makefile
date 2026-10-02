GO      ?= go
VERSION := $(shell cat VERSION)
LDFLAGS := -s -w -X main.cliVersion=$(VERSION)

.PHONY: build release release-amd64 release-arm64 test clean guard

# 本机架构，开发用
build:
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/center ./cmd/center
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/vm-collect ./cmd/vm-collect

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

# 推送前自检：确认私有配置、数据库、密钥没被 git 跟踪
guard:
	@if git ls-files | grep -qE '^private/|(^|/)center\.json$$|(^|/)id_ed25519(\.pub)?$$|\.db$$'; then \
		echo "!! 检测到私有文件被跟踪，别推："; \
		git ls-files | grep -E '^private/|(^|/)center\.json$$|(^|/)id_ed25519(\.pub)?$$|\.db$$'; \
		exit 1; \
	fi
	@echo "OK: 没有私有文件被跟踪"

clean:
	rm -rf bin dist
