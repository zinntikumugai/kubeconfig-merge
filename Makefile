# kubeconfig-merge
#
# Go は mise で 1.26.6 に固定している。PATH に go が無い環境（devcontainer の
# 素の shell 等）では自動的に `mise exec -- go` にフォールバックする。
# 明示的に指定したいときは `make build GO="mise exec -- go"`。

GO ?= go
ifeq ($(shell command -v $(firstword $(GO)) 2>/dev/null),)
GO := mise exec -- go
endif

BINARY  := kubeconfig-merge
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# アーカイブ名には先頭の "v" を含めない: v1.0.0 -> 1.0.0
DIST_VERSION := $(VERSION:v%=%)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: all build test vet e2e dist clean

all: build

## build: ./kubeconfig-merge を生成する（静的リンク・バージョン埋め込み）
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) .

## test: 単体テスト + シナリオテスト
test:
	$(GO) test -race ./...

## vet: 静的解析
vet:
	$(GO) vet ./...

## e2e: ビルドしたバイナリを実ファイルに対して起動する e2e テスト
e2e: build
	./scripts/e2e.sh

## dist: 4 プラットフォーム分の tar.gz と sha256sums.txt を dist/ に生成する
dist:
	@rm -rf dist
	@mkdir -p dist
	@set -eu; \
	for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		name=$(BINARY)_$(DIST_VERSION)_$${os}_$${arch}; \
		stage=dist/$$name; \
		mkdir -p $$stage; \
		echo "building $$name"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $$stage/$(BINARY) .; \
		files="$(BINARY)"; \
		if [ -f README.md ]; then cp README.md $$stage/; files="$$files README.md"; fi; \
		if [ -f LICENSE ]; then cp LICENSE $$stage/; files="$$files LICENSE"; fi; \
		tar -czf dist/$$name.tar.gz -C $$stage $$files; \
		rm -rf $$stage; \
	done
	@cd dist && sha256sum *.tar.gz > sha256sums.txt
	@ls -l dist

## clean: 生成物を削除する
clean:
	rm -f $(BINARY)
	rm -rf dist
