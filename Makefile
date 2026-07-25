.DEFAULT_GOAL := check

CONTAINER_IMAGE ?= hexroute-ingest:contract

.PHONY: build build-ctl build-observe-root build-observe-user check container-build container-test fmt postgres-test race secret-test shell-test terraform-contract-test terraform-state-test terraform-test test vet

build:
	go build ./cmd/...

build-observe-root:
	mkdir -p bin
	go build -o bin/hexrouted ./cmd/hexrouted

build-observe-user:
	mkdir -p bin
	go build -o bin/hexroute-userd ./cmd/hexroute-userd

build-ctl:
	mkdir -p bin
	go build -o bin/hexroutectl ./cmd/hexroutectl

container-build:
	docker build -t "$(CONTAINER_IMAGE)" .

container-test:
	tests/container_runtime_test.sh "$(CONTAINER_IMAGE)"

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	@test -z "$$(gofmt -l .)" || { gofmt -d .; exit 1; }

postgres-test:
	tests/postgres_migrations_test.sh

terraform-contract-test:
	tests/terraform_contract_test.sh

terraform-test:
	tests/terraform_modules_test.sh

terraform-state-test:
	tests/terraform_state_policy_test.sh

shell-test: build-observe-root build-observe-user
	bash -n scripts/baseline/*.sh scripts/macos/*.sh scripts/terraform-state-policy.sh tests/*.sh
	tests/baseline_archives_test.sh
	tests/emergency_restore_test.sh
	tests/container_contract_test.sh
	tests/observe_root_launchd_test.sh
	tests/observe_user_launchd_test.sh
	tests/terraform_contract_test.sh
	tests/terraform_state_policy_test.sh

secret-test:
	go test ./internal/secretguard -run TestRepositorySecretCanaries -count=1

check: fmt vet test race build shell-test secret-test
