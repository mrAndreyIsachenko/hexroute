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

# named collects the quoted values of a typed string constant block.
named() {
  local file="$1" suffix="$2"
  grep -oE "$suffix = \"[a-z_]+\"" "$file" \
    | sed -E 's/.* = "([a-z_]+)"/\1/' | sort -u
}

require() {
  local value="$1" document="$2" what="$3"
  grep -qF "\`$value\`" "$document" || {
    printf '%s %s is not explained in %s\n' "$what" "$value" "$document" >&2
    status=1
  }
}

while read -r component; do
  [ -n "$component" ] || continue
  require "$component" "$reference" "component"
done <<<"$(named internal/connectivity/model.go 'Component')"

while read -r state; do
  [ -n "$state" ] || continue
  require "$state" "$reference" "component state"
done <<<"$(named internal/connectivityreduce/snapshot.go 'ComponentState')"

while read -r value; do
  [ -n "$value" ] || continue
  require "$value" "$reference" "aggregate or authorization value"
done <<<"$(named internal/connectivityreduce/snapshot.go 'Authorization')"

while read -r class; do
  [ -n "$class" ] || continue
  require "$class" "$reference" "diff classification"
done <<<"$(named internal/connectivityreduce/diff.go 'Classification')"

while read -r reason; do
  [ -n "$reason" ] || continue
  require "$reason" "$reference" "diff reason"
done <<<"$(named internal/connectivityreduce/diff.go 'DiffReason')"

while read -r class; do
  [ -n "$class" ] || continue
  require "$class" "$reference" "proposal class"
done <<<"$(named internal/connectivityreduce/proposal.go 'ProposalClass')"

# Every declared source is named, so a new collector cannot be introduced
# without saying which component it speaks for.
while read -r source; do
  [ -n "$source" ] || continue
  require "$source" "$reference" "source"
done <<<"$(grep -oE '\{"[a-z]+\.[a-z]+"' internal/safety/connectivity.go \
  | sed -E 's/\{"([a-z.]+)"/\1/' | sort -u)"

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
  grep -F "$project" "$references" | grep -qE '/(blob|tree)/[0-9a-f]{40}' || {
    printf '%s is referenced without pinning the reviewed commit\n' \
      "$project" >&2
    status=1
  }
done

# The commands the documents tell an operator to run have to exist.
for binary in hexroute-connectivity-replay hexroute-connectivity-watch; do
  [ -d "cmd/$binary" ] || {
    printf '%s is documented and is not a command in this repository\n' \
      "$binary" >&2
    status=1
  }
done

[ "$status" -eq 0 ] || exit 1

printf 'ok: the connectivity read model documents every value it can report\n'
