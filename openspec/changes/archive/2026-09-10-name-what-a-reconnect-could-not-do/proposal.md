## Why

A reconnect that fails reports one thing: that it failed.

`Client.Reconnect` can stop at eight distinct places — no client, an unreadable
clock, a one-time-code window too short, credentials it could not read, a PIN it
could not use, a code it could not derive, nothing to submit, and a client that
did not start the session. All eight arrive at the log as `recovery_failed`.

That is the last thing standing between this system and knowing whether its user
half works. `cut-pritunl-recovery-to-hexroute` archived with the reconnect half
unproven, and the plan was to wait for a real fault. A real fault would produce
the same undistinguished `recovery_failed`, so waiting proves nothing until the
attempt names what it hit.

Observed on 2026-09-09 at 20:54:07Z: the runtime attempted a reconnect and
failed. Two explanations fit and neither is established — credentials an
unattended agent cannot read, and a client refusing to connect while Pritunl was
already reconnecting the session three seconds later. Choosing between them
required reading source, and reading source cannot say which one happened.

This is the fourth path in this system found reporting a failure without saying
which check produced it. The others were the IPC layer, the rescue refusal, and
the difference between being unable to act and acting and failing.

## What Changes

- The reconnect's distinct failures become named errors rather than message
  text, so a caller can tell them apart with `errors.Is`.
- The user runtime records which one it hit, beside the failure it already
  records.
- Nothing about when a reconnect is attempted, or what it submits, changes.

Not breaking. The umbrella error stays, so a caller that only needs to know the
attempt failed is unaffected.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `pritunl-recovery-ownership`: the requirement that a refusal names the check
  that made it extends to a runtime's own failed attempt. It already covers what
  the answering runtime refused and whether this one could act at all; what it
  does not cover is an attempt that was made and did not work.

## Non-goals

- **Why the observed failure happened is not decided here.** This change makes
  the next one legible; it does not guess between the two explanations.
- **The induction's race is not removed.** Removing the session's address makes
  Pritunl reconnect it, and an attempt landing in that window may fail for that
  reason. A named failure of "the client did not start the session" would be an
  artefact of the method rather than an answer about a real fault, and the
  reading has to allow for it.
- **No credential is read to find out.** The suspicion about the Keychain is not
  tested by running the command that reads a secret.

## Impact

- `internal/pritunlclient`: the errors it returns.
- `internal/userdaemon`: the outcome it records.
- `internal/logging`: the reasons it admits.

## Ownership boundary

Public Hexroute. Generic daemon code, no provider identity, no live endpoint, no
deployment evidence. Twilight remains the active production owner and nothing
here changes that.

## Rollout

An ordinary build and install of the user daemon, which needs no password. The
root daemon is untouched.

## Rollback

Reinstall the previous user daemon binary with the same script. Nothing on disk
changes shape, so a binary from before this reads everything written after it.
