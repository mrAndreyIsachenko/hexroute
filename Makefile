.DEFAULT_GOAL := check

CONTAINER_IMAGE ?= hexroute-ingest:contract

.PHONY: build build-ctl build-ingress-observer build-ingress-probe build-observe-root build-observe-user build-policy check container-build container-test fmt ingress-observer-release-test postgres-test race secret-test shell-test spec-check terraform-contract-test terraform-state-test terraform-test test vet

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

build-policy:
	mkdir -p bin
	go build -o bin/hexroute-policy ./cmd/hexroute-policy

build-ingress-probe:
	mkdir -p bin
	go build -o bin/hexroute-ingress-probe ./cmd/hexroute-ingress-probe

build-ingress-observer:
	mkdir -p bin
	go build -o bin/hexroute-ingress-observer ./cmd/hexroute-ingress-observer

ingress-observer-release-test:
	tests/ingress_observer_release_test.sh

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
	bash -n scripts/baseline/*.sh scripts/macos/*.sh scripts/*.sh tests/*.sh
	tests/baseline_archives_test.sh
	tests/emergency_restore_test.sh
	tests/container_contract_test.sh
	tests/observe_root_launchd_test.sh
	tests/observe_user_launchd_test.sh
	tests/provider_b_documentation_test.sh
	tests/policy_cli_boundary_test.sh
	tests/policy_cloud_independence_test.sh
	tests/policy_documentation_test.sh
	tests/operator_resume_boundary_test.sh
	tests/policy_signer_profile_host_test.sh
	tests/ingress_observer_release_test.sh
	tests/terraform_contract_test.sh
	tests/terraform_state_policy_test.sh

secret-test:
	go test ./internal/secretguard ./internal/repositoryguard -count=1

spec-check:
	OPENSPEC_TELEMETRY=0 openspec validate --all --strict --no-interactive

check: fmt vet test race build shell-test secret-test spec-check
