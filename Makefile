# Convenience targets - none of these are required; the assignment is driven by
# the `./redis-tool` wrapper.
#
# Works on Linux, macOS, and WSL2 Ubuntu.

VERSION ?= dev
LDFLAGS := -X github.com/redis-tool/redis-tool/cmd.Version=$(VERSION)

BIN := bin/redis-tool
RUN := ./redis-tool

.PHONY: build tidy fmt vet clean infra-up infra-down provision status seed verify upgrade demo help

help:
	@echo Targets:
	@echo "  build      - go mod tidy + go build -> $(BIN)"
	@echo "  tidy       - go mod tidy"
	@echo "  fmt        - gofmt -s -w ."
	@echo "  vet        - go vet ./..."
	@echo "  clean      - remove bin and .run"
	@echo "  demo       - full happy-path. Requires Ansible + Docker/Podman."

build: tidy
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .

tidy:
	go mod tidy

fmt:
	gofmt -s -w .

vet:
	go vet ./...

clean:
	@rm -rf bin
	@rm -rf .run

# ---- Runtime targets (need Ansible + Docker/Podman) ------------------------

infra-up: build
	$(RUN) infra up

infra-down: build
	$(RUN) infra down --yes

provision: build
	$(RUN) provision --version 7.0.15

status: build
	$(RUN) status

seed: build
	$(RUN) data seed --keys 1000

verify: build
	$(RUN) data verify

upgrade: build
	$(RUN) upgrade --target-version 7.2.6 --strategy rolling

# Full happy-path demo, captures output into the submission directory.
demo: build
	@mkdir -p output
	$(RUN) infra up                                          | tee output/00_infra_up.txt
	$(RUN) provision --version 7.0.15                        | tee output/01_provision_output.txt
	$(RUN) data seed --keys 1000                             | tee output/02_data_seed_output.txt
	$(RUN) status                                            | tee output/03_status_output.txt
	$(RUN) upgrade --target-version 7.2.6 --strategy rolling | tee output/04_upgrade_output.txt
	$(RUN) verify --full                                     | tee output/05_verify_output.txt
