# Tasks

## 1. Preserve The Evidence Before Destroying It

- [x] 1.1 Copy both stranded policy stores privately, before any code or configuration changes. The fix destroys the only naturally occurring instance of this defect, and it carries real signatures, a two-generation history and a genuinely superseded static digest at once. This is available exactly once.
- [x] 1.2 Record the measured starting state alongside it: what each domain reports, and which check refuses at which point in time. The claim afterwards is "this was stuck and now is not", and that needs a before.

## 2. Separate Lineage From Authority

- [x] 2.1 Add the lineage read. Assert it verifies the signature under the pinned key, every artifact digest, the immutable artifacts and the trusted compiler, and that it refuses when any of those fail. Assert it does not compare the predecessor's validity window or static digest against the present.
- [x] 2.2 Give it a return type that cannot authorize: generation numbers, payload digest, schema, and the predecessor's own validity and static digest as facts. Assert by construction that no caller can evaluate policy from it — the record carries no manifest, payload or approval.
- [x] 2.3 Use it in the installer and in both daemons at startup, so no current-generation value is ever taken from a configuration file while the store holds intact evidence. Assert an architectural boundary: the strict path is what governs, the lineage path is what chains, and neither substitutes for the other.
- [x] 2.4 Assert the relaxation does not leak to the candidate. A candidate is still refused for a wrong static digest, an untrusted compiler, a bad signature, or a window it is outside.

## 3. Stop Calling Expiry A Fault

- [x] 3.1 Report an expired active generation as `state: none, reason: expired`, and raise no suspension. Assert mutations remain refused, and that they are refused for the absence of an active generation rather than for a suspension.
- [x] 3.2 Assert `expired` is distinguishable from `no_valid_generation`, because one of them means the store holds a chain to build on.
- [x] 3.3 Assert a successor generation restores the active state without a daemon restart.

## 4. Announce The End Before It Arrives

- [x] 4.1 Surface `expires_at` in the status both daemons report.
- [x] 4.2 Raise a bounded non-critical `policy_expiry` incident at seven days remaining, at forty-eight hours, and once lapsed and unreplaced. Assert one announcement per threshold per generation, that the night window defers it rather than waking the operator, and that no policy content reaches the text.
- [x] 4.3 Assert the category is its own rather than borrowed, so the operator is never shown a security failure for a calendar event.

## 5. Make The Configuration Checkable

- [x] 5.1 Add an offline check for a prepared daemon policy configuration that calls the same validation the daemon applies. Assert it is the same function and not a second statement of the rules — a checker that drifts supplies confidence immediately before an irreversible step.
- [x] 5.2 Keep fail-closed startup. Assert an invalid policy control block still refuses to start rather than degrading, because a daemon that quietly runs without policy control is how this machine sat dormant for a month.

## 5b. Run The Thing Before Believing It

- [x] 5b.1 Add a smoke test that starts the real daemon, drives it over the real socket, and asserts every response it produces is one the daemon could have assembled. Nothing in this repository ran a daemon before it: thirty gates checked imports, plists, structure and documentation, and all of them passed while `policy status` returned an internal error on the machine.
- [x] 5b.2 Record what it cannot reach and why, measured rather than assumed. The policy store path derives from the operator's real home directory — `user.Current()` ignores HOME on macOS — so a test must not give the daemon a policy control block. Reintroducing the defect that reached the machine leaves the smoke test green and fails the boundary test in `policycontrol`; assert at the boundary a value crosses, because running the daemon widens the net rather than replacing it.

## 6. Arm The Control Plane On This Machine

- [x] 6.1 Prepare both daemon configurations privately as whole files, not edits, with the previous versions kept beside them. Include both compiler digests in the trusted list: the predecessor was compiled by the August compiler and its lineage read still checks that.
- [x] 6.2 Run the offline check against both prepared files before either goes near a live path.
- [x] 6.3 Install and restart under the guarded procedure. Assert afterwards that both domains answer, that neither reports a suspension, and that nothing is authorized — the only generation in either store is all-deny and expired.

## 7. Prove It On The Real Thing

