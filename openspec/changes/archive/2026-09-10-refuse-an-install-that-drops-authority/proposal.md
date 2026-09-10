## Why

On 2026-09-10 installing both daemons removed a signed generation 4 from this
machine. The configurations handed to the installers were the ones the
documented procedure names — `private/<domain>-observe.json` from the working
copy — and both were stale: neither carried `policy_control`, and root's had
lost `pritunl_service_label`. The installers copied them over the live files
without comparing them, and two runtimes holding a signed authority became
runtimes holding none. Root additionally lost the named service that is the
whole of its rescue capability.

Every check in the path passed. The offline check the procedure requires
validates a configuration on its own, and a configuration that drops an
authority is not malformed — it is a different, valid configuration. Nothing
asked the only question that mattered: is this less than what is installed.

It was survivable because the policy stores are separate files that nothing
wrote to. That is luck about file layout, not a property anything guarantees.

## What Changes

The offline check can be given the configuration currently installed, and
refuses a candidate that drops any of its settings. The installers use it, keep
the configuration they replaced, and a daemon whose store holds an authority
its configuration cannot read says so rather than starting quietly without one.

## Capabilities

### Modified Capabilities

- `policy-generation-continuity`: the pre-installation check compares a
  candidate against what is installed rather than judging it alone, and an
  authority a runtime cannot read is reported rather than silently absent.

## Impact

- `internal/configreduction` (new) — the comparison, one function, used by both
  daemons.
- `internal/rootdaemon`, `internal/userdaemon` — the check gains the second
  input; a store holding an unreadable authority is reported.
- `scripts/macos/observe-root-launchd.sh`, `scripts/macos/observe-user-launchd.sh`
  — compare before replacing, and keep what was replaced.
- `docs/macos/root-observe.md`, `docs/macos/user-observe.md` — the procedure
  says what the refusal means and how to proceed deliberately.
