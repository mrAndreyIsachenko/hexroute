# Tasks

## 1. The ground

- [x] 1.1 Audit every value of every remaining check against the section it appears in; verified by the counts per vocabulary.

      Audited 2026-10-09. Every value of all seven vocabularies is explained in
      its own section today, so **nothing is currently undocumented** — the gate
      passes for the right reason by luck rather than by construction.

      Six of the seven have values that also appear elsewhere: all eight
      component names in their payload sections; the component states
      `conflict`, `degraded`, `ready`, `stale`, `unknown`; `unauthorized`; the
      classifications `conflict`, `missing`, `stale`, `unknown`; and the diff
      reason `none`, which appears in Authorization and six payload sections.
      Only `proposal class` is confined to one section.

- [x] 1.2 Demonstrate that a removed explanation goes unnoticed, rather than arguing it from the overlap; verified by the gate's exit status with the row deleted.

      The row explaining the component state `degraded` — "the owner asserted
      partial function" — was deleted from `### Component states` and the gate
      exited **0**. The word survives in the `managed_transports` payload
      section, where it counts degraded transports and says nothing about what a
      degraded component is. The reference was restored and the diff is empty.

      That is the defect, and it is a loss the gate cannot see rather than a
      falsehood it currently tells.

## 2. The reader

- [x] 2.1 Give the payload helper a second entry point: a vocabulary, a section and a document; verified by all seven checks running through it.

      `tests/payload_documentation.py` became
      `tests/reference_documentation.py` with two entry points, `payloads` and
      `vocabulary`. The seven checks are seven calls, and the shell gate holds
      only the seven headings — the values still come from the code.

- [x] 2.2 Refuse when the section is absent, rather than reading the whole document; verified by a fixture asking for a heading the document does not have.

      This record first claimed verification that had not happened. The reader
      refused an absent heading, but nothing exercised that path, and the
      mutation run found it: "widen to the whole document when the heading is
      absent" survived. The gate now carries a fixture for it, and two more the
      harnesses could not reach — an empty vocabulary, and a value explained in
      the *next* section rather than this one.

      Widening is the defect being removed, so an absent section is a refusal
      and not a fallback.

- [x] 2.3 Refuse a value the section explains and the code no longer declares, naming it; verified by a stray row added to each of the four sections the check is given.

      All four refuse `a_value_nothing_declares`.

- [x] 2.4 Apply the reverse check only where a section's rows are its vocabulary, and record which four those are; verified by the measurement that decides it.

      Measured 2026-10-09. Four sections have exactly as many rows as values and
      nothing else: the component names, the component states, the
      classifications and the proposal classes.

      Three do not. `## Authorization` tabulates the authorization **reasons**
      and explains `authorized` and `unauthorized` in prose; `## The diff`
      tabulates the classifications and explains its twelve reasons in prose;
      the declared sources sit in the second column of the component table. For
      those, a row-strict reverse check would demand rewriting the document to
      satisfy a gate rather than a reader, which is a separate tightening.

- [x] 2.5 Say how many sections and values were held; verified by the eight lines it prints.

      `8 values`, `7 values`, `8 values`, `4 values`, `2 values`, `12 values`,
      `9 values`, and the payload line's `8 payloads, 25 fields`.

## 3. The seven checks

- [x] 3.1 Key the component check to `## Who owns what`; verified by the gate refusing when `scoped_routes` is removed from it.

- [x] 3.2 Key the component state check to `### Component states`; verified by the `degraded` deletion from 1.2 now being refused.

- [x] 3.3 Key the authorization check to `## Authorization`, without the reverse direction.

- [x] 3.4 Key the classification and diff reason checks to `## The diff` — the first with the reverse direction, the second without, because that section's table is the classifications.

- [x] 3.5 Key the proposal class check to `## The proposals`, with the reverse direction.

- [x] 3.6 Key the source check to `## Who owns what`; verified by the gate refusing when `root.relays` is removed from it.

      The sources are declared as a table in the code rather than as a typed
      constant block, so the reader takes `@sources` as the name of that shape.
      One reader, two shapes, named at the call site.

- [x] 3.7 Record any value that turns out to be explained outside its own section, and move its explanation rather than widening the check; verified by the gate passing without a widened check.

      None had to move. Every value of all seven vocabularies was already
      explained in its own section — which the audit in 1.1 established before
      any code was written, and which is why this change touches the gate and
      not the document.

## 4. The gate holds

- [x] 4.1 Prove each of the seven refuses a removed explanation; verified by one deletion per vocabulary, each expected to fail.

      Seven deletions, each applied inside its own section only, the reference
      restored and compared byte for byte between them: `scoped_routes`,
      `degraded`, `missing`, `observe`, `unauthorized`, `stale_observation`,
      `root.relays`. All seven refused.

      The harness refuses its own fixture too: if the value it means to remove
      is not mentioned in that section, it reports that the fixture proves
      nothing rather than counting a pass.

      Seven deletions, not one. The `degraded` case is the one already
      demonstrated; the other six are the ones this change is for.

