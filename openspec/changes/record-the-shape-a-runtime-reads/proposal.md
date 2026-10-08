# Record the shape a runtime reads

## Why

The repository holds no record of the configuration its daemons run on.

Measured 2026-10-08. The public example, `deploy/macos/root-observe.example.json`,
carries **46 settings against the 112** the machine's root configuration holds.
It has no `tunnel_supervision` at all and no `policy_control`: it predates the
policy control plane and the whole of tunnel observation, including the executor
that `hold-the-tunnel-under-a-grant` built. Ten of the settings it lacks —
`tunnel_supervision.execution` entire, with its thresholds, its state paths, its
handover binary, `sing_box` and the target key — exist **only on one machine**.

The working copy had fallen behind the same way, so an install that passed it
would have removed those ten. That was caught, and only by accident of
procedure: the comparison exists, `hexrouted --check --config X --installed Y`
refuses a candidate that drops a setting and names what would be lost, and
`policy-generation-continuity` already requires it. But it has no occasion. It
is reached when an install is attempted, which is after the operator has decided
to install, and nothing anywhere requires the example to keep up. So every
change that added a setting wrote it into the live machine and into nothing
else, for two months, and the only thing standing between a stale file and the
running executor was a guard that fires mid-install.

A configuration whose shape is recorded nowhere cannot be reconstructed, cannot
be reviewed, and cannot be compared against except by reading the one machine
that has it.

## What Changes

- The public example configuration becomes the **record of the shape**: it
  SHALL carry every setting the daemon's configuration decoder accepts, with
  placeholder values and no live ones.
- A gate SHALL refuse a setting the decoder accepts and the example omits, so a
  setting cannot be added to a runtime without being recorded in the repository.
  The gate reads the decoder and the example, both of which are in the
  repository, so it needs no privilege and no machine.
- **Values stay out.** The example carries keys and placeholders. This adds
  nothing to what the public repository may hold, and the existing requirement
  that it hold no live deployment state is unchanged — the gate asserts the
  shape and must not assert a value.
- The user domain's example gets the same treatment.
- **Not a new comparison.** `policy-generation-continuity`'s pre-install check
  is right as it stands. This change gives it something to be checked against
  and makes the omission visible in the repository rather than on one host.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `repository-and-deployment-boundary` — it owns what the public repository must
  and must not hold. It gains a requirement that the shape of every
  configuration a runtime reads is in the repository, which is the same boundary
  stated from the other side: the shape is public, the values are not.

## Impact

- `deploy/macos/root-observe.example.json` and the user domain's example — both
  grow to the decoder's full shape with placeholder values.
- `tests/` — one new gate comparing the decoder's accepted settings against the
  examples.
- `internal/rootdaemon/config.go` and `internal/userdaemon/config.go` — read by
  the gate, not changed by it.
- `docs/macos/root-observe.md` and `docs/macos/user-observe.md` — the section
  saying the working copy is not the record gains the comparison to run before
  an install, which today an operator learns only by attempting one.
- The working copy's own divergence from this machine is carried forward once
  inside this change. It is a task with a recorded reading, not a requirement:
  `private/` is not in the repository and nothing here can hold it true.
- Nothing is installed, no runtime is restarted, and no live value enters Git.
