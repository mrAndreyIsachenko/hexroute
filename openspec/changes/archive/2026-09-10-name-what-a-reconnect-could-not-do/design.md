# Design

## Context

See proposal.md — Why.

`Client.Reconnect` already distinguishes its failures, in message text:
`credentials unavailable`, `PIN unavailable`, `one-time code unavailable`,
`nothing to submit`, `the client did not start the session`. All of them wrap one
sentinel, `ErrReconnect`, so `errors.Is` cannot tell them apart and the text is
the only carrier. `recovery.perform` maps any error to `recoveryFailed`, and the
text is dropped there.

`ErrWindowTooShort` is already its own sentinel and is already distinguishable.
It is the shape the others should have.

## Goals / Non-Goals

**Goal.** A caller can tell the failures apart without parsing a message.

**Non-goal.** Changing when a reconnect is attempted, what it submits, or the
order of its steps. This is about what the failure says, not about what the
attempt does.

**Non-goal.** Deciding what the observed failure was. Naming makes the next one
legible; it does not retroactively explain the last.

## Decisions

### Named sentinels that keep their umbrella

Each distinct stop becomes its own error, wrapping `ErrReconnect` the way
`ErrOuterNotReady` and `ErrServiceNotStale` wrap `ErrPrecondition` in the rescue
path. A caller that only needs to know the attempt failed matches the umbrella
and is unaffected; one that needs to know where matches the specific error.

*Alternative rejected: parsing the message.* It works until someone rewords a
sentence, and nothing tells them not to.

*Alternative rejected: an error code enum returned alongside the error.* Two
things to keep in step, and the standard library already has the mechanism.

### The outcome carries the step, not the message

`recoveryOutcome` gains values for the distinct failures, as it already has for
the distinct refusals. The message stays out of the log: the logging vocabulary
is a closed allowlist on purpose, and a message is free text that would carry
whatever a future error string happens to contain — including, one day, something
that should not be in a log at all.

### What is deliberately not distinguished

`no client` and `nothing to submit` are faults in the deployment or in this
code, not conditions of the machine. They stay under the general failure: a
reader who sees them is reading a bug report, not a diagnosis.

## Risks / Trade-offs

**A named failure that is an artefact of the induction.** → Removing the
session's address makes Pritunl reconnect it, so an attempt landing in that
window may fail because a connect is under way. That is the method's fault, not
the machine's, and the spec's non-goal says so. The reading has to allow for it.

**More vocabulary in the log allowlist.** → Four reasons, each mapping to one
step a reader would go and look at. The alternative is the one word that sent
yesterday's reading to the wrong place twice.

## Migration Plan

None. No stored shape changes. Install the user daemon; the root daemon is
untouched. Rollback is reinstalling the previous binary.

## Ownership

Unchanged. The user runtime holds the credentials and this does not alter what
it does with them; it alters what it says afterwards. No cloud component reads
any of it.