- [x] 4.4 Prove the reverse check on the four row-aligned vocabularies; verified by one undeclared row per section, each expected to fail.

      All four refuse. And the three it is withheld from are required to
      **accept** the same row, so the withholding is visibly a decision rather
      than an oversight — a check that refused there would be refusing the
      document's own shape.

- [x] 4.2 Prove it fails on the parent commit; verified by its exit status before and after.

      The before is 1.2: with the parent's flat check, deleting the row that
      explains the component state `degraded` left the gate exiting **0**. With
      this change the same deletion is refused. The gate now sees a loss it
      could not see.

- [x] 4.3 Prove a fully explained reference still passes, so the refusals are about what is missing; verified by all eight checks passing on the document as it stands and by `make check` returning 0.

## 5. Mutation discipline

- [x] 5.1 Mutate the section bounding and both refusals; verified by every survivor closed by a test or recorded with the reason it was left.

      Seven mutations over the reader: widening when the heading is absent,
      reading past the next heading, starting before the heading, never
      reporting a missing value, never reporting a stray row, applying the
      reverse check where it is withheld, and passing an empty vocabulary.

      First run: **4 killed, 3 survived** — and all three survived because my
      own proofs did not reach them, not because the reader was right. Two were
      paths nothing exercised, and the third I had mis-targeted: the string
      `start += len(heading)` appears in the payload reader as well, and the
      replacement hit that one, which the two harnesses do not run.

      So the gate gained fixtures for the unreached paths and the harness now
      judges by the gate as well as by its own two proofs. Second run:
      **7 killed, 0 survived.**

      The run's real finding is in 2.2: a task of mine claimed a verification
      that had not been performed, and the mutation is what caught it.

## 6. Close

- [x] 6.1 Run the full gate and report any gate that did not run and why; verified by the gate's exit status, not by reading its output.

      `make check` returned 0, with all seven vocabulary lines and the payload
      line passing.

      Four gates did not run and none is applicable to this diff, which touches
      one gate, one reader and this change: `postgres-test`, `container-build`
      and `container-test`, `terraform-test` and `terraform-state-test`, and the
      `policy-qualification` commands — nothing was installed and no runtime
      restarted. Docker and the Terraform CLI are both present, so the first
      three were skipped for scope rather than availability.

- [x] 6.2 Confirm the rename reached the baseline as a rename and not as a second requirement; verified by reading the baseline after archiving.

      It did. The old name is gone from the baseline, the new one appears once,
      the capability holds 13 requirements rather than 14, and the renamed
      requirement carries nine scenarios — the four the previous change wrote
      and the five this one added.

      The delta renames the requirement added last change and replaces its
      content. A `MODIFIED` block replaces the whole requirement, and the
      validator refused a first attempt that silently dropped three scenarios
      the current spec held, naming each: "a MODIFIED requirement replaces the
      whole block, so archive refuses to drop them". That refusal is why this
      task existed, and it did the work the task was written to double-check.

      This tick and 6.3's are written after the archive, because both describe
      the act of archiving and cannot be true before it. That is different from
      the previous change, which archived with nine tasks simply unticked.

- [x] 6.3 Sync the delta into the baseline, validate and archive, with every task above ticked first; verified by the drift gate.

      One requirement renamed and replaced in
      `observable-connectivity-state-machine`. Every task but 6.2 was ticked
      before archiving, and 6.2 could not be: it reads the result of the
      archive.

- [x] 6.4 Record what this leaves open, including the ten vocabularies with no check at all.

      **Ten published vocabularies have no check at all.** Found by this
      change's audit and larger than what it fixes: sixteen string vocabularies
      are published by these packages and the gate holds six. `Reason` (12
      values), `Lifecycle` (5), `LinkClass` (5), `AuthorizationReason` (5),
      `AggregateState` (4), `ExpiryClass` (4), `ResolverClass` (4), `PathClass`
      (3), `SelectedClass` (3) and `ProfileClass` (2) are unheld — 47 values,
      six of them mentioned in prose in the payload sections and held by
      nothing. Several have no owning section yet: nothing in the reference is
      about where a `Lifecycle` or a `Reason` is explained, so that change
      decides document structure as well as adding checks.

      **Three sections cannot take the reverse check as they stand.**
      `## Authorization` tabulates the authorization reasons, `## The diff`
      tabulates the classifications, and the declared sources sit in a second
      column. Giving those three the reverse direction means restructuring the
      document, which is a tightening with its own justification and not this
      one.

      **A vocabulary is not held to having a producer.** `link_class` declares
      three values nothing emits, `expiry_class` two,
      `managed_transports.degraded` is never set. The reference says so and
      nothing holds it true. The same shape as the unwired-package list this
      repository keeps.

      **`### A component row`'s fields are not held either.** They are a
      vocabulary in the same sense — the fields of a published record — and the
      payload reader already holds the payloads' own.

      **Carried, untouched:** `readmodel/checkpoints` bounded by nothing;
      HEX-19; HEX-11; no path from a host event to an alert; nine of the ten
      causes a root cycle can name unseen on this machine; and the two ingress
      routes on each other's links, which needs a decision rather than a
      change.
