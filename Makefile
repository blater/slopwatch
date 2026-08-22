SHELL := /bin/sh

ROOT := $(abspath .)
BUILD_DIR := $(ROOT)/build
STRUCTURAL_DIR := $(ROOT)/analyzers/structural
TYPESCRIPT_DIR := $(ROOT)/analyzers/typescript
STRUCTURAL_BIN := $(STRUCTURAL_DIR)/slopslap-structural
RUST_BIN := $(STRUCTURAL_DIR)/slopslap-structural-rust
JAVA_JAR := $(STRUCTURAL_DIR)/slopslap-structural-java.jar
GO_BIN := $(BUILD_DIR)/slopslap
WATCH_BIN := $(BUILD_DIR)/slopwatch
TS_MARKER := $(TYPESCRIPT_DIR)/dist/src/cli.js
TS_NATIVE_BIN := $(TYPESCRIPT_DIR)/slopslap-typescript

GO_ENV := CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
GO_FLAGS := -trimpath -buildvcs=false
JAR_DATE := 1980-01-01T00:00:02Z

STRUCTURAL_GO_SOURCES := $(wildcard $(STRUCTURAL_DIR)/cmd/slopslap-structural/*.go) \
  $(wildcard $(STRUCTURAL_DIR)/internal/*/*.go)
STRUCTURAL_JAVA_SOURCES := $(wildcard $(STRUCTURAL_DIR)/adapters/java/src/dev/slopslap/structural/*.java)
STRUCTURAL_RUST_SOURCES := $(wildcard $(STRUCTURAL_DIR)/adapters/rust/src/*.rs)
GO_SOURCES := $(wildcard $(ROOT)/go/cmd/slopslap-go/*.go) \
  $(wildcard $(ROOT)/go/internal/*/*.go)
WATCH_SOURCES := $(wildcard $(ROOT)/go/cmd/slopwatch/*.go)
TS_SOURCES := $(wildcard $(TYPESCRIPT_DIR)/src/*.ts) $(wildcard $(TYPESCRIPT_DIR)/test/*.ts)

.PHONY: all build dev-build build-structural build-rust build-java build-go \
  build-typescript test test-structural test-typescript clean

all: build

build: build-structural build-rust build-java build-go build-typescript

dev-build: build-structural build-typescript build-go

$(STRUCTURAL_BIN): $(STRUCTURAL_GO_SOURCES) $(STRUCTURAL_DIR)/go.mod
	@mkdir -p $(dir $@) $(BUILD_DIR)/go-cache
	@$(GO_ENV) GOCACHE=$(BUILD_DIR)/go-cache go build -C $(STRUCTURAL_DIR) $(GO_FLAGS) -o $@ ./cmd/slopslap-structural

build-structural: $(STRUCTURAL_BIN)

$(RUST_BIN): $(STRUCTURAL_RUST_SOURCES) $(STRUCTURAL_DIR)/adapters/rust/Cargo.toml $(STRUCTURAL_DIR)/adapters/rust/Cargo.lock
	@mkdir -p $(BUILD_DIR)/cargo-target
	@CARGO_TARGET_DIR=$(BUILD_DIR)/cargo-target cargo build --locked --release --manifest-path $(STRUCTURAL_DIR)/adapters/rust/Cargo.toml
	@cp $(BUILD_DIR)/cargo-target/release/slopslap-structural-rust $@

build-rust: $(RUST_BIN)

$(JAVA_JAR): $(STRUCTURAL_JAVA_SOURCES)
	@rm -rf $(BUILD_DIR)/java-classes
	@mkdir -p $(BUILD_DIR)/java-classes $(dir $@)
	@javac --release 17 -g:none -d $(BUILD_DIR)/java-classes $(STRUCTURAL_JAVA_SOURCES)
	@jar --create --file $@ --date $(JAR_DATE) --main-class dev.slopslap.structural.Main -C $(BUILD_DIR)/java-classes .

build-java: $(JAVA_JAR)

$(GO_BIN): $(GO_SOURCES) $(ROOT)/go/go.mod $(ROOT)/go/go.sum
	@mkdir -p $(dir $@) $(BUILD_DIR)/go-cache
	@$(GO_ENV) GOCACHE=$(BUILD_DIR)/go-cache go build -C $(ROOT)/go $(GO_FLAGS) -o $@ ./cmd/slopslap-go

$(WATCH_BIN): $(WATCH_SOURCES) $(ROOT)/go/go.mod $(ROOT)/go/go.sum
	@mkdir -p $(dir $@) $(BUILD_DIR)/go-cache
	@$(GO_ENV) GOCACHE=$(BUILD_DIR)/go-cache go build -C $(ROOT)/go $(GO_FLAGS) -o $@ ./cmd/slopwatch

build-go: $(GO_BIN) $(WATCH_BIN)

$(TS_MARKER): $(TS_SOURCES) $(TYPESCRIPT_DIR)/package.json $(TYPESCRIPT_DIR)/package-lock.json $(TYPESCRIPT_DIR)/tsconfig.json
	@npm --prefix $(TYPESCRIPT_DIR) ci --ignore-scripts
	@npm --prefix $(TYPESCRIPT_DIR) run build

$(TS_NATIVE_BIN): $(TS_MARKER) $(TYPESCRIPT_DIR)/src/scriptc-entry.ts
	@mkdir -p $(TYPESCRIPT_DIR)/node_modules/@slopslap
	@ln -sfn $(TYPESCRIPT_DIR) $(TYPESCRIPT_DIR)/node_modules/@slopslap/typescript-analyzer
	@cd $(TYPESCRIPT_DIR) && npm exec --offline -- scriptc build src/scriptc-entry.ts --dynamic --out $(TS_NATIVE_BIN)

build-typescript: $(TS_NATIVE_BIN)

test-structural: build-structural build-rust build-java
	@$(GO_ENV) GOCACHE=$(BUILD_DIR)/go-cache go test $(GO_FLAGS) ./...
	@cargo test --locked --manifest-path $(STRUCTURAL_DIR)/adapters/rust/Cargo.toml

test-typescript: build-typescript
	@npm --prefix $(TYPESCRIPT_DIR) test

test: test-structural test-typescript

clean:
	@rm -rf $(BUILD_DIR) $(ROOT)/.slopslap-go $(STRUCTURAL_BIN) $(RUST_BIN) $(JAVA_JAR) $(GO_BIN) $(WATCH_BIN) $(TS_NATIVE_BIN) $(TYPESCRIPT_DIR)/dist $(TYPESCRIPT_DIR)/node_modules
