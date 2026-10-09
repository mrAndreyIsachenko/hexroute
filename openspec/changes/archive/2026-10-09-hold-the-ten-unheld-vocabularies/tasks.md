# Tasks

## 1. The ground

- [x] 1.1 Read where each of the ten is explained today, so the work is sized by what each needs rather than by their number; verified by the table in the proposal.

      Measured 2026-10-09. Seven are already explained in the place that owns
      them and about the thing they are: `AuthorizationReason` as its own rows
      in `## Authorization`, and six class vocabularies inside their payload
      field's own rows. They need a check and nothing else.

      Three do not. `Reason` has no place and ten of its twelve values appear
      nowhere in the document. `Lifecycle` and `AggregateState` have no place of
      their own.

- [x] 1.2 Establish that two of them would pass a section-keyed check for the wrong reason, rather than assuming the previous change's pattern covers them; verified by the value sets.

      `Lifecycle` (5 values) and `AggregateState` (4) are **strict subsets** of
      `ComponentState` (7), and the rows of `### Component states` are exactly
      `ComponentState`'s seven. A check keyed to that section finds every
      lifecycle and aggregate value and is satisfied by rows about the derived
      state — a third thing.

      So the pattern that fixed the last two changes does not reach this case,
      and finding that out before writing the checks is what this task was for.

## 2. Three vocabularies, three tables

- [x] 2.1 Say in one sentence, before the tables, that three vocabularies share these words and what distinguishes them; verified by the sentence naming all three roles, and by saying which word a reader is most likely to get wrong: `degraded` is three claims.

- [x] 2.2 Give what a collector asserted its own table — `Lifecycle`, five values, read from the mappers that emit each; verified by the gate keyed to its subheading.

      Five rows, five values, nothing else. `degraded` names the three shapes a
      collector actually reports it for: a link up with no gateway, routes some
      of which are misplaced, relays some of which answer. `unknown` says both
      halves of what it covers — the probe answered badly, or could not be
      asked.

- [x] 2.3 Give the derived state its own table — `ComponentState`, seven values; verified by the gate. The rows are the ones that were there, moved under their own subheading.

- [x] 2.4 Give the summary its own table — `AggregateState`, four values; verified by the gate.

      Reading the derivation turned up what the table most needed to say, and it
      corrects a note of mine: **the aggregate is degraded by the integrity of
      the streams on its own** — an open gap, an evicted gap, a source owing a
      baseline, a source with conflicts — so the host can read `degraded` with
      every component `ready`.

      The previous change recorded that the watcher "reports `degraded` on this
      component alone". With `open_gaps: 2` on 2026-10-08 the integrity clause
      was degrading it too, and the new table says so.

- [x] 2.5 Answer whether `stale` and `conflict` belong in the collector's table as values it never asserts; verified by the decision being written down either way.

      They do not. A table about `Lifecycle` holds `Lifecycle`'s five values and
      nothing else, because a word in a table about a vocabulary that does not
      contain it is the confusion this change exists to remove. The comparison
      is made in a sentence under the table instead: a collector never asserts
      `stale` or `conflict`, and those two belong to the model.

      This also keeps the three tables exactly their vocabularies — 5, 7 and 4
      with nothing missing and nothing extra — which is what let the reverse
      check be given to all three.

## 3. The reasons a collector gives

- [x] 3.1 Add a section after `### A component row`, where the `reason` field is already named; verified by the gate finding it.

- [x] 3.2 Explain all twelve values, each read from the mapper that emits it rather than paraphrased from its name; verified by each row naming what emits it.

      Twelve rows for twelve values. Each says what emits it, and the reading
      found something the names hide, which 3.3 records.

- [x] 3.3 Record any value whose mapper says something other than its name suggests; verified by the rows and the paragraph under them.

      **Five of the twelve are emitted by nothing, and a sixth only by a
      fixture.** `policy_applied`, `wake_rebaseline`, `boot_rebaseline`,
      `expiry_approaching` and `expired` have no emitter in the running system;
      `baseline` is emitted only by `internal/connectivity/fixture.go`.

      A wake or a boot does set `rebaseline_required` on a component row, but no
      fact carries `wake_rebaseline` or `boot_rebaseline` as its reason, and
      `session_expiry` reports an `expiry_class` rather than an
      `expiry_approaching` reason. The vocabulary was fixed before the
      collectors that would use it.

      One near-miss worth recording: a first search for `baseline`'s emitter
      matched `ReasonBaselineAccepted` in `internal/connectivityaccept` — a
      different package's constant with a similar name. Searching by the
      qualified name instead showed the real answer. The section also says
      plainly that nothing holds any of this true.

## 4. The gate

- [x] 4.1 Hold the subset case; verified by performing the merge and reading the refusal, which needed no new gate.

      The design said the reader would gain a subset check. It already refuses:
      with each of the three keyed to its own subheading holding exactly its own
      values, the **reverse check that was already there** is the subset gate.
      Merging the derived state's `stale` and `conflict` rows back into the
      collector's table makes them rows that are not values of `Lifecycle`, and
      the check named both and refused.

      So the reader gained nothing, and the alternative the design rejected —
      "rely on the subheading keying alone" — was rejected for a reason that
      does not hold: a subheading renamed or merged away makes its check
      refuse, which does not depend on anyone remembering.

