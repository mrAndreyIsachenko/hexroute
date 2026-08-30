.DEFAULT_GOAL := check

CONTAINER_IMAGE ?= hexroute-ingest:contract

.PHONY: build build-ctl build-ingress-observer build-ingress-probe build-observe-root build-observe-user build-policy build-policy-installer build-policy-qualification check container-build container-test fmt fuzz ingress-observer-release-test install-policy-qualification logs-policy-qualification policy-qualification-faults policy-qualification-restart-session policy-qualification-status policy-qualification-summary policy-qualification-arm-sleep postgres-test race secret-test shell-test shell-test-tools spec-check terraform-contract-test terraform-state-test terraform-test test uninstall-policy-qualification vet

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

build-policy-installer:
	mkdir -p bin
	go build -o bin/hexroute-policy-installer ./cmd/hexroute-policy-installer

build-policy-qualification:
	mkdir -p bin
	go build -o bin/hexroute-policy-qualification ./cmd/hexroute-policy-qualification

install-policy-qualification: build-policy-qualification
	scripts/macos/policy-qualification-launchd.sh install bin/hexroute-policy-qualification

uninstall-policy-qualification:
	scripts/macos/policy-qualification-launchd.sh uninstall

policy-qualification-status:
	scripts/macos/policy-qualification-launchd.sh status

policy-qualification-summary:
	bash scripts/macos/policy-qualification-summary.sh

policy-qualification-arm-sleep:
	scripts/macos/policy-qualification-launchd.sh arm-sleep

policy-qualification-restart-session:
	scripts/macos/policy-qualification-launchd.sh restart-session

policy-qualification-faults:
	scripts/macos/policy-qualification-faults.sh

logs-policy-qualification:
	scripts/macos/policy-qualification-launchd.sh logs

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

fuzz:
	go test ./... -run '^$$'

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

# The shell contracts reach for tools the Go gate does not. An absent one used
# to surface as the contract it was being used to check — "missing container
# contract pattern" when the truth was "no ripgrep" — so the dependency is
# declared here, once, by the target that runs them.
SHELL_TEST_TOOLS := rg terraform

shell-test-tools:
	@for tool in $(SHELL_TEST_TOOLS); do \
		command -v "$$tool" >/dev/null 2>&1 || { \
			printf 'shell-test requires %s, which is not installed\n' "$$tool" >&2; \
			exit 1; \
		}; \
	done

shell-test: shell-test-tools build-observe-root build-observe-user build-policy-installer build-policy-qualification
	bash -n scripts/baseline/*.sh scripts/macos/*.sh scripts/ops/*.sh scripts/*.sh tests/*.sh
	tests/baseline_archives_test.sh
	tests/emergency_restore_test.sh
	tests/container_contract_test.sh
	tests/observe_root_launchd_test.sh
	tests/observe_user_launchd_test.sh
	tests/provider_b_documentation_test.sh
	tests/policy_cli_boundary_test.sh
	tests/policy_installer_boundary_test.sh
	tests/policy_qualification_launchd_test.sh
	tests/policy_cloud_independence_test.sh
	tests/package_reachability_test.sh
	tests/operational_acceptance_drill_test.sh
	bash tests/reconciler_shadow_integration_test.sh
	tests/reconciler_qualification_documentation_test.sh
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
	scripts/openspec-drift-check.sh

check: fmt vet test race fuzz build shell-test secret-test spec-check
