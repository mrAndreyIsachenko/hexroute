#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
operations="$repo_root/docs/macos/policy-operations.md"
architecture="$repo_root/docs/architecture/atomic-policy-generations.md"
qualification="$repo_root/docs/macos/policy-qualification.md"

for file in "$operations" "$architecture" "$qualification"; do
  test -s "$file"
done

for term in \
  'make install-policy-qualification' \
  'make policy-qualification-faults' \
  'make policy-qualification-arm-sleep' \
  'make policy-qualification-restart-session' \
  'Reboot downtime is accounted for' \
  'make uninstall-policy-qualification'; do
  grep -Fq "$term" "$qualification"
done

for term in \
  '"$POLICY_BIN" compile' \
  '"$POLICY_BIN" diff' \
  '"$POLICY_BIN" replay' \
  '"$POLICY_BIN" sign' \
  '"$POLICY_BIN" rollback' \
  'hexroutectl policy status' \
  'hexroutectl policy prepare' \
  'hexroutectl policy commit' \
  'hexroutectl policy abort' \
  '`restart_required`' \
  '`domain_mismatch`' \
  '`authorization_suspended`' \
  'static authority'; do
  grep -Fq "$term" "$operations"
done

for term in \
  'NVIDIA OpenShell' \
  '736e431d454c7de8a71e0fcdd3221ad6f9a552cb' \
  'independent Go implementation' \
  'GraphQL, JSON-RPC or MCP' \
  'does not import, embed or run OpenShell code' \
  '`operator_resume`'; do
  grep -Fq "$term" "$architecture"
done

grep -Fq 'docs/architecture/atomic-policy-generations.md' "$repo_root/README.md"
grep -Fq 'docs/macos/policy-operations.md' "$repo_root/README.md"
grep -Fq 'docs/macos/policy-qualification.md' "$repo_root/README.md"

printf 'ok: policy operations and OpenShell attribution documentation are complete\n'
