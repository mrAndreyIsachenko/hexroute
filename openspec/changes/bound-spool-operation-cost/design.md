# Design

## What was measured

`sample(1)` was misleading twice: it showed every thread parked in
`pthread_cond_wait` while the process held most of a core. `fs_usage` returned
nothing at all, almost certainly refused by system integrity protection.
`spindump` answered, and its on-CPU samples are unambiguous:

    encoding/json.stateInString        30
    jsoncanonicalizer.Transform        28   (three frames)
    encoding/json.(*Decoder).readValue 23
    runtime.mallocgcSmallNoscan        14
    runtime.scanobject                 14
    encoding/json.appendCompact        11

with `__open` and `read` in the same bucket. That is the signature of decoding
and canonicalising stored records, over and over.

The store behind it:

    connectivity/user/spool   40,237 files   50 MB
    connectivity/root/spool   20,421 files   26 MB

Both inside the hundred-megabyte bound. Nothing overflowed.

And the cost reproduces without the machine: 13.2ms to append with 200 stable
entries, 45.4ms with 1,600. Seeding 1,800 appends took forty-seven seconds.

## The line

`Spool.Append` calls `scanStable`, which reads the whole directory and calls
`readEntry` on every file. `readEntry` reads the file, decodes the record,
decodes and validates the event inside it — which canonicalises it to check a
digest — and then re-marshals the whole entry to compute a size, which it
compares against the size the filesystem reported in the `Lstat` it already did.

Six operations call `scanStable`: `Open`, `Append`, `Entries`,
`EntriesBySequenceRanges`, `Size` and `Acknowledge`. Asking the spool how large
it is re-proves every record in it.

## Cost, not count

The obvious repair is to bound the entry count. It was rejected.

The byte bound is deliberate, documented and holding: fifty and twenty-six
megabytes against a hundred. The system is doing precisely what is written down
and is unusable while doing it, which means the specification is missing a
statement about cost rather than about size.

Bounding the count would also decide something else in passing. The spool is a
drain buffer, and its only drain is the cloud uploader — `telemetry/batch.go` is
the sole caller of `Acknowledge`. That uploader does not run on this machine, so
the spool fills and stays full. Evicting by count would silently discard
observations the design promises to keep until acknowledged. Whether
undeliverable telemetry should age out is a real question; it should be answered
because someone decided it, not because something was slow.

## Prove what you use

The principle that replaces the rescan is one sentence: **the integrity of a
stored record is proved when it is read for use, not when it is counted.**

`Append` does not use stored records. It needs the highest sequence, the total
size, and enough per-record detail to choose evictions. Sequences are in the
filenames. Sizes are in the file metadata — the same metadata already read, and
already trusted enough to compare a recomputed value against.

`Entries` and `EntriesBySequenceRanges` do hand records out, so they decode and
prove them. That is where the cost belongs, and it is paid by the uploader, on
the path that is about to send the data somewhere.

An in-memory index was the other candidate and was rejected. This spool re-reads
because disk is the truth: a crash, another process or an operator could have
changed it between operations. A cache reintroduces exactly the divergence the
re-reading exists to avoid, and it diverges without saying so.

## What a damaged record should stop

Today, one unreadable file makes `scanStable` return `ErrCorruptSpool`, which
fails `Append`, which fails `Journal.Append`, which fails the publication. A
record damaged three weeks ago stops the machine recording what is happening
now.

That trade is backwards. The damaged record is already lost and refusing to
append does not recover it; what refusing costs is every future observation. So
a damaged record is set aside and reported, and the spool keeps recording.

Set aside, not deleted. The damaged record is the only evidence of whatever
damaged it, and this repository has already learned that lesson the expensive
way — the stranded policy stores were snapshotted this morning precisely because
applying a fix destroys the only naturally occurring instance of the fault.

The channel already exists: `IncidentSpoolOverflow` is a spool incident
category, so the spool already has a way to say something is wrong with it.

## The publication deadline

The user daemon publishes each cycle through `ipc.Client{Path: path}` with no
`Timeout`, so it takes `ioTimeout` — fifteen seconds — against an observation
interval of fifteen seconds. One slow peer therefore consumes the entire cycle.

The graceful path is already written, and its comment is right: a publication
that fails is abandoned rather than buffered, because a fact held back and sent
later would describe a moment that has passed. Only the waiting is wrong.

Deriving the deadline from the interval rather than fixing a constant keeps the
property that matters — the wait is shorter than the cycle — if the interval is
ever reconfigured. A constant would drift out of that relationship silently.

This is not merely a consequence of the spool being slow. It is the coupling
that carried a fault in root into a symptom in user, and it cost hours of
misdirected diagnosis today: a healthy user daemon that would not answer, while
the damage was somewhere else entirely.

## Why the existing store is left alone

After this change the size of the spool stops mattering, so cleaning it becomes
unhurried rather than urgent. Removing seventy-six megabytes of undelivered
telemetry means declaring it worthless, which is the deferred question again.

That the size stops mattering is a claim, and it is a task rather than a
sentence: the cost test runs at the scale the machine actually reached. Seeding
sixty thousand entries is itself only affordable once the fix exists, which is a
neat inversion — the repair pays for its own proof.

## Proving it

Three levels, and the third decides.

The spool test measures what the defect is about: cost against entry count,
mutation-checked by restoring the rescan.

The journal test covers the layer the daemon actually uses. The spool is not
what `hexrouted` calls; the journal is, and a mistake fits between them.

The live acceptance is the one that counts, because today demonstrated that a
green gate says nothing about the machine. Sixty thousand real records exist in
one place, and the acceptance is stated as behaviour rather than as a number:
the root daemon answers `policy status` promptly while the user daemon
publishes. It cannot do that now.

There is no smoke test for the root daemon to sit beside the user daemon's one.
It refuses to configure its socket unless it is running as root, and a unit gate
must not run privileged.
