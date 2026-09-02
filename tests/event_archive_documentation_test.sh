#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# A document that falls behind the code reads as current and is wrong, and the
# reader has no way to tell which half they are looking at. So every value an
# operator can be shown is checked to be named where it is explained.

document=docs/local-event-archive.md
[ -f "$document" ] || {
  printf '%s is missing\n' "$document" >&2
  exit 1
}

status=0

require() {
  local value="$1" what="$2"
  grep -qF "\`$value\`" "$document" || {
    printf '%s %s is not explained in %s\n' "$what" "$value" "$document" >&2
    status=1
  }
}

# Every overflow reason and commentary outcome an operator can read.
while read -r value; do
  [ -n "$value" ] || continue
  require "$value" "overflow reason"
done <<<"$(grep -oE 'ArchiveOverflow[A-Za-z]+ ArchiveOverflowReason = "[a-z_]+"' \
  internal/event/schema.go | sed -E 's/.*= "//; s/"//' | sort -u)"

while read -r value; do
  [ -n "$value" ] || continue
  require "$value" "commentary outcome"
done <<<"$(grep -oE 'Commentary[A-Za-z]+ CommentaryOutcome = "[a-z_]+"' \
  internal/eventarchive/report.go | sed -E 's/.*= "//; s/"//' | sort -u)"

# The bounds an operator plans disk around. They are read from the code so the
# document cannot quietly describe a different archive.
age="$(grep -oE 'DefaultMaxAge = [0-9]+ \* [0-9]+ \* time\.Hour' \
  internal/eventarchive/archive.go | grep -oE '^DefaultMaxAge = [0-9]+' |
  grep -oE '[0-9]+$' || true)"
[ -n "$age" ] || age=30
grep -qE "\| $age days \|" "$document" || {
  printf 'the age bound is %s days and the document does not say so\n' "$age" >&2
  status=1
}
grep -q '256 \* 1024 \* 1024' internal/eventarchive/archive.go &&
  { grep -qE '\| 256 MiB \|' "$document" || {
    printf 'the size bound is 256 MiB and the document does not say so\n' >&2
    status=1
  }; }

# The commands it tells an operator to run, and the flag that enables the
# whole thing.
for binary in hexroute-archive-report hexroute-archive-annotate; do
  grep -qF "$binary" "$document" || {
    printf '%s is not named in %s\n' "$binary" "$document" >&2
    status=1
  }
  [ -d "cmd/$binary" ] || {
    printf '%s is documented and is not a command here\n' "$binary" >&2
    status=1
  }
done

for argument in --connectivity-event-archive; do
  grep -qF -- "$argument" "$document" || {
    printf '%s is not named in %s\n' "$argument" "$document" >&2
    status=1
  }
  grep -rqF -- "$argument" internal/rootdaemon deploy/macos || {
    printf '%s is documented and no job or daemon takes it\n' "$argument" >&2
    status=1
  }
done

# The installer subcommands the document tells an operator to type.
for subcommand in install attempts reports uninstall; do
  grep -qE "^  $subcommand\)" scripts/macos/archive-review-launchd.sh || {
    printf 'the document names `%s` and the installer has no such subcommand\n' \
      "$subcommand" >&2
    status=1
  }
done

[ "$status" -eq 0 ] || exit 1

printf 'ok: the local event archive documents every value it can report\n'
