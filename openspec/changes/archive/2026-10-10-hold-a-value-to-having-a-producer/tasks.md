# Tasks

## 1. The ground

- [x] 1.1 Count how many published values have a producer, and get the count right; verified by hand checks of individual values against each attempt.

      Measured 2026-10-09: **88 values, 74 produced, 2 emitted only by a
      fixture, 12 by nothing.** Fourteen of the eighty-eight are absent from the
      running system.

      Four attempts, each wrong answer caught by checking one value by hand:

      1. Counting the declaring file whole: **88 of 88 produced.** Every
         vocabulary's `Valid()` switch names all of its constants.
      2. Excluding the declaring file whole: `DiffReason` 11 of 12 unproduced.
         `diff.go` declares those values and returns them.
      3. Cutting the declaration and the validity switch: `expired` produced.
      4. It is not. The match was `internal/policy`'s own `ReasonExpired`, a
         different constant with the same bare name, and nothing references
         `connectivity.ReasonExpired`.

      The four are the reason this change exists rather than a note about
      method: a question that answered wrongly four times to someone reading
      closely is not one a reader of the reference will re-derive.

- [x] 1.2 Record which vocabularies are fully produced, so the list is sized by the reading; verified by the per-vocabulary report.

      Twelve of sixteen are fully produced. The four that are not: `Reason` (6
      of 12), `LinkClass` (3 of 5), `ResolverClass` (3 of 4), `ExpiryClass` (2
      of 4).

## 2. The reading

- [x] 2.1 Read the vocabularies and their constants from the code, with no list of vocabularies in the gate; verified by a fixture declaring one the gate has never heard of.

      `tests/value_producers.py` takes two sets of directories — which to hold
      and where to search — and discovers the vocabularies under the first. No
      list of vocabularies, values or files. Its fixture declares a `Shade`
      vocabulary the gate has never seen and is held to it.

- [x] 2.2 Count a bare name only inside the declaring package, and the qualified name outside it; verified by a fixture with two packages declaring the same constant name.

      The fixture's second package declares its own `ShadeThird` and emits it,
      so the held vocabulary's `third` has no producer. That is the mistake
      which made `expired` look produced: the match was `internal/policy`'s own
      `ReasonExpired`.

- [x] 2.3 Exclude the declaring file's `const` blocks and validity switches, and nothing else of it; verified by a fixture whose declaring file both validates and returns its values.

      The fixture's declaring file has a `valid()` switch naming all three
      values and a function returning one of them. Reading it as written makes
      everything produced; skipping it loses the one it returns. Both are
      mutations and both die.

- [x] 2.4 Count a fixture as not a producer; verified by a fixture whose only emitter is in a file named as one, with the declaring file's own emitter taken away.

- [x] 2.5 Refuse when a whole vocabulary has no emitter anywhere, rather than reporting every value unproduced; verified by a fixture of a vocabulary nothing uses.

      That is what an indirect emitter the textual search cannot see would look
      like at scale, and the design accepts the search on the condition that
      this failure is loud. The fixture writes both values down and is still
      refused, because a vocabulary nothing emits is a broken read rather than
      an unused vocabulary.

- [x] 2.6 Say how many vocabularies, values, producers and recorded exceptions were held; verified by the line it prints: `16 vocabularies, 88 values, 74 produced, 14 written down`.

## 3. The record

- [x] 3.1 Write down the fourteen values that have no producer, each with its reason; verified by the gate passing with the list and refusing without it.

      Fourteen entries in `UNPRODUCED`, grouped by why: the rebaseline reasons
      the collectors do not carry, the expiry the session collector reports as a
      class instead, a policy application no collector observes, the link kinds
      the observer cannot tell apart, and the DNS classes with no collector at
      all.

- [x] 3.2 Refuse a value that gains a producer while still on the list; verified by a fixture where one is emitted and listed.

      The first version of this fixture refused for the **other** reason — it
      left a second value unwritten — so it would have passed with the check
      removed. A mutation found it. The fixture now writes that second value
      down, leaving the stale entry as the only thing to refuse.

- [x] 3.3 Refuse a value that leaves the list while still unproduced; verified by an empty list over the same fixture, and by a list naming a value no vocabulary declares.

