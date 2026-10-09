#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# The reference describes a vocabulary the code owns. A document that falls
# behind it is worse than no document: it reads as current and is wrong, and
# the reader has no way to tell which half they are looking at.
#
# So every value an operator can be shown has to be named where it is
# explained. Adding a component, a state, a classification or a proposal class
# without documenting it fails here.

reference=docs/connectivity-read-model-reference.md
overview=docs/connectivity-read-model.md

for document in "$reference" "$overview"; do
  [ -f "$document" ] || {
    printf '%s is missing\n' "$document" >&2
    exit 1
  }
done

status=0

# Every published vocabulary is held to the section that owns it, not to the
# document as a whole.
#
# A check for a backtick-quoted word anywhere in the reference cannot notice an
# explanation being removed, because these vocabularies overlap: `degraded` is a
# component state and a transport count, `missing` is a scoped-routes quantity
# and a diff classification, `none` is a diff reason and a value of six payload
# classes. Measured 2026-10-09: the row explaining the component state
# `degraded` was deleted from its own section and this gate exited 0.
#
# --rows adds the reverse direction, that every row in the section is a value of
# the vocabulary. It is given to the four sections whose rows are their
# vocabulary and withheld from the three whose tables belong to another one:
# `## Authorization` tabulates the authorization reasons, `## The diff`
# tabulates the classifications, and the declared sources sit in the second
# column of the component table.
vocabulary() {
  if ! python3 tests/reference_documentation.py vocabulary "$@" "$reference"; then
    status=1
  fi
}

vocabulary "component" "## Who owns what" \
  internal/connectivity/model.go Component --rows
vocabulary "component state" "### Component states" \
  internal/connectivityreduce/snapshot.go ComponentState --rows
vocabulary "diff classification" "## The diff" \
  internal/connectivityreduce/diff.go Classification --rows
vocabulary "proposal class" "## The proposals" \
  internal/connectivityreduce/proposal.go ProposalClass --rows
vocabulary "authorization value" "## Authorization" \
  internal/connectivityreduce/snapshot.go Authorization
vocabulary "diff reason" "## The diff" \
  internal/connectivityreduce/diff.go DiffReason
# A new collector cannot be introduced without saying which component it speaks
# for: the sources are the first column of the table that pairs them.
vocabulary "declared source" "## Who owns what" \
  internal/safety/connectivity.go @sources

# The rollout names arguments an operator will type. They have to be the ones
# the installed jobs actually take.
for argument in --connectivity-read-model --publish-connectivity-to \
  --connectivity-qualification --connectivity-qualification-session; do
  grep -qF -- "$argument" "$overview" || {
      printf '%s is not named in the rollout in %s\n' "$argument" "$overview" >&2
      status=1
    }
  grep -rqF -- "$argument" deploy/macos scripts/macos || {
    printf '%s is documented and no installed job takes it\n' "$argument" >&2
    status=1
  }
done

# Every quantity a component payload carries is explained under that component.
# Not by the require() above: its match is a backtick-quoted word anywhere in
# the document, and these vocabularies overlap, so it passes for the wrong
# reason. Measured 2026-10-09: it called the managed_transports payload fully
# explained because `configured` matched a row about routes and `ready` and
# `degraded` matched rows about component states.
if ! python3 tests/reference_documentation.py payloads \
  internal/connectivity/payload.go "$reference"; then
  status=1
fi

# The vocabulary reader is held to refusing too, on the two paths the proof
# harnesses for this change could not reach: an absent section and a vocabulary
# with no values. A mutation run found both unheld, and the task that claimed
# otherwise was corrected.
vocabulary_fixture="$(mktemp -d "${TMPDIR:-/tmp}/hexroute-vocabulary-gate.XXXXXX")"
trap 'rm -rf "$payload_fixture" "$vocabulary_fixture"' EXIT

cat >"$vocabulary_fixture/source.go" <<'SOURCE'
package example

type Thing string

const (
	ThingFirst  Thing = "first"
	ThingSecond Thing = "second"
)

type Empty string
SOURCE

cat >"$vocabulary_fixture/reference.md" <<'DOC'
## The things

| Value | What it says |
| --- | --- |
| `first` | the first one |
| `second` | the second one |

## Something else
DOC

reader() {
  python3 tests/reference_documentation.py vocabulary "$@" >/dev/null 2>&1
}

if ! reader thing "## The things" "$vocabulary_fixture/source.go" Thing \
  "$vocabulary_fixture/reference.md" --rows; then
  printf 'the vocabulary reader refused a fully explained fixture\n' >&2
  status=1
fi

# An absent section is a refusal, not a search of the whole document: widening
# is the defect this change removed.
if reader thing "## A section that is not there" "$vocabulary_fixture/source.go" \
  Thing "$vocabulary_fixture/reference.md"; then
  printf 'the vocabulary reader widened to the document when its section was absent\n' >&2
  status=1
fi

# A vocabulary it read nothing from is a broken query, not an empty answer.
if reader empty "## The things" "$vocabulary_fixture/source.go" Empty \
  "$vocabulary_fixture/reference.md"; then
  printf 'the vocabulary reader passed with no values to check\n' >&2
  status=1
fi

# The section ends at the next heading: a value explained after it does not
# count as explained in it.
cat >"$vocabulary_fixture/leaked.md" <<'DOC'
## The things

| Value | What it says |
| --- | --- |
| `first` | the first one |

## Something else

