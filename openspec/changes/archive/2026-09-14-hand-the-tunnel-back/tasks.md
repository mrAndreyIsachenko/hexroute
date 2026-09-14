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

- [x] 1.5 A command line elsewhere of any length does not stop the tunnel being found.

      Found by `check-release` on the machine, 2026-09-14, before `release` ran:
      "this runtime's tunnel — invalid process observation". Reading `args`
      instead of `comm` brought in a length bound of 4,096 bytes that refused the
      whole listing. Measured on the machine: 1,005 processes, two of them with
      command lines of 5,758 and 6,109 bytes, both unrelated. So the tunnel
      could be neither found nor declared absent — for any owner.

      The daemon installed at 13:07 from this branch carried it, and observed a
      failure on every cycle since. It performs nothing, but its decisions from
      then read the process as gone; they are to be read back and set aside
      rather than compared.

      Read back after reinstalling the fix at 13:26:02Z: seventeen decisions
      between 13:07:29Z and 13:25:56Z, every one `rebuild_tunnel` on
      `process_gone` with `process_running: false`, while the tunnel ran
      throughout. Those seventeen are set aside. `check-release`, run right
      after through the same observer, found the tunnel as pid 44094.

      No test could see it, because every test's listing was a few short lines.
      The listing is now invalid only when its columns are, and a test carries a
      7,000-byte line beside the tunnel; putting a bound back fails it.

- [x] 1.6 Only a root sing-box is the tunnel.

      Measured before deciding: this runtime's tunnel runs as uid 0, and the
      previous owner starts its own with `sudo -n "$SING_BOX_BIN" run -c
      "$CONFIG"` from a supervisor whose launchd job runs as root, with no
      privilege drop anywhere. So requiring uid 0 excludes no real tunnel. What
      it excludes is any user on the machine running a sing-box that names the
      owner's configuration — counted, it would hide a lost tunnel, or be what a
      release stops as root. The previous owner's ingress probe is root too; it
      is told apart by its configuration, not by this.

      Tested with a user's sing-box listed before root's; removing the check
      fails it.

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
- [x] 4.2 `make check` green in both repositories.

      Twilight on its branch before PR 18 merged; this repository on its branch
      after every change above, with the exit status read directly rather than
      through a pipe.
- [x] 4.3 Run `release` on the machine and record what the tunnel did.

      Installed from both branches first. `check-release` refused once — the
      listing defect under 1.5 — and after the fix held on every precondition,
      including that the previous owner's supervisor was running.

      `release-1789392457`, 2026-09-14 13:27:37Z, `--deadline 180s`: completed
      at `proven` on two proofs. The deadline was raised from 120 s because the
      previous owner notices on a sixty-second tick and its restart was measured
      at fifty-seven seconds, which left three seconds of margin.

      What the machine did, read afterwards rather than from the outcome:

          13:27:30Z  HEALTHY -> HANDED_OVER   (its last tick under the claim)
          13:28:40Z  HANDED_OVER -> SINGBOX_EXITED  process_missing
          13:28:47Z  SINGBOX_EXITED -> STARTING  singbox_started
          13:29:16Z  ingress twilight-1 quarantined; STARTING -> FAILOVER
          13:29:25Z  FAILOVER -> HEALTHY  startup_probe_ok

      One sing-box afterwards, pid 75412, uid 0, running the previous owner's
      configuration; this runtime's pid 44094 gone. The tunnel address is on
      utun15, the five inherited destinations route through it, general traffic
      answers and an address behind the tunnel answers 421. The previous owner
      recorded no carrier rebuild.

      The quarantine of the first ingress at startup is not attributed to the
      release: the same ingress was failing with read timeouts in the
      supervisor's log earlier the same day.

      **A statement made before the run was wrong.** It said the supervisor would
      read the newly installed script when it restarted after the release. It
      did not restart as a process: its loop reruns `run_once` inside the same
      bash process on exit code 75, with the functions it loaded at 11:14. The
      proof is in its own record — between 13:07 and 13:27 it moved
      `HANDED_OVER -> HEALTHY` sixteen times, which the installed script cannot
      do while a claim is held. Without a claim the two versions behave the
      same, so nothing about this release or the coming soak depends on it. The
      next handover does: the supervisor has to be restarted as a process first,
      and `check` has to learn to ask whether the running supervisor is older
      than the script installed for it, because reading the installed file is
      reading the wrong thing.

## 5. Close

- [x] 5.1 Sync the delta into the baseline, validate, archive.
