# Calibration results

A local model is worth pointing at code nobody has read only if it can be
shown to work on code somebody has. Every case here is a real defect from
this repository, paired with the fix that closed it, so each asks two
questions: does the model see the defect, and does it clear the fix.

```
CASE                               QUESTION      BEFORE     AFTER     
----                               --------      ------     -----     
01-summary-hides-a-gap             contradiction HOLDS      HOLDS     
01-summary-hides-a-gap             omission      VIOLATED   HOLDS     
02-stale-counted-as-rejected       contradiction VIOLATED   HOLDS     
02-stale-counted-as-rejected       omission      HOLDS      HOLDS     
03-host-sequence-reused            contradiction VIOLATED   HOLDS     
03-host-sequence-reused            omission      VIOLATED   HOLDS     
04-pointer-loss-wedges-writes      contradiction VIOLATED   HOLDS     
04-pointer-loss-wedges-writes      omission      VIOLATED   HOLDS     
05-user-domain-ignores-a-wake      contradiction VIOLATED   HOLDS     
05-user-domain-ignores-a-wake      omission      HOLDS      HOLDS     
06-control-duplicate-detection     contradiction HOLDS      HOLDS     
06-control-duplicate-detection     omission      HOLDS      HOLDS     

model qwen3.5:9b
defects seen        7
defects missed      3
fixes cleared       14
fixes falsely flagged 0
unreadable answers  0

A run that flags everything is worth nothing however many defects it
sees: read the two right-hand columns together, not the first one alone.
```

## What this says

Every defect was caught by at least one of the two questions, and no fix
was flagged — including the control, which was never broken. Zero false
flags on fourteen is the result that makes the thing usable at all: a
reviewer that cries wolf stops being read after the third time.

## What it does not say

A single answer is not a finding. Between two runs of this same bench,
cases 01 and 05 swapped which question caught them — what one run sees
the next misses. Ask three times and take the majority, or treat the
answer as a lead that is dead until it becomes a failing test.

The fragments were chosen by someone who knew the answer. A real audit
gets a package and has to find the place itself, so seven of ten is an
upper bound rather than a working rate.

And the worst defects this repository produced are not this shape at all.
A state the read model enters by itself and cannot leave has no
requirement it contradicts; it is found by starting twice, which no
amount of reading will do.

## Running it

```
bash scripts/audit/calibrate.sh          # qwen3.5:9b by default
AUDIT_MODEL=phi4-mini bash scripts/audit/calibrate.sh
```

It refuses to start if the model does not answer. A silent model and a
model with nothing to say produce the same empty output, and a scoreboard
built from that reads like a measurement.