| Value | What it says |
| --- | --- |
| `second` | explained in the wrong place |
DOC
if reader thing "## The things" "$vocabulary_fixture/source.go" Thing \
  "$vocabulary_fixture/leaked.md"; then
  printf 'the vocabulary reader accepted an explanation from the next section\n' >&2
  status=1
fi

# And the payload gate is held to refusing, because a gate that only ever
# passes is indistinguishable from one that checks nothing.
payload_fixture="$(mktemp -d "${TMPDIR:-/tmp}/hexroute-payload-gate.XXXXXX")"

cat >"$payload_fixture/payload.go" <<'SOURCE'
package connectivity

type Payload struct {
	Example *ExamplePayload `json:"example,omitempty"`
}

type ExamplePayload struct {
	Counted uint16 `json:"counted"`
	Other   uint16 `json:"other"`
}
SOURCE

refuses() {
  local what="$1" reference="$2"
  if python3 tests/reference_documentation.py payloads "$payload_fixture/payload.go" \
    "$reference" >/dev/null 2>&1; then
    printf 'the payload gate accepted %s\n' "$what" >&2
    status=1
  fi
}

# A row under another component does not explain this one.
cat >"$payload_fixture/elsewhere.md" <<'DOC'
### The `other_thing` payload

| Field | What it says |
| --- | --- |
| `counted` | something else entirely |
| `other` | also something else |
DOC
refuses "an explanation under another component" "$payload_fixture/elsewhere.md"

# A field with no row at all.
cat >"$payload_fixture/partial.md" <<'DOC'
### The `example` payload

| Field | What it says |
| --- | --- |
| `counted` | what it counts |
DOC
refuses "a payload with one field unexplained" "$payload_fixture/partial.md"

# A row for a quantity nothing reports.
cat >"$payload_fixture/stale.md" <<'DOC'
### The `example` payload

| Field | What it says |
| --- | --- |
| `counted` | what it counts |
| `other` | the other one |
| `removed` | a quantity nothing carries any more |
DOC
refuses "an explanation no field carries" "$payload_fixture/stale.md"

# And it refuses to pass when there is nothing to check.
printf 'package connectivity\n' >"$payload_fixture/empty.go"
if python3 tests/reference_documentation.py payloads "$payload_fixture/empty.go" \
  "$reference" >/dev/null 2>&1; then
  printf 'the payload gate passed with no payloads to check\n' >&2
  status=1
fi

# The whole fixture, explained, is accepted — so the refusals above are about
# what is missing rather than about the gate refusing everything.
cat >"$payload_fixture/whole.md" <<'DOC'
### The `example` payload

| Field | What it says |
| --- | --- |
| `counted` | what it counts |
| `other` | the other one |
DOC
if ! python3 tests/reference_documentation.py payloads "$payload_fixture/payload.go" \
  "$payload_fixture/whole.md" >/dev/null 2>&1; then
  printf 'the payload gate refused a fully explained payload\n' >&2
  status=1
fi

# Every architectural reference stays pinned to the commit that was reviewed.
# An unpinned reference is a claim nobody can check once the project moves on,
# and each of these was read out of code that has since moved.
references=docs/architecture/connectivity-references.md
[ -f "$references" ] || {
  printf '%s is missing\n' "$references" >&2
  exit 1
}
for project in firezone/firezone netbirdio/netbird \
  microsoft/agent-framework-go Layr-Labs/chain-indexer; do
  grep -qF "$project" "$references" || {
    printf '%s is not among the documented architectural references\n' \
      "$project" >&2
    status=1
    continue
  }
  # A link to the project without a commit is not a pin.
  grep -F "$project" "$references" | grep -E '/(blob|tree)/[0-9a-f]{40}' >/dev/null || {
    printf '%s is referenced without pinning the reviewed commit\n' \
      "$project" >&2
    status=1
  }
done

# The closeout runbook is read once, at the end of a three-day soak, by
# someone who cannot afford a command that turned out not to exist. Its
# subcommands and flags are checked like any other documented value.
closeout=docs/macos/connectivity-qualification-closeout.md
[ -f "$closeout" ] || {
  printf '%s is missing\n' "$closeout" >&2
  status=1
}
if [ -f "$closeout" ]; then
  for subcommand in install status; do
    grep -qE "^  $subcommand\)" scripts/macos/observe-root-launchd.sh || {
      printf 'the closeout names `%s` and the root installer has no such subcommand\n' \
        "$subcommand" >&2
      status=1
    }
  done
  for subcommand in attempts reports; do
    grep -qE "^  $subcommand\)" scripts/macos/archive-review-launchd.sh || {
      printf 'the closeout names `%s` and the review installer has no such subcommand\n' \
        "$subcommand" >&2
      status=1
    }
  done
  for flag in --qualification --session --state --store; do
    grep -qF -- "$flag" "$closeout" || continue
    grep -qF -- "\"${flag#--}\"" cmd/hexroute-connectivity-replay/main.go || {
      printf 'the closeout uses %s and hexroute-connectivity-replay has no such flag\n' \
        "$flag" >&2
      status=1
    }
  done
  grep -qF -- '--connectivity-event-archive' "$closeout" || {
    printf 'the closeout does not say what the reinstall turns on\n' >&2
    status=1
  }
fi

# The commands the documents tell an operator to run have to exist.# The commands the documents tell an operator to run have to exist.
for binary in hexroute-connectivity-replay hexroute-connectivity-watch; do
  [ -d "cmd/$binary" ] || {
    printf '%s is documented and is not a command in this repository\n' \
      "$binary" >&2
    status=1
  }
done

[ "$status" -eq 0 ] || exit 1

printf 'ok: the connectivity read model documents every value it can report\n'
