# Telos — Makefile
# Unix-utility naming; state lives in GitHub, not in markdown.

GO        ?= go
BIN_DIR   ?= bin
HOST_BIN  ?= $(BIN_DIR)/telosd
HOST_PKG  ?= ./cmd/telosd
IMAGE     ?= telos:dev
PLATFORM  ?= linux/arm64

.PHONY: all build host seed test vet fmt tidy run accept docker-arm64 clean

all: vet test build

# Local acceptance check: boot the host and assert the contract + seed graph.
accept:
	./scripts/local-acceptance.sh

build: host

# Regenerate the seed ACS from cmd/genbootstrap and sync the embedded copy.
# bootstrap.acs (repo root) is canonical; cmd/telosd/bootstrap.acs is the
# embedded copy compiled into the binary. Keep them identical (guarded by a test).
seed:
	$(GO) run ./cmd/genbootstrap > bootstrap.acs
	cp bootstrap.acs cmd/telosd/bootstrap.acs

host:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(HOST_BIN) $(HOST_PKG)

run: host
	$(HOST_BIN)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

tidy:
	$(GO) mod tidy

# Build the ARM64 container image locally. Does NOT push or deploy — AgentCore
# deployment is a separate, gated step.
docker-arm64:
	docker build --platform $(PLATFORM) -t $(IMAGE) .

clean:
	rm -rf $(BIN_DIR) dist
