# Tasks

## 1. The tunnel process is the owner's

- [x] 1.1 The process observer reads command lines and matches a sing-box by the configuration it runs.
- [x] 1.2 The claim records the configuration its holder runs; a v1 claim reads as this runtime's default.
- [x] 1.3 Possession, release and the daemon's `process_gone` ask for the owner's process, never the first by name.
- [x] 1.4 A test that an ingress probe's sing-box is not taken for the tunnel, in each of the three.

      The observer is asked with the probe listed before the tunnel, as it is
      whenever its pid is the lower, and with a probe alone; the incumbent and
      the daemon's cycle are tested on the configuration they ask for rather
      than on what the fake returns. An unreadable claim is a failed
      observation, not a lost tunnel.

      `expected_sing_box_parent_pid` is still accepted and no longer used. The
      decoder refuses unknown fields and the live configuration could not be
      read without root, so removing it could have stopped the daemon starting.

      Mutations, 2026-09-14: ten applied, ten killed — any sing-box running any
      configuration, no path boundary, any executable running the tunnel, the
      cycle asking for another configuration, the owner option not wired, an
      unreadable claim read as no tunnel, a v1 claim read as absent, a claim
      placed without a configuration, the incumbent not asked by its
      configuration. One more did not compile and is not counted; it was
      replaced by the two valid forms above it.

      "Any executable" survived its first run. The test's other lines were
      refused for not saying `run`, so the executable check was never reached.
      It carries the right command line under another name now.

## 2. Release

- [x] 2.1 Sessions record their kind, and release's phases.

      `kind` is empty on every session written before this, and empty reads as
      a handover — tested on a session file in that shape. The phases are
      `stopping`, `released` and `restored`.
- [x] 2.2 Stop own, release claim, prove Twilight's tunnel by two consecutive traversals within the deadline.

      Tested on the order, not on counts: stop-own, then release. A tunnel that
      will not stop keeps its claim and nothing else is started; a tunnel that
      already died still gives the claim back.
- [x] 2.3 Restore this runtime's tunnel and claim when Twilight does not take it.

      In the handover's order: claim, then any tunnel the previous owner raised
      late, then this runtime's own. A claim that could not be released is
      restored over rather than failed on — the fake refuses a second placement
      with the real store's error, which is what found the first version
      matching against a private error that could never occur.
- [x] 2.4 `check` for release: the previous owner reads claims, its configuration and binary are present, this runtime holds the claim.

      `hexroute-handover check-release`. It also asks whether the previous
      owner's supervisor is running at all: without it nothing raises the tunnel
      after the claim goes, and the release would spend its deadline and take the
      tunnel back. It prints what it does not check.
- [x] 2.5 A later invocation finds a release in flight and undoes exactly its phase.

      At `stopping` the tunnel is started again under the claim that was never
      released; at `released` the claim is placed again and the tunnel started;
      at `prepared` nothing is touched. `abort` builds what an undo needs when
      it is given the flags, and aborts a handover as before when it is not.

## 3. Twilight

- [x] 3.1 Take a new carrier baseline on the first tick without a claim.

      The claimed branch empties the baseline; the watchdog already takes an
      empty one as new. Twilight PR 18.
- [x] 3.2 A test that a baseline frozen under a claim is not compared against after release.

      Driven both ways. The first version anchored on the first
      `if tunnel_claimed; then` in the file, which has no `else`, and failed
      with the fix in place — so a green result would have meant nothing.

## 4. Gates and evidence

- [x] 4.1 Mutate identification, the order of stop and release, the restore path and the baseline; confirm the named tests fail.

      Identification: ten, all killed (recorded under 1.4).

      Release: nine killed — the claim released before its own tunnel stops, no
      restore when the previous owner does not take it, a restore that does not
      take a late tunnel, keep releasing the claim, a held claim breaking the
      restore, abort treating a release as a handover, stopping without waiting,
      a release beginning beside one in flight, an empty kind reading as a
      release. Three first forms were not counted: one anchor matched twice,
      one left an import unused, one was unreachable code that vet refused. Each
      was redone in a valid form and killed.

      The command: four killed — the release check passing with nothing held,
      forgetting the other runtime, writing a claim, and release running with
      nothing held. The last first removed the refusal outright, which left a
      variable unused and did not compile; it was recorded as killed before
      that was noticed, and was then redone as a refusal that can never fire,
      and killed.

      The baseline in Twilight: driven both ways under 3.2.
- [ ] 4.2 `make check` green in both repositories.
- [ ] 4.3 Run `release` on the machine and record what the tunnel did.

## 5. Close

- [ ] 5.1 Sync the delta into the baseline, validate, archive.