- [x] 4.2 Key a check to a subheading as well as a section; verified by the three tables being held separately.

      And it exposed a defect in the reader written last change: it bounded a
      section at the next `## ` or `### ` and knew nothing of `#### `, so the
      collector's subsection swallowed the derived table and the check failed on
      a correct document. The payload reader had the same hole. Both now end a
      section at the next heading of any level.

- [x] 4.3 Add the seven checks for the vocabularies already explained; verified by each refusing when its value's explanation is removed. Six are given the forward direction only, because their values live inside their payload field's own rows; `AuthorizationReason` has its own rows and is given both.

- [x] 4.4 Add the three checks for the vocabularies gaining a place, all three with the reverse direction, because each table is exactly its vocabulary.

- [x] 4.5 Say how many vocabularies and values the gate holds in total; verified by the nineteen `ok:` lines the gate prints — sixteen vocabularies, the payload line, and the two it already had.

- [x] 4.6 Prove every one of the ten refuses a removed explanation, and that the six already held still do; verified by one deletion per vocabulary, sixteen in all.

      Sixteen deletions, each inside its own section, the reference restored and
      compared byte for byte between them. All sixteen refused.

      The harness parses the table of checks **out of the gate itself**, so the
      proof cannot drift from what the gate runs, and it refuses its own fixture
      when the value it means to remove is not in that section.

- [x] 4.7 Prove it fails on the parent commit; verified by its exit status before and after.

      On the parent commit the ten checks do not exist, so the proof is the
      other direction: the scan in 6.1 reads the code and finds sixteen
      published vocabularies with sixteen held. Before this change it found six
      held and ten unheld, which is the measurement the change was proposed
      from.

## 5. Mutation discipline

- [x] 5.1 Mutate the subheading keying and the section bounding; verified by every survivor closed by a test.

      Five mutations over the two readers' section bounds: a subsection
      swallowing the next, bounding dropped at the section level, and reading to
      the end of the document, in the vocabulary reader and the payload reader.

      First run: **3 killed, 2 survived** — both survivors because the shapes
      they break do not occur in the reference today. Nothing divides a `## `
      section with a `### `, and no payload section is followed by a `#### `, so
      those bounds were correct and unexercised.

      Closed with fixtures rather than recorded, because the document's shape is
      not the reader's contract. Writing them, the gate caught my own assertion
      backwards: with correct bounds the stray row is **outside** the section,
      so the reader must pass and refusing is the defect. Second run:
      **5 killed, 0 survived.**

## 6. Close

- [x] 6.1 Confirm all sixteen published vocabularies are held, by reading the code rather than counting the gate's calls; verified by the scan that found the ten.

      `published vocabularies: 16   held by the gate: 16   unheld: none`. The
      scan reads the typed constant blocks from the five packages and compares
      them against the suffixes the gate names, so a vocabulary added to the
      code and not to the gate appears here as unheld.

- [x] 6.2 Run the full gate and report any gate that did not run and why; verified by the gate's exit status, not by reading its output.

      `make check` returned 0, with nineteen `ok:` lines from the documentation
      gate.

      Four gates did not run and none is applicable to this diff, which touches
      one reference document, one gate and one reader: `postgres-test`, the two
      container gates, the two terraform gates, and the `policy-qualification`
      commands — nothing was installed and no runtime restarted. Docker and the
      Terraform CLI are both present, so the first three were skipped for scope
      rather than availability.

- [x] 6.3 Sync the delta into the baseline, validate and archive, with every task that can be ticked beforehand ticked; verified by the drift gate.

      One requirement replaced in `observable-connectivity-state-machine`,
      carrying all nine scenarios it already had plus two for the subset case.
      Every other task was ticked before archiving; this one describes the
      archive and is ticked after it.

- [x] 6.4 Record what this leaves open.

      **Nothing holds a value to having a producer, and this change found how
      wide that is.** Five of the twelve reasons are emitted by nothing and a
      sixth only by a fixture; `link_class` declares three values nothing emits
      and `expiry_class` two; `managed_transports.degraded` is never set. The
      reference now says so for each, and no gate would notice a value becoming
      reachable or staying unreachable after its collector arrives. It is the
      same shape as the unwired-package list this repository already keeps, and
      it is now the largest unheld thing in this area.

      **Three sections still cannot take the reverse check.**
      `## Authorization` tabulates the authorization reasons while its own two
      values are prose, `## The diff` tabulates the classifications while its
      twelve reasons are prose, and the declared sources sit in a second column.
      Giving those the reverse direction means restructuring the document.

      **`### A component row`'s fields are still unheld.** They are a vocabulary
      in the same sense, and the payload reader already holds the payloads' own.

      **Carried, untouched:** `readmodel/checkpoints` bounded by nothing;
      HEX-19; HEX-11; no path from a host event to an alert; nine of the ten
      causes a root cycle can name unseen on this machine; and the two ingress
      routes on each other's links.
