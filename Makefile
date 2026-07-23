.DEFAULT_GOAL := check

.PHONY: build build-observe-root check fmt race secret-test shell-test test vet

build:
	go build ./cmd/...

build-observe-root:
	mkdir -p bin
	go build -o bin/hexrouted ./cmd/hexrouted

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	@test -z "$$(gofmt -l .)" || { gofmt -d .; exit 1; }

shell-test: build-observe-root
	bash -n scripts/baseline/*.sh scripts/macos/*.sh tests/*.sh
	tests/baseline_archives_test.sh
	tests/emergency_restore_test.sh
	tests/observe_root_launchd_test.sh

secret-test:
	go test ./internal/secretguard -run TestRepositorySecretCanaries -count=1

check: fmt vet test race build shell-test secret-test
