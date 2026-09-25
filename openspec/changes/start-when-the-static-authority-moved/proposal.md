## Why

Changing static authority is supposed to be a reviewed configuration
installation and a guarded restart, after which the successor generation is
installed and activated. On 2026-09-25 that sequence was performed on this
machine and deadlocked: with the new static digest installed, neither daemon
would start, and activation goes through their sockets.

The generation already active was compiled against the envelope that came
before, so recovering it fails with `restart_required` — and a handler that
cannot recover an active generation returns an error instead of coming up. Both
daemons refused and launchd restarted them every ten to twenty seconds, until
the configuration was rolled back.

A crash loop is worse than the state it refuses to represent. It removes
observation and the operator socket along with mutation authority, and it leaves
no way forward: the only act that resolves a static mismatch — installing and
activating the successor — needs the daemons that will not start.

## What Changes

- A daemon whose stored active generation was compiled against a different
  static authority SHALL start. It reports `restart_required` with reason
  `static_mismatch`, naming the generation it cannot run, and performs no
  mutation.
- The state and the reason already exist in the vocabulary, and are already
  produced when a candidate is prepared against the wrong static authority. This
  change makes a daemon reach the same conclusion about its own store at
  startup, rather than refusing to run.
- The status names the generation from the store's lineage, which is the one
  reading that deliberately does not compare the predecessor's static digest.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `atomic-policy-generations`: what a daemon does at startup when the static
  authority installed for it no longer matches the generation in force. The
  capability already says a candidate needing a different static configuration
  is refused live; it does not say what becomes of a daemon on the other side of
  that change, and the implementation refuses to run.

## Impact

- `internal/policycontrol`: the handler's construction, where a failure to
  recover the active generation is classified.
- `internal/policystore`: read only — the lineage it already exposes is what
  names the generation.
- `docs/macos/policy-operations.md`: the sequence for changing static authority,
  which can be written down as executable once it is.

## Non-goals

- No change to what may be activated. A candidate whose static digest differs
  from the installed one is still refused, and is still resolved by installing
  the configuration it needs rather than by forcing it through.
- No change to any other startup failure. Corruption, an invalid signature, a
  clock anomaly and an unreadable store keep the answers they have.

## Rollout

The daemons are restarted with the new binaries in the ordinary way. Nothing
about an installed generation changes, and a machine whose static authority
matches its generation behaves exactly as it does today.

## Rollback

Reinstall the previous binaries and restart. The state this change adds is
reported rather than stored: nothing on disk carries it, so there is nothing to
undo.

## Production ownership boundary

This is the local control plane on the operator's own machine. Twilight owns the
tunnel throughout and is untouched; the cloud is not involved.
