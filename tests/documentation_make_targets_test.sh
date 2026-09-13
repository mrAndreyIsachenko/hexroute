#!/bin/bash
# Every `make <target>` a document hands an operator to run must exist.
#
# A runbook naming a target nobody wrote fails at the moment somebody is
# following it, which is the moment they are least able to work out what was
# meant. This repository has already shipped a ceremony naming a signing key
# that was not the one the host pins, for the same reason: the line was written
# from memory rather than from the machine.
#
# Only fenced blocks are read. Prose mentions a target belonging to the other
# runtime's repository, which is not on this machine in CI, and a gate that read
# sentences would be reporting on a Makefile it cannot see.
set -euo pipefail
cd "$(dirname "$0")/.."

targets=$(awk -F: '/^[a-zA-Z0-9][a-zA-Z0-9_-]*:/ {print $1}' Makefile | sort -u)

fenced() {
  awk '
    /^```/ { fenced = !fenced; next }
    fenced { print }
  ' "$1"
}

missing=0
checked=0
while read -r document; do
  while read -r target; do
    [ -n "$target" ] || continue
    checked=$((checked + 1))
    if ! printf '%s\n' "$targets" | grep -qx "$target"; then
      echo "$document names a make target that does not exist: make $target"
      missing=1
    fi
  done < <(fenced "$document" | grep -oE '(^|[;&|] *)make +[a-z0-9][a-z0-9_-]*' |
    sed -E 's/.*make +//' | sort -u)
done < <(find docs -name '*.md')

if [ "$missing" -ne 0 ]; then
  exit 1
fi
echo "documentation-make-targets: $checked documented invocations, all exist"