- [x] 3.4 Say in each entry what the value is waiting for, where that is known from the code; verified by the entries naming a collector or a condition rather than restating the value.

      Each says what stands in its place: `rebaseline_required` on a component
      row instead of a reason, `expiry_class` instead of an expiry reason,
      `wired` for any link that is up because the observer cannot tell kinds
      apart, and for the DNS classes that no collector exists and a test asserts
      none can emit one.

## 4. The gate holds

- [x] 4.1 Prove it refuses each of the three measurement mistakes, not only the current state; verified by the mutation run rather than by three separate fixtures.

      The three mistakes are mutations — reading the declaring file as written,
      skipping it whole, and counting a bare name everywhere — and all three
      die against the fixtures. A fixture per mistake would have restated what
      the mutation already demonstrates.

- [x] 4.2 Prove it fails on the parent commit; verified by running it with nothing written down.

      The gate did not exist on the parent commit, so the proof runs the other
      way: with an empty list it exits **1** and names all fourteen —
      `ExpiryClass.expiring`, `ExpiryClass.expired`, the three `LinkClass`
      kinds, the six `Reason` values and the three `ResolverClass` classes. That
      is the state the repository was in before this change, and nothing said
      so.

- [x] 4.3 Register it so it runs, and prove `make check` carries it; verified by `tests/value_producer_test.sh` in the Makefile's shell-test list and by its two `ok:` lines in the output.

## 5. Mutation discipline

- [x] 5.1 Mutate the package awareness, the exclusions and both directions of the record; verified by every survivor closed by a test.

      Ten mutations: both ways of getting the package awareness wrong, three
      ways of mishandling the declaring file, counting a fixture, and all four
      refusals.

      First run: **9 killed, 1 survived** — and the survivor was a fixture of
      mine refusing for the wrong reason, not a hole in the gate. It wrote down
      one value that was emitted while leaving another unwritten, so the refusal
      came from the second and the fixture would have passed with the first
      check removed. Second run after isolating it: **10 killed, 0 survived.**

      That is the third run this week whose finding was about my own proofs
      rather than the code.

## 6. Close

- [x] 6.1 Run the full gate and report any gate that did not run and why; verified by the gate's exit status, not by reading its output.

      `make check` returned 0, and `tests/value_producer_test.sh` is in the
      Makefile's shell-test list so it runs there.

      Four gates did not run and none is applicable to this diff, which adds one
      gate and its helper: `postgres-test`, the two container gates, the two
      terraform gates, and the `policy-qualification` commands — nothing was
      installed and no runtime restarted. Docker and the Terraform CLI are both
      present, so the first three were skipped for scope rather than
      availability.

- [x] 6.2 Sync the delta into the baseline, validate and archive, with every task that can be ticked beforehand ticked; verified by the drift gate.

      One requirement replaced in `observable-connectivity-state-machine`,
      carrying all eleven scenarios it had plus four for the producer rule.
      Every other task was ticked before archiving; this one describes the
      archive.

- [x] 6.3 Record what this leaves open, including the two statements of one fact and the vocabularies outside these sixteen.

      **Two statements of one fact.** The reference says in prose which values
      nothing emits; `UNPRODUCED` says it in a list. Nothing holds the two in
      step, so one can drift from the other. Closing it means either making the
      prose machine-readable — writing the document for the gate — or deriving
      the prose from the list, which is a generated document. Neither is
      obviously right, which is why it is recorded rather than chosen.

      **Vocabularies outside these sixteen.** The repository publishes typed
      string vocabularies elsewhere — the control machine's reasons and states,
      the planner's operation kinds and reasons, the policy reasons, the tunnel
      executor's blocks — and the argument for holding them is the same. The
      gate takes the directories to hold as arguments, so extending it is a
      line in the Makefile plus whatever the scan turns up. What it will turn up
      is unknown: this reading found 14 of 88 unproduced in one area.

      **Whether an unproduced value should be removed.** Four of the `Reason`
      values describe a rebaseline and an expiry the collectors do not report,
      and two `ResolverClass` values wait on a DNS collector that does not
      exist. Deleting them is a judgement about what those collectors will do.

      **A textual search can miss an indirect emitter.** The gate refuses when a
      whole vocabulary has none, which is what that looks like at scale, but a
      single value emitted through a variable would read as unproduced. Nothing
      in these vocabularies is used that way today.

      **Carried, untouched:** `readmodel/checkpoints` bounded by nothing;
      HEX-19; HEX-11; no path from a host event to an alert; nine of the ten
      causes a root cycle can name unseen on this machine; and the two ingress
      routes on each other's links.
