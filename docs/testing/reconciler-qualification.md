# Generation-Bound Reconciler Qualification

This document defines the synthetic-only qualification gate for the
generation-bound network reconciler. It is evidence for the engine boundary; it
does not enable production adapters, live shadow cutover or mutation authority.

The first rollout remains synthetic. Twilight stays the production owner of live
networking, AdGuard, Pritunl, sing-box, routes, DNS and both Codex access paths.

## Gate

Run the gate before enabling the synthetic runtime feature flag:

```sh
make check
```

`make check` includes these reconciler-relevant suites:

- `make test`: unit, schema-boundary, crash-recovery, replay and IPC package
  tests.
- `make race`: race coverage for action lifecycle, cancellation, compensation
  and operation-session transitions.
- `make fuzz`: fuzz-smoke compilation across all Go packages. Today this is
  `go test ./... -run '^$'`; when explicit `Fuzz*` entrypoints are added, this
  target is the stable place to run them.
- `bash tests/reconciler_shadow_integration_test.sh`: disabled-gate IPC,
  peer-authentication, cloud-loss and capability-leak checks.
- `make secret-test`: secret-canary and repository-boundary checks.
- `make spec-check`: strict OpenSpec validation for all active and baseline
  requirements.

The gate is not satisfied by a single package test. It must cover unit, race,
fuzz-smoke, crash-recovery, replay, IPC, schema-boundary, secret-canary and
capability-leak evidence in one `make check` run.

## Interpretation

Passing qualification means deterministic synthetic traces replay to the same
canonical sessions, checkpoints, plans, transitions, outcomes and provenance.
It also means repository checks did not find production mutation dependencies in
the reconciler or synthetic adapter surface.

Passing qualification does not grant production route, DNS, firewall, process,
tunnel, Pritunl, Keychain, OTP or credential authority. Every real adapter, live
shadow qualification and ownership cutover requires a separate grill session,
OpenSpec change and independently executable rollback.
