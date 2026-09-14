# Design

## Release is the handover run backwards, with the same discipline

A written procedure is not a transaction, which is why the handover is one. The
way back is used more often than the way in — every soak and every rollback —
so it gets the same treatment rather than a runbook line.

Phases, each recorded before the act it names:

| phase | means | undone by |
|---|---|---|
| `prepared` | nothing touched | nothing |
| `stopping` | this runtime is stopping its own sing-box; the claim is still held | starting it again from the signed version |
| `released` | the claim is gone; Twilight owns the tunnel and is expected to raise it | starting this runtime's tunnel again and placing the claim again |
| `proven` | two consecutive traversals through Twilight's tunnel | — |
| `restored` | Twilight did not raise it in time; this runtime runs it again and holds the claim | — |

A session records which transaction it belongs to — `kind: handover` or
`kind: release` — so a later invocation that finds one knows which undo applies.
A session without a kind is a handover, which is every session written before
this change.

## Order: stop, then release

Stopping first leaves an interval with no tunnel while the claim is held, which
Twilight does not act on. It lasts as long as it takes to remove a file.
Releasing first leaves two runtimes each believing the tunnel is theirs: Twilight
starts a second sing-box against an interface the first still holds.

After release, Twilight notices on its next tick — sixty seconds at most — that
its own process is gone, and restarts. Its measured restart was fifty-seven
seconds end to end, so the deadline is the handover's: one hundred and twenty.

## If Twilight does not take it

`release` starts this runtime's tunnel again from the signed version and places
the claim again. Leaving the machine with no tunnel and no owner until a person
reads the terminal is the arrangement the handover's design rejected, and it is
no better in this direction.

## The tunnel process is identified by its configuration

Twilight finds its own sing-box as `sing-box run -c <its configuration>`. This
runtime took the first process whose executable was named `sing-box`, and
Twilight's ingress probe runs `sing-box run -c /tmp/twilight-vless-ingress.*`.
The two were told apart on 2026-09-14 only by pid order.

The owner decides which configuration: a claim on disk means this runtime's,
none means Twilight's. The claim carries the path of the configuration its
holder runs (`hexroute.tunnel-claim.v2`), because a claim that did not say which
process it covers would leave the reader to guess. A v1 claim, which is the one
on disk at the time of writing, is read as covering this runtime's default
content path.

The process observer reads `args` rather than `comm`. The path contains a space,
so the columns before it are parsed by position and the remainder is the command
line.

## Twilight's carrier baseline

The supervisor takes its carrier signature when its monitor loop starts and
updates it only while the claim is absent. A supervisor started while the claim
was held keeps a signature from that moment — 11:14 for the one running now —
and on release compares the current carrier against it: one rebuild for a change
that happened while it was not watching, or none for a change that did. The
baseline is taken again on the first tick without a claim.
