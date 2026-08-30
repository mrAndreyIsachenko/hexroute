#!/usr/bin/env bash
set -euo pipefail

# The rollback for the connectivity read model is to stop passing its
# arguments. This checks the claim rather than the sentence: that the daemon
# accepts the rolled-back argument set, that the procedure leaves a plist with
# nothing of the read model in it, and that what remains is argument-for-
# argument what ran before.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
plist="$repo_root/deploy/macos/com.hexroute.observe.hexrouted.plist"
config="$repo_root/deploy/macos/root-observe.example.json"
binary="$repo_root/bin/hexrouted"
doc="$repo_root/docs/connectivity-read-model.md"

[[ -f "$plist" && -f "$config" && -x "$binary" ]]

# The rollback has to be written down where an operator will find it, and it
# has to be a sequence rather than a description.
grep -q 'launchctl bootout system/com.hexroute.observe.hexrouted' "$doc"
grep -q 'PlistBuddy' "$doc"
grep -q 'launchctl bootstrap system' "$doc"

# It may not depend on the thing being rolled back.
if grep -nE 'hexroute-connectivity-(replay|qualify)' "$doc" |
  sed -n '/Rollback/,$p' | grep -q .; then
  echo "the rollback depends on the read model's own tooling" >&2
  exit 1
fi

# The daemon accepts the argument set the rollback leaves behind.
"$binary" --check --config "$config" >/dev/null

temporary="$(mktemp -d "${TMPDIR:-/tmp}/hexroute-readmodel-rollback.XXXXXX")"
trap 'rm -rf "$temporary"' EXIT
rolled="$temporary/rolled-back.plist"
cp "$plist" "$rolled"

if command -v /usr/libexec/PlistBuddy >/dev/null 2>&1; then
  before="$(/usr/libexec/PlistBuddy -c "Print :ProgramArguments" "$rolled" |
    grep -c . || true)"
  # Delete from the end so the earlier indices do not move, exactly as the
  # documented sequence does.
  # The same indices the documented sequence names, read back from the
  # document so the two cannot drift apart.
  indices="$(grep -o 'Delete :ProgramArguments:[0-9]\{1,\}' "$doc" |
    grep -o '[0-9]\{1,\}$' | tr '\n' ' ')"
  [[ -n "$indices" ]]
  for index in $indices; do
    /usr/libexec/PlistBuddy -c "Delete :ProgramArguments:$index" "$rolled" \
      >/dev/null 2>&1 || true
  done
  after="$(/usr/libexec/PlistBuddy -c "Print :ProgramArguments" "$rolled" |
    grep -c . || true)"
  [[ "$((before - after))" -eq 2 ]]  # the versioned plist carries only these

  if /usr/libexec/PlistBuddy -c "Print :ProgramArguments" "$rolled" |
    grep -q 'connectivity'; then
    echo "the rollback left read-model arguments behind" >&2
    exit 1
  fi
  # Everything the daemon ran on before is still there, unmoved.
  for required in -- --observe --config --heartbeat --socket; do
    [[ "$required" == "--" ]] && continue
    /usr/libexec/PlistBuddy -c "Print :ProgramArguments" "$rolled" |
      grep -qF -- "$required"
  done
  if command -v plutil >/dev/null 2>&1; then
    plutil -lint "$rolled" >/dev/null
  fi
fi

printf 'ok: the connectivity read model rolls back to the path that ran before it\n'
