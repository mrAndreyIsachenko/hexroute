# Design

## Context

See proposal.md — Why. The state that shapes the approach:

`Archive.Append` calls `scan()` before it does anything else, and `scan()` does
an `os.ReadFile` and a `json.Unmarshal` for every stored record. Two callers
need what scan returns: `partitionByAge`, which reads `Metadata.WallClock` out of
each record, and the size check, which needs the total and — when it evicts —
each record's priority.

Three facts about the store decide the design:

- Records are named `%020d.event` by a monotonic sequence, so lexical order is
  sequence order and the directory alone gives every sequence.
- Size is `Lstat` on each entry. It never needed a read.
- Age and priority live inside the record. They are the only two things that do.

The spool solved the same problem and its solution is in the baseline as
`bounded-spool-operation-cost`: read only what you hand out, take sequences and
sizes from the directory, read records only when evicting. The archive is harder
in exactly one place — the spool has no age bound, so it never needed a
timestamp from inside a record.

## Goals / Non-Goals

Design-level only; the proposal already fixes the scope.

**Goal.** The common append — the one that evicts nothing — reads no stored
record except at most one, and its cost does not grow with the archive.

**Non-goal.** Making the eviction path cheap. Eviction rewrites the directory
anyway, and choosing a victim by priority is a question only the records can
answer. Cost there is proportional to what is being evicted, which is the right
shape.

**Non-goal.** An in-archive index or sidecar file. It would make the append
cheaper still, and it would introduce a second source of truth that has to
survive a crash, agree with the directory, and be repaired when it does not. The
directory is already crash-safe and already authoritative.

## Decisions

### The oldest retained record is the lowest retained sequence

Age eviction needs to know whether anything has expired. Reading every record to
find out is the defect. The alternative is to read the one record that can
answer it.

Sequences are handed out monotonically and records are appended in order, so the
lowest retained sequence is the oldest retained record. The append reads that
one file. If its stamp is inside the window, nothing has expired and no other
record is read. If it is outside, the walk continues upward and stops at the
first record inside the window — reading exactly the expired set plus one.

*Alternative rejected: a timestamp in the filename.* It removes the last read
entirely, and it costs a migration of the 41,492 records already on disk plus a
filename format that everything reading the archive would have to learn. The
change would stop being about cost.

*Alternative rejected: file mtime as the age.* Free, and wrong. `mtime` is a
property of the file, not of the event. A restore, a copy or a backup tool
rewrites it, and the archive would then answer for a window it is not covering
while reporting the window it thinks it is. An archive whose retention claim can
quietly become false is worse than a slow one.

### The monotonicity this relies on, and what happens when it does not hold

The walk assumes sequence order implies stamp order. That holds while the wall
clock moves forward. It does not hold across a clock that steps backwards, and
this host is a laptop that sleeps.

The consequence is bounded and is not corruption: a record stamped earlier than
one below it stops the walk sooner than a full scan would, so the archive
retains a record it could have evicted. It retains too much, never too little,
and the next append re-evaluates from the new lowest sequence. Retaining an
expired record is a smaller fault than dropping a retained one, and the direction
of the error is fixed rather than incidental.

This is a real narrowing of behaviour and the spec says so: the scenarios are
written about what is read, not about an exact eviction set.

### Size stays where it is, priority still costs a read

Total size comes from `Lstat`, which is already what the directory gives. The
byte bound can therefore be evaluated without a read at all.

Choosing what to evict cannot: eviction removes diagnostics before operational
before critical, and priority is inside the record. So the size path keeps
reading — but only when the bound is actually reached. Below the bound there is
nothing to choose and nothing is read.

That preserves the existing requirement in `local-event-archive` exactly:
priority-ordered eviction, an overflow record naming what was dropped, and a
refusal rather than a silent drop when only critical records remain.

### The mirror stays synchronous

`journal.Append` writes to the spool and then to the archive in the same call, so
a fact is in both stores or in neither. Once the append is cheap there is nothing
to buy by decoupling them, and decoupling would trade that property for facts
lost between the journal and the archive on a crash — with a lag that would then
have to be observed and reported to be trustworthy.

## Risks / Trade-offs

**A clock step backwards makes the archive retain more than its age bound.** →
Bounded, self-correcting, and in the safe direction: too much retained, never too
little. Re-evaluated on the next append. Stated in the spec.

**A damaged lowest record could stall age eviction.** → It is set aside and
reported on the path that reads it, exactly as the spool does, and the walk
continues from the next sequence. A record that cannot be proved must not stop
the observation still being made.

**A future reader adds a second unconditional scan and the defect returns.** →
This is the second time this shape has appeared, in the second of two stores. The
regression test asserts the property — that appending does not read stored
records — rather than a duration, so a reintroduced scan fails the test on any
machine rather than only on a slow one.

**The measured numbers age.** → They are evidence for the requirement, not the
requirement. The test asserts behaviour; the numbers stay in the spec as the
record of what it cost when it was wrong.

## Migration Plan

None on disk. No record changes shape, no filename changes, and no index is
introduced, so an archive written before this change is read by the code after it
and the reverse. Deployment is an ordinary root daemon build and install;
rollback is reinstalling the previous binary with the same script.

## Ownership

Unchanged by this design. The archive is root-owned local state under the root
daemon's own directory; nothing here touches the user domain, Keychain or OTP
access, AdGuard, either Codex path, or any Twilight-owned runtime. No cloud
component reads or writes the archive, and losing the cloud changes nothing about
it: the archive is local evidence, and every path that repairs it is local.
