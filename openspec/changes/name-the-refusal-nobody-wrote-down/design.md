## Context

Root answers a rescue request through three layers, and only the innermost one
can write down a refusal.

```
ipc.Server            reports transport and response-validation errors only
  Dispatcher          mutation gate: precondition_failed, no logger
    Broker            envelope nobody took: internal_error, no logger
      pritunlRescuer  every refusal reported                    ← the only voice
```

The requirement that a refusal name its check was written for the innermost
layer and is honoured there. The two outer layers were built before it and
answer with the same code as a genuine precondition failure, so a reader cannot
see that a different thing happened.

## Decisions

**The gate writes its own refusal rather than delegating to the rescuer.**
Moving the gate inside the rescuer would put the runtime's standing to act at
all behind the evaluation of a particular act, which inverts them: a suspended
runtime would evaluate an act it may not perform, and a decision would be
reached that nothing may spend. The gate stays where it is and gains somewhere
to write.

**An envelope nobody took is not a refusal.** The broker's answer today is
`internal_error`, which is correct — nothing refused, the runtime failed to
answer. The defect is that it is silent and that the caller files it under
refusal. Both sides are corrected: the broker records it, and the caller stops
calling it a refusal.

**The reason for a request nothing answered is new, not borrowed.**
`recovery_failed` already means an attempt that was made and did not work.
`recovery_refused` means the other side looked and disagreed. Neither covers
the other side not looking. Reusing either would put the reader in the wrong
place, which is the fault being fixed.

**The dispatcher takes a reporter, not a logger.** It already takes handlers
rather than concrete types; a reporter keeps the package free of the logging
component vocabulary and keeps root's choice of which stream to write to in
`rootdaemon`, where the other refusal reporters already live.

## Risks

The gate refuses on a stable condition, so a suspended runtime under a busy
caller will write the same line repeatedly. The connectivity publisher already
solved this with a report-every-N counter; the same is used here rather than a
change gate, because a gate keyed on an unchanging reason would write once and
then hide a condition that persists.
