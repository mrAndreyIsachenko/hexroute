## Context

Three things had to hold for a stale file to remove a signed authority, and
each is a separate place this could have been caught.

```
the operator runs the documented command with a working-copy config
  the offline check validates it alone                  ← asks "is this valid"
    the installer copies it over the live file          ← asks nothing
      the daemon starts, finds no policy_control        ← asks nothing
        the store still holds an active generation      ← nobody asks
```

Only the last line is unambiguous evidence that something is wrong: a store
holding an authority the runtime is not configured to read. The other two are
places where the wrong answer was returned because the wrong question was
asked.

## Decisions

**The comparison is over key paths, not values.** A configuration legitimately
changes values on every install — a new interval, a rotated pinned key, a
different probe address. What it should not do without being told is stop
carrying a setting the installed one carries. So the rule is mechanical: every
object key path present in the installed configuration must be present in the
candidate. Adding is free.

**Arrays are not descended into.** A route or endpoint list shrinking is a real
reduction, but a legitimate one, and indexing into arrays would report a
reordering as a loss. Treating the array as one key keeps the rule free of
judgement, which is what makes it safe to enforce by refusing.

**The check refuses; it does not warn.** A warning printed by a `sudo` command
in a terminal is read after the copy has happened. The refusal is the whole
value: this failure was invisible precisely because everything printed success.

**The bypass is an environment variable, not a flag.** `HEXROUTE_ALLOW_REDUCED_CONFIG=1`
matches `OPENSPEC_DRIFT_ALLOW` — a deliberate act that reads as deliberate in
shell history, and one nobody types by habit. Deliberately stepping a domain
back to no policy is a real operation and must stay possible.

**The replaced configuration is kept.** One copy, beside the installed one,
owned and masked the same way. Reconstructing what was lost on 2026-09-10 took
an hour of matching digests against a manifest to find which of two signer
directories was the right one; a copy of the file would have made it a move.
It is not a version history — one copy answers "what did I just replace".

**The daemon reports rather than refuses.** A root daemon that will not start
observes nothing, which is worse than one observing without authority. So a
store holding an active generation the configuration cannot read is an event,
not a fatal error. It is the only one of the three checks that works no matter
how the configuration arrived — including by hand, which no installer guards.

## Risks

A first install has nothing to compare against, and must not be refused. The
installers pass the second input only when a configuration is already in place,
so the absent case is not a special rule inside the check — it is a call that
is not made.

The daemon's new report needs a store to consult before it knows whether it can
read one. It reads the store's active pointer only to answer whether something
is there, and never to act on it: a runtime without `policy_control` has no
pinned key and cannot validate what it finds, so the event says an authority is
present and unreadable, never that it is valid.
