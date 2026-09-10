# Tasks

## 1. Prove the silence before changing it

- [x] 1.1 A regression that fails against the current code: a request refused by
      the mutation gate leaves a record naming the gate, and a request no reader
      took leaves a record naming that.
- [x] 1.2 A regression that fails against the current code: the asking runtime
      reports a refusal and a failure of the runtime it asked as different
      reasons.
- [x] 1.3 Confirm by mutation — collapse each new record and each new reason back
      into what it replaced, and confirm the named test fails. Drive the
      mutations through the dispatcher and through the cycle, not through the
      helper that maps a code to an outcome: three times in the previous change
      a helper-level mutation passed while the call site was untested.

## 2. Give the outer layers a voice

- [x] 2.1 The dispatcher takes a reporter and calls it when the mutation gate
      refuses. The reason names the gate, not the act.
- [x] 2.2 The broker records an envelope no reader took, on the same path that
      answers `internal_error`.
- [x] 2.3 Root wires both to the stream its other refusal reports already use,
      and bounds repetition the way the connectivity publisher does.

## 3. Keep the two apart at the caller

- [x] 3.1 The unnamed-code arm of `refusalOutcome` gets its own outcome, distinct
      from the precondition arm.
- [x] 3.2 It maps to its own logging reason, and it reports a degraded result
      rather than a refusal: nothing refused this.
- [x] 3.3 `recovery_refused` keeps its meaning — the other side looked and
      disagreed — and stops carrying the case where it did not look.

## 4. Gates

- [x] 4.1 `make check`, judged by exit status.
- [x] 4.2 `make secret-test`, and read the new test names and fixtures by hand.

## 5. Prove it on the machine

- [x] 5.1 Install root and the user daemon; confirm each running binary by digest
      and by the revision it carries.

      Both are installed and both were confirmed by digest and by the revision
      the binary carries, not by the installer's report: root
      `82b2719ed9d53199b`, user `bdcd8868b222b9c2a`, each from `55f7213` with a
      clean tree.

      Installing them cost this machine its policy state and taught something
      the procedure does not say. `docs/macos/*-observe.md` gives the install
      command with `private/<domain>-observe.json` as the config argument, and
      the installer copies that file over the installed one without comparing
      them. Both checkout copies were stale: neither carried `policy_control`
      and the root one had lost `pritunl_service_label`. So the install
      silently replaced a runtime holding an active generation 4 with one
      holding no policy at all, and root additionally lost the named service
      that is the whole of its rescue capability. Both daemons reported
      `policy_state=none, bundle_generation=0` afterwards.

      Nothing was lost that could not be rebuilt, because the policy stores are
      separate from the configs and nothing wrote to them. The blocks were
      reconstructed from the active manifest — `compiler_sha256`,
      `static_sha256` and `policy_schema` — and from the signer's public key,
      which was found by matching its digest against the manifest's
      `signer_fingerprint` rather than by trusting a path: the first candidate
      under `.local` was a different, older signer and was refused. Both
      domains came back to bundle 4, active, expiring 2026-10-07.

      The hazard is worth naming beyond this change: an installer that
      overwrites a live configuration with an unversioned working copy will do
      this again, and the only reason it was survivable is that the policy
      store is a different file.
- [x] 5.2 Reproduce a gate refusal and a not-taken envelope against a running
      daemon, using a request that cannot perform the act, and read both
      records.

      One of the two was reproduced. Both records are made in
      `internal/operator`, which both daemons wire identically, so the domain
      the reproduction runs in decides only whose log receives it.

      **The envelope nobody took: reproduced.** A spare user daemon was run on
      its own socket, state and logs, with no policy control and a client that
      answers more slowly than the socket's deadline allows. A request for a
      mutating action, carrying a generation the daemon cannot be at, waited
      15.002 seconds and the caller gave up; the daemon recorded
      `ipc_request_rejected / request_not_taken` at 15.002 seconds, followed by
      the write to the connection the caller had already closed. Nothing of the
      installed runtime was touched, and the spare was removed afterwards.

      A second identical request was not recorded, which is the repetition
      bound working: the first of a repeating refusal is always written and
      then every sixtieth. That is deliberate and it means the log says a
      condition began rather than how many times it held.

      **The gate refusal: not reproducible here.** The gate closes only when
      authorization is suspended or when a policy store holds no active
      generation. Both domains carry an active generation 4, the store is at a
      fixed path that ignores the environment, and a second process opening it
      can write to it. Every route to the closed state therefore changes
      production policy state, and none was taken. Its coverage is the
      regression and the mutation that collapses it.
- [x] 5.3 Record what each said.

      `request_not_taken`, from a live daemon, at the moment the request
      context expired. Before this change the same request produced an internal
      failure on the wire and nothing in any log, which is exactly what a
      refused rescue looked like on 2026-09-10.

      The asking side of that pair could not be read here: a caller whose own
      deadline expires first records a transport failure rather than an
      answer, so `root_internal` is reached only when the answering runtime
      returns inside the deadline. Both halves are covered by regressions; only
      the answering half has been seen on a machine.

      What this leaves standing is worth saying plainly. Neither of the two
      conditions can be induced against the installed root: its loop pauses 6.6
      to 13.1 seconds against a 15-second deadline, and its gate cannot be
      closed without suspending an authority this system exists to protect.
      That is the third time in this capability that the thing worth observing
      cannot be produced on demand, after a service launchd repairs faster than
      hexroute observes and a session Pritunl restores whatever the autostart
      flag says.

## 6. Close

- [x] 6.1 Sync the delta into the baseline and archive.
