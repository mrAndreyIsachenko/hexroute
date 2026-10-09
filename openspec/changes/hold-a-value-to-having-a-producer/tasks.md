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

- [ ] 2.1 Read the vocabularies and their constants from the code, with no list of vocabularies in the gate; verified by a fixture declaring one the gate has never heard of.

- [ ] 2.2 Count a bare name only inside the declaring package, and the qualified name outside it; verified by a fixture with two packages declaring the same constant name.

- [ ] 2.3 Exclude the declaring file's `const` blocks and validity switches, and nothing else of it; verified by a fixture whose declaring file both validates and returns its values.

- [ ] 2.4 Count a fixture as not a producer; verified by a fixture emitted only from a file named as one.

- [ ] 2.5 Refuse when a whole vocabulary has no emitter anywhere, rather than reporting every value unproduced; verified by a fixture of a vocabulary nothing uses.

      That is what an indirect emitter the textual search cannot see would look
      like at scale, and the design accepts the search on the condition that
      this failure is loud.

- [ ] 2.6 Say how many vocabularies, values, producers and recorded exceptions were held; verified by the line it prints.

## 3. The record

- [ ] 3.1 Write down the fourteen values that have no producer, each with its reason; verified by the gate passing with the list and refusing without it.

- [ ] 3.2 Refuse a value that gains a producer while still on the list; verified by a fixture where one is emitted and listed.

- [ ] 3.3 Refuse a value that leaves the list while still unproduced; verified by removing an entry and expecting the refusal.

- [ ] 3.4 Say in each entry what the value is waiting for, where that is known from the code; verified by the entries naming a collector or a condition rather than restating the value.

## 4. The gate holds

- [ ] 4.1 Prove it refuses each of the three measurement mistakes, not only the current state; verified by three fixtures, one per mistake.

- [ ] 4.2 Prove it fails on the parent commit; verified by its exit status before and after.

- [ ] 4.3 Register it so it runs, and prove `make check` carries it; verified by the gate's own line in the output.

## 5. Mutation discipline

- [ ] 5.1 Mutate the package awareness, the exclusions and both directions of the record; verified by every survivor closed by a test.

      A mutation that does not run is rewritten, not counted. The last two runs
      each found survivors that were paths the proofs did not reach rather than
      defects, so the harness judges by the gate as well as by its own proofs.

## 6. Close

- [ ] 6.1 Run the full gate and report any gate that did not run and why; verified by the gate's exit status, not by reading its output.

- [ ] 6.2 Sync the delta into the baseline, validate and archive, with every task that can be ticked beforehand ticked; verified by the drift gate.

- [ ] 6.3 Record what this leaves open, including the two statements of one fact and the vocabularies outside these sixteen.