- [x] 7.1 Compile, sign and activate generation 3: the content of the expired generation 2, no new authority, and a nine-day window. Record the deviation from the thirty-day default and its reason.
- [x] 7.2 That activation is the live acceptance for section 2 — a real predecessor, expired, under a superseded static digest, with real signatures. Record what the install and activation reported.
- [x] 7.3 Observe the seven-day announcement arrive, roughly two days later. A test with an injected clock proves the code; this proves the incident reaches a person through the delivery path, which is the half that is new.

      Generation 3 expires 2026-09-16T07:56:38Z, so the seven-day threshold fell
      at 2026-09-09T07:56:38Z. The user daemon emitted `local_notification` with
      result `reported` at 2026-09-09T07:56:55Z — seventeen seconds later, inside
      one fifteen-second observation cycle. `reported` is emitted only when the
      dispatch returned `LocalDelivered`, so the incident went out through
      osascript rather than being computed and dropped.

      It was dispatched at severity `warning` on purpose: critical bypasses the
      night window, and a deadline seven days out is not worth waking anyone for.
      The crossing happened at 10:56 local, outside the night window, so nothing
      was deferred.

      It was retrieved from Notification Center and read. It arrives quietly on
      this machine — no banner, straight to the list — which is why it was not
      noticed at the time. For a deadline seven days out that is the right
      loudness; whether the lapsed stage deserves more is a separate question.

      The observation also surfaced a defect this task exists to catch, and a
      test with an injected clock could not have. Notification Center held five
      identical announcements for the same generation from 2026-09-07, and each
      one lands within a second of a `daemon_started` in the user daemon's log:
      07:53:57, 08:24:37, 08:38:50, 10:46:28 and 11:08:15, against twelve
      restarts that day. See 7.6.
- [x] 7.4 Write the thirty-day default into the operations runbook, with deviation permitted and a recorded reason required. Fourteen days was inherited by copy last time, and that is how this started.
- [x] 7.5 Document recovery from a lapsed generation in the runbook, next to the states table, so the `expired` reason has a row that says what to do.

- [x] 7.6 The announcement repeats on every restart, and this change's own
      specification forbids it: "crossing the same threshold is not announced
      again for the same generation". `notification.Service` keeps its delivery
      record in `entries map[deliveryKey]deliveryEntry`, built empty by
      `NewService`, so suppression lasts for the life of the process rather than
      the life of the generation. Twelve restarts on 2026-09-07 produced five
      identical announcements.

      This is not cosmetic. The announcement is worth having only if it is worth
      reading, and five copies of one message is how a person learns to skip it —
      which is what happened here.

      The delivery record now survives a restart, beside the other state the
      user daemon writes every cycle. Only deliveries whose identity outlives
      the process are kept, and the caller declares that, because the caller is
      what put the number in `Generation`: the Pritunl safe-mode notification is
      keyed by a state generation that counts from zero at start, and
      remembering against it would let a restart reach the same number and
      silence a genuine safe-mode entry.

      Confirmed on the host rather than only in tests. The first start after the
      fix announced once and wrote the record — 08:31:10Z started, 08:31:14Z
      delivered — and the next restart at 08:31:45Z announced nothing.

## 7b. Blocked On The Root Daemon

- [x] 7b.1 The activation cannot complete: the root daemon is unreachable over its IPC socket while burning most of a core, so `policy prepare` reaches the user domain and times out against root. No pointer has moved and the machine is safe; generation 3 is signed and installed in both stores, and its window runs to 2026-09-16.
- [x] 7b.2 The cause is not established. Three explanations were offered and all three were wrong: a lifetime-average CPU figure read as instantaneous, saturation attributed to the user daemon's publishes while the measurement was confounded by the diagnostic probes themselves, and an idle-when-unqueried claim contradicted by the daemon being unreachable with the user daemon stopped. The spool defect is real and reproduced, but that it causes this is not shown.
- [x] 7b.3 Diagnose under controlled measurement in its own change rather than between activation attempts. Each attempt stops an observer on a live machine to test a guess.

## 8. Verify

- [x] 8.1 Run `make check` and resolve every failure.
- [x] 8.2 For each new property, restore the defect it guards and confirm the named test fails. A test that still passes with the relaxation put back into the lineage path is measuring something else.
- [x] 8.3 Run `openspec validate recover-policy-generations-after-expiry --strict` and keep proposal, design, specs and tasks consistent with what was built.
- [x] 8.4 Sync the delta into the baseline specs and archive. This change closes on section 7 rather than on a soak: what it repairs is proven by a generation being installed and activated, and the announcement by being received.
