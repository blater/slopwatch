SHELL := /bin/sh

ROOT := $(abspath .)
BUILD_DIR := $(ROOT)/build
STRUCTURAL_DIR := $(ROOT)/analyzers/structural
TYPESCRIPT_DIR := $(ROOT)/analyzers/typescript
TYPESCRIPT_WORK_DIR := $(BUILD_DIR)/typescript-work
TYPESCRIPT_RUNTIME_DIR := $(BUILD_DIR)/typescript
STRUCTURAL_BIN := $(STRUCTURAL_DIR)/slopslap-structural
RUST_BIN := $(STRUCTURAL_DIR)/slopslap-structural-rust
JAVA_JAR := $(STRUCTURAL_DIR)/slopslap-structural-java.jar
JAVA_RUNTIME_BIN := $(STRUCTURAL_DIR)/java-runtime/bin/java
GO_BIN := $(BUILD_DIR)/slopmark
WATCH_BIN := $(BUILD_DIR)/slopwatch
TS_MARKER := $(TYPESCRIPT_WORK_DIR)/dist/src/cli.js
TS_LAUNCHER := $(TYPESCRIPT_RUNTIME_DIR)/slopslap-typescript

GO_ENV := CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
GO_FLAGS := -trimpath -buildvcs=false
GO_TEST_FLAGS := -buildvcs=false
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
  build-typescript test test-clean test-structural test-go test-typescript clean

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

$(JAVA_RUNTIME_BIN): $(JAVA_JAR)
	@rm -rf $(STRUCTURAL_DIR)/java-runtime
	@jlink --add-modules java.base,jdk.compiler --strip-debug --no-man-pages --no-header-files --compress=2 --output $(STRUCTURAL_DIR)/java-runtime

build-java: $(JAVA_JAR) $(JAVA_RUNTIME_BIN)

$(GO_BIN): $(GO_SOURCES) $(ROOT)/go/go.mod $(ROOT)/go/go.sum
	@mkdir -p $(dir $@) $(BUILD_DIR)/go-cache
	@$(GO_ENV) GOCACHE=$(BUILD_DIR)/go-cache go build -C $(ROOT)/go $(GO_FLAGS) -o $@ ./cmd/slopslap-go

$(WATCH_BIN): $(WATCH_SOURCES) $(ROOT)/go/go.mod $(ROOT)/go/go.sum
	@mkdir -p $(dir $@) $(BUILD_DIR)/go-cache
	@$(GO_ENV) GOCACHE=$(BUILD_DIR)/go-cache go build -C $(ROOT)/go $(GO_FLAGS) -o $@ ./cmd/slopwatch

build-go: $(GO_BIN) $(WATCH_BIN)

$(TS_MARKER): $(TS_SOURCES) $(TYPESCRIPT_DIR)/package.json $(TYPESCRIPT_DIR)/package-lock.json $(TYPESCRIPT_DIR)/tsconfig.json
	@rm -rf $(TYPESCRIPT_WORK_DIR)
	@mkdir -p $(TYPESCRIPT_WORK_DIR)
	@cp -R $(TYPESCRIPT_DIR)/src $(TYPESCRIPT_WORK_DIR)/src
	@cp -R $(TYPESCRIPT_DIR)/test $(TYPESCRIPT_WORK_DIR)/test
	@cp $(TYPESCRIPT_DIR)/package.json $(TYPESCRIPT_DIR)/package-lock.json $(TYPESCRIPT_DIR)/tsconfig.json $(TYPESCRIPT_WORK_DIR)/
	@npm --prefix $(TYPESCRIPT_WORK_DIR) ci --ignore-scripts
	@npm --prefix $(TYPESCRIPT_WORK_DIR) run build

$(TS_LAUNCHER): $(TS_MARKER) $(TYPESCRIPT_DIR)/slopslap-typescript.sh $(TYPESCRIPT_DIR)/package.json $(TYPESCRIPT_DIR)/package-lock.json
	@rm -rf $(TYPESCRIPT_RUNTIME_DIR)
	@mkdir -p $(TYPESCRIPT_RUNTIME_DIR)/dist
	@cp -R $(TYPESCRIPT_WORK_DIR)/dist/src $(TYPESCRIPT_RUNTIME_DIR)/dist/src
	@cp $(TYPESCRIPT_DIR)/package.json $(TYPESCRIPT_DIR)/package-lock.json $(TYPESCRIPT_RUNTIME_DIR)/
	@npm --prefix $(TYPESCRIPT_RUNTIME_DIR) ci --omit=dev --ignore-scripts
	@cp $(TYPESCRIPT_DIR)/slopslap-typescript.sh $(TS_LAUNCHER)
	@chmod 755 $(TS_LAUNCHER)

build-typescript: $(TS_LAUNCHER)

test-structural: build-structural build-rust build-java
	@$(GO_ENV) GOCACHE=$(BUILD_DIR)/go-cache go test -C $(STRUCTURAL_DIR) $(GO_TEST_FLAGS) ./...
	@cargo test --locked --manifest-path $(STRUCTURAL_DIR)/adapters/rust/Cargo.toml

test-typescript: build-typescript
	@npm --prefix $(TYPESCRIPT_WORK_DIR) test

test-go: build
	@$(GO_ENV) GOCACHE=$(BUILD_DIR)/go-cache go test -C $(ROOT)/go $(GO_TEST_FLAGS) ./...

test: test-structural test-typescript test-go

test-clean: clean
	@$(MAKE) test

clean:
	@rm -rf $(BUILD_DIR) \
	  $(STRUCTURAL_BIN) $(RUST_BIN) $(JAVA_JAR) $(STRUCTURAL_DIR)/java-runtime \
	  $(STRUCTURAL_DIR)/adapters/rust/target
