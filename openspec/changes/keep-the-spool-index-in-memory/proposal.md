## Why

`keep-the-index-in-memory` returned about 2.7 seconds of a pause of six to
thirteen and said plainly that the rest was unaccounted for and wanted another
profile rather than another guess.

The profile already taken had the answer in it. Read as a call tree rather than
a leaf histogram, it names two frames from this repository:
`eventarchive.parseStableName` and `spool.parseStableName`, with
`spool.scanIndex` beside them. The archive was half of it. The spool is the
other half, and it is the same defect in the sibling store: `Append` lists the
directory every time, and the spool lives inside the connectivity state
directory that holds 107,600 files.

The two are paid together. The connectivity journal mirrors every published fact
into both, which is the "paid twice per published fact" the archive's own
requirement already mentions.

## What Changes

The spool learns its directory once and keeps that current as it publishes and
evicts, exactly as the archive now does. Anything else that moves a stable
record drops it instead of amending it.

## Capabilities

### Modified Capabilities

- `bounded-spool-operation-cost`: appending costs the write, not a directory
  listing.

## Impact

- `internal/spool` — the listing is kept, amended on commit, dropped elsewhere.
- Not in scope: the retention window that admits 456MB across 176,000 files.
