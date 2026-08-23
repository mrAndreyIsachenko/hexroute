---
name: dod-gate
description: Close out a Hexroute task or change against the repository Definition Of Done — scope, boundary, spec truth, regression evidence, the right gates for what actually changed, rollback and reconciliation. Use before marking a task complete, before committing implementation work, or when asked whether something is done.
allowed-tools: Bash(make:*), Bash(go:*), Bash(git:*), Bash(openspec:*)
license: MIT
compatibility: Hexroute repository only. Requires Go toolchain and openspec CLI; some gates require docker or terraform.
metadata:
  author: local
  version: "1.0"
---

Decide whether work in this repository is actually done, and say plainly what is
not. `AGENTS.md` states the Definition Of Done in prose; this skill executes it.

**Done is not "the code works".** In a repository that is replacing a live
production runtime, done means: the spec describes what was built, a regression
test would have caught the bug, the right gates ran for what actually changed,
nothing private leaked into public Git, and the change can be reversed.

**This skill never deploys.** It does not install, activate, cut over, restart a
runtime or touch Twilight. Hexroute is pre-cutover: live changes go through an
explicit guarded cutover, which is its own change with its own evidence.

---

## 1. Establish the scope

```bash
git status --short
git diff --name-only
OPENSPEC_TELEMETRY=0 openspec list --json
```

Name the change this work belongs to and read its `tasks.md`. Every modified
path must map to a task; a modified path with no task is either scope creep
(revert or split it out) or a missing task (add it, then say so).

If the work belongs to no change and is not a narrow low-risk fix, stop: it
needed a proposal first.

## 2. Repository boundary

This repository is public. Before anything else, confirm the diff carries no
live provider identity, private endpoint, credential, Terraform root state or
deployment evidence — those belong to `hexroute-infra`, and current runtime
behavior belongs to `twilight`.

```bash
make secret-test
```

`internal/secretguard` and `internal/repositoryguard` mechanize this, including
the canary fixtures. They are necessary, not sufficient: read new `testdata/`,
fixtures, docs and test names yourself. A real hostname in a test fixture passes
Go tests and is still a leak.

Stop and report if anything private appears. Do not "redact and continue"
silently — the file may already exist in the working tree's history.

## 3. Spec truth

The specs must describe what was built, not what was planned:

- Tick a task only when its work exists. Never tick to make the gate pass.
- If the implementation diverged from the delta spec, fix the **delta** first
  (`openspec-update-change`), then continue. The code is not the record.
- New behavior needs a testable WHEN/THEN scenario, including the failure and
  recovery paths, not only the happy path.
- Every touched baseline capability needs a regression scenario proving the old
  behavior still holds.
- Secret redaction and coexistence scenarios are required whenever local or
  cloud boundaries changed.

## 4. Regression evidence

Ask one question and answer it concretely: **which test fails without this
change?** Name it. If none does, the work is not done — for anything touching
recovery, activation or mutation authority, the test comes first.

For a bug fix, the test must reproduce the original failure, not merely exercise
the fixed path.

## 5. Run the gates for what actually changed

Always:

```bash
make check
```

That is `fmt vet test race fuzz build shell-test secret-test spec-check` — and
`spec-check` now includes the OpenSpec drift check, so a completed-but-unarchived
change fails here by design (see step 8). Do not set `OPENSPEC_DRIFT_ALLOW=1` to
get past it; that bypass exists for restoring service during an outage.

Then, based on the diff — these are **not** in `make check`:

| Changed | Gate | Needs |
|---|---|---|
| `internal/database/migrations/**`, schema, ingest persistence | `make postgres-test` | docker |
| `Dockerfile`, `cmd/hexroute-ingest`, container runtime contract | `make container-build container-test` | docker |
| `terraform/modules/**`, fixtures | `make terraform-test`, `make terraform-state-test` | terraform CLI |
| launchd/qualification runtime on this Mac | `make policy-qualification-status`, `-summary`, `-faults` | live macOS session |

`terraform-contract-test` and `ingress-observer-release-test` already run inside
`shell-test`; do not re-run them separately to pad the report.

**Report skipped gates as skipped.** If docker or terraform is unavailable, say
which gate did not run and why. A DoD report that omits an unrun gate is a false
report — that is the specific failure this skill exists to prevent.

## 6. Coexistence and privilege boundaries

For any change touching daemons, paths, labels, sockets, stores or the network:

- AdGuard untouched — never stopped, disabled or reconfigured.
- Both Codex paths (normal and Twilight fallback) still available.
- Hexroute paths, launchd labels, state, sockets and logs still disjoint from
  Twilight. No shared file, no shared label, no shared socket.
- Root network/process authority still separate from user Keychain/OTP access.
- Cloud components still telemetry-only: no cloud path can request a local
  mutation, and losing the cloud must leave local recovery working.

## 7. Rollback

For anything that can affect connectivity, activation or mutation authority:
the rollback must be written down, independently executable, and not depend on
the thing being rolled back. "Revert the commit" is not a rollback for a change
that installed or activated something.

If a rollback cannot be stated in one command or one short documented sequence,
the change is not done.

## 8. Reconcile and archive

If this closes the last open task in the change, run the `openspec-reconcile`
skill: sync the delta specs into the baseline, validate, archive. Until that
happens the baseline describes a system that no longer exists — and `make check`
will keep failing on the drift gate.

## 9. Commit shape

Small commits, each mapped to explicit tasks and its regression evidence. Do not
mix the baseline spec sync with unrelated implementation. Never commit private
evidence, live endpoints or `.local/` artifacts.

Commit only when asked.

---

**Output**

```
## DoD: <change> / <task or scope>

Scope           <n> files, all mapped to tasks (or: unmapped: <paths>)
Boundary        ✓ secret-test | manual review of <files>
Spec truth      ✓ delta matches implementation | scenarios: <added/updated>
Regression      ✓ <test name> fails without this change
Gates           ✓ make check | ✓ make postgres-test | ⚠ container-test SKIPPED (no docker)
Coexistence     ✓ AdGuard, Codex paths, disjoint labels/paths verified — or n/a
Rollback        <one line, or n/a for non-connectivity work>
Reconcile       archived / not the last task / pending

VERDICT: done | not done — <what is missing>
```

**Guardrails**
- Never declare done with a skipped gate unexplained, or a failing gate
  "unrelated". Report it.
- Never weaken a test, loosen an assertion or tick a task to turn a gate green.
- Never deploy, install, activate, cut over or restart anything from this skill.
- Never bypass the drift gate to close a task.
- Report the actual command output. If a gate was not run, say it was not run.
