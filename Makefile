# Convenience targets - none of these are required; the assignment is driven by
# the `./redis-tool` wrapper.
.PHONY: build tidy fmt vet lint clean infra-up infra-down provision status seed verify upgrade demo

BIN := bin/redis-tool
VERSION ?= dev

build: tidy
	@mkdir -p bin
	go build -ldflags "-X github.com/redis-tool/redis-tool/cmd.Version=$(VERSION)" -o $(BIN) .

tidy:
	go mod tidy

fmt:
	gofmt -s -w .

vet:
	go vet ./...

clean:
	rm -rf bin .run

infra-up:
	./redis-tool infra up

infra-down:
	./redis-tool infra down --yes

provision: build
	./redis-tool provision --version 7.0.15

status: build
	./redis-tool status

seed: build
	./redis-tool data seed --keys 1000

verify: build
	./redis-tool data verify

upgrade: build
	./redis-tool upgrade --target-version 7.2.6 --strategy rolling

# Full happy-path demo, captures output into the submission directory.
demo: build
	mkdir -p output
	./redis-tool infra up                                          | tee output/00_infra_up.txt
	./redis-tool provision --version 7.0.15                        | tee output/01_provision_output.txt
	./redis-tool data seed --keys 1000                             | tee output/02_data_seed_output.txt
	./redis-tool status                                            | tee output/03_status_output.txt
	./redis-tool upgrade --target-version 7.2.6 --strategy rolling | tee output/04_upgrade_output.txt
	./redis-tool verify --full                                     | tee output/05_verify_output.txt
