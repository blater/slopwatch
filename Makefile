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
GO_APP_ENV := $(GO_ENV)
ifeq ($(shell go env GOOS),darwin)
GO_APP_ENV := CGO_ENABLED=1 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
endif
GO_FLAGS := -trimpath -buildvcs=false
GO_TEST_FLAGS := -buildvcs=false -count=1
JAR_DATE := 1980-01-01T00:00:02Z

STRUCTURAL_GO_SOURCES := $(wildcard $(STRUCTURAL_DIR)/cmd/slopslap-structural/*.go) \
  $(wildcard $(STRUCTURAL_DIR)/internal/*/*.go)
STRUCTURAL_JAVA_SOURCES := $(wildcard $(STRUCTURAL_DIR)/adapters/java/src/dev/slopslap/structural/*.java)
STRUCTURAL_RUST_SOURCES := $(wildcard $(STRUCTURAL_DIR)/adapters/rust/src/*.rs)
GO_CALIBRATION := $(ROOT)/go/internal/sourceestimate/calibration_default.json
GO_SOURCES := $(shell find $(ROOT)/go/cmd/slopslap-go $(ROOT)/go/internal -type f -name '*.go')
WATCH_SOURCES := $(shell find $(ROOT)/go/cmd/slopwatch -type f -name '*.go')
TS_SOURCES := $(wildcard $(TYPESCRIPT_DIR)/src/*.ts) $(wildcard $(TYPESCRIPT_DIR)/test/*.ts)

.PHONY: all build build-artifacts dev-build build-structural build-rust build-java build-go \
  build-typescript test test-clean test-structural test-go test-typescript clean

all: build

# Standard builds always run the same suite as CI and releases.
build: test

# Internal compilation prerequisite; avoids recursion through build -> test.
build-artifacts: build-structural build-rust build-java build-go build-typescript

# Normative source conformance, including currently unsupported capabilities.
# This deliberately fails when any expected result is not delivered.
.PHONY: test-shallow-adapters
test-shallow-adapters: build-artifacts
	@python3 tools/shallow_adapter_acceptance.py

.PHONY: test-shallow-carriers
test-shallow-carriers: build-artifacts
	@python3 tools/shallow_carrier_acceptance.py

.PHONY: test-shallow-values
test-shallow-values: build-artifacts
	@python3 tools/shallow_value_acceptance.py

dev-build: build-structural build-typescript build-go
ifeq ($(filter test%,$(MAKECMDGOALS)),)
	@$(MAKE) --no-print-directory clean-go-cache
endif

$(STRUCTURAL_BIN): $(STRUCTURAL_GO_SOURCES) $(STRUCTURAL_DIR)/go.mod
	@mkdir -p $(dir $@) $(BUILD_DIR)/go-cache
	@$(GO_ENV) GOCACHE=$(BUILD_DIR)/go-cache go build -C $(STRUCTURAL_DIR) $(GO_FLAGS) -o $@ ./cmd/slopslap-structural

build-structural: $(STRUCTURAL_BIN)

$(RUST_BIN): $(STRUCTURAL_RUST_SOURCES) $(STRUCTURAL_DIR)/adapters/rust/Cargo.toml $(STRUCTURAL_DIR)/adapters/rust/Cargo.lock
	@mkdir -p $(BUILD_DIR)/cargo-target
	@CARGO_TARGET_DIR=$(BUILD_DIR)/cargo-target cargo build --locked --release --manifest-path $(STRUCTURAL_DIR)/adapters/rust/Cargo.toml
	@cp $(BUILD_DIR)/cargo-target/release/slopslap-structural-rust $@.tmp
	@mv -f $@.tmp $@

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

$(GO_BIN): $(GO_SOURCES) $(GO_CALIBRATION) $(ROOT)/go/go.mod $(ROOT)/go/go.sum
	@mkdir -p $(dir $@) $(BUILD_DIR)/go-cache
	@$(GO_APP_ENV) GOCACHE=$(BUILD_DIR)/go-cache go build -C $(ROOT)/go $(GO_FLAGS) -o $@ ./cmd/slopslap-go

$(WATCH_BIN): $(WATCH_SOURCES) $(GO_SOURCES) $(GO_CALIBRATION) $(ROOT)/go/go.mod $(ROOT)/go/go.sum
	@mkdir -p $(dir $@) $(BUILD_DIR)/go-cache
	@$(GO_APP_ENV) GOCACHE=$(BUILD_DIR)/go-cache go build -C $(ROOT)/go $(GO_FLAGS) -o $@ ./cmd/slopwatch

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
	@$(GO_ENV) GOCACHE=$(BUILD_DIR)/go-cache SLOPSLAP_JAVA_TEST_JAR=$(JAVA_JAR) go test -C $(STRUCTURAL_DIR) $(GO_TEST_FLAGS) -parallel=4 ./...
	@CARGO_TARGET_DIR=$(BUILD_DIR)/cargo-target cargo test --locked --release --manifest-path $(STRUCTURAL_DIR)/adapters/rust/Cargo.toml

test-typescript: build-typescript build-structural
	@cd $(TYPESCRIPT_WORK_DIR) && SLOPSLAP_DEPTH_EVALUATOR=$(STRUCTURAL_BIN) node --test dist/test/*.test.js

test-go: build-artifacts
	@$(GO_APP_ENV) GOCACHE=$(BUILD_DIR)/go-cache go test -C $(ROOT)/go $(GO_TEST_FLAGS) ./...

test: test-structural test-typescript test-go
	@bash util/test-distribution.sh

test-clean: clean
	@$(MAKE) test

# Compiler intermediates are useful within a build, but must not accumulate
# across normal builds. Keep the finished binaries; only discard this cache.
# Test goals defer build cleanup so parallel test prerequisites can finish first.
.PHONY: clean-go-cache
clean-go-cache:
	@rm -rf "$(BUILD_DIR)/go-cache"

clean:
	@rm -rf $(BUILD_DIR) \
	  $(STRUCTURAL_BIN) $(RUST_BIN) $(JAVA_JAR) $(STRUCTURAL_DIR)/java-runtime \
	  $(STRUCTURAL_DIR)/adapters/rust/target

.PHONY: test-shallow-sensitivity test-shallow-holdout test-shallow-calibration test-shallow-safeguards test-shallow-regressions test-shallow-context test-shallow-fresh test-shallow-high test-shallow-confirmation

test-shallow-sensitivity:
	@mkdir -p $(BUILD_DIR)/go-cache
	@$(GO_ENV) GOCACHE=$(BUILD_DIR)/go-cache SHALLOW_SENSITIVITY_OUTPUT=$(BUILD_DIR)/shallow-sensitivity.json go test -C $(ROOT)/go $(GO_TEST_FLAGS) ./internal/sourceestimate -run TestCalibration -count=1 -v

test-shallow-regressions:
	@$(GO_ENV) GOCACHE=$(BUILD_DIR)/go-cache go test -C $(ROOT)/go $(GO_TEST_FLAGS) ./internal/sourceestimate ./internal/native ./internal/report ./internal/scoring ./internal/follow ./internal/fixanalysis/nativeadapter

test-shallow-high: build-artifacts
	@status=0; python3 $(ROOT)/tools/shallow_holdout_acceptance.py --manifest $(ROOT)/docs/evidence/shallow-v4/connected-high-holdout/manifest.json --require-high-language go --require-high-language typescript --require-high-language rust --output $(BUILD_DIR)/shallow-high-holdout.json || status=1; python3 $(ROOT)/tools/shallow_holdout_context.py --manifest $(ROOT)/docs/evidence/shallow-v4/connected-high-holdout/manifest.json --context $(ROOT)/docs/evidence/shallow-v4/connected-high-context.json --frozen-only --output $(BUILD_DIR)/shallow-high-context.json || status=1; exit $$status

test-shallow-confirmation: build-artifacts
	@status=0; python3 $(ROOT)/tools/shallow_holdout_acceptance.py --manifest $(ROOT)/docs/evidence/shallow-v4/connected-confirmation-holdout/manifest.json --require-high-language go --require-high-language rust --output $(BUILD_DIR)/shallow-confirmation-holdout.json || status=1; python3 $(ROOT)/tools/shallow_holdout_context.py --manifest $(ROOT)/docs/evidence/shallow-v4/connected-confirmation-holdout/manifest.json --context $(ROOT)/docs/evidence/shallow-v4/connected-confirmation-context.json --frozen-only --output $(BUILD_DIR)/shallow-confirmation-context.json || status=1; exit $$status

test-shallow-fresh: build-artifacts
	@python3 $(ROOT)/tools/shallow_holdout_acceptance.py --manifest $(ROOT)/docs/evidence/shallow-v4/structural-fresh-holdout/manifest.json --allow-no-high-cases --output $(BUILD_DIR)/shallow-fresh-holdout.json

test-shallow-context: build-artifacts
	@python3 $(ROOT)/tools/shallow_holdout_context.py --frozen-only --output $(BUILD_DIR)/shallow-holdout-context.json

test-shallow-holdout: build-artifacts
	@python3 $(ROOT)/tools/shallow_holdout_acceptance.py

test-shallow-safeguards:
	@python3 -m unittest discover -s $(ROOT)/tools -p 'test_shallow*.py'
	@python3 $(ROOT)/tools/shallow_calibration_change.py
	@$(GO_ENV) GOCACHE=$(BUILD_DIR)/go-cache go test -C $(ROOT)/go $(GO_TEST_FLAGS) ./internal/native -run TestCalibration -count=1

# Keep producing both evaluation artifacts when either independent check fails.
test-shallow-calibration:
	@status=0; $(MAKE) test-shallow-sensitivity || status=1; $(MAKE) test-shallow-holdout || status=1; $(MAKE) test-shallow-context || status=1; $(MAKE) test-shallow-fresh || status=1; $(MAKE) test-shallow-high || status=1; $(MAKE) test-shallow-confirmation || status=1; $(MAKE) test-shallow-safeguards || status=1; $(MAKE) test-shallow-regressions || status=1; exit $$status
