#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
architecture="$repo_root/docs/architecture/provider-b-ingress.md"

test -f "$architecture"

for heading in \
  '## Current State' \
  '## Topology' \
  '## Ownership' \
  '## Lifecycle Gates' \
  '## Independent Signals' \
  '## Failure And Rollback' \
  '## Operator Rules'; do
  rg -Fq "$heading" "$architecture"
done

for state in \
  '**Published**' \
  '**Instantiated**' \
  '**Provisioned**' \
  '**Qualified**' \
  '**Inventory-admitted**' \
  '**Failover-enabled**'; do
  rg -Fq "$state" "$architecture"
done

for signal in \
  '**TCP reachability**' \
  '**Public TLS fallback**' \
  '**Authenticated transport**' \
  '**Signed heartbeat**'; do
  rg -Fq "$signal" "$architecture"
done

for boundary in \
  'Twilight remains the selected production traffic owner' \
  'No probe result can restart XRay' \
  'Never represent provider B as production-ready' \
  'Outside the current provider-B change'; do
  rg -Fiq "$boundary" "$architecture"
done

rg -Fq 'docs/architecture/provider-b-ingress.md' "$repo_root/README.md"
rg -Fq 'docs/architecture/provider-b-ingress.md' "$repo_root/terraform/README.md"
rg -Fq '../architecture/provider-b-ingress.md' \
  "$repo_root/docs/cloud/ingress-functional-probes.md"
rg -Fq 'architecture/provider-b-ingress.md' "$repo_root/docs/roadmap.md"

if rg -n \
  '[0-9]{12}|arn:aws:|AKIA[0-9A-Z]{16}|(^|[^0-9])([0-9]{1,3}\.){3}[0-9]{1,3}([^0-9]|$)|-----BEGIN .*PRIVATE KEY-----' \
  "$architecture"; then
  printf 'provider-B architecture contains private-shaped deployment data\n' >&2
  exit 1
fi

printf 'ok: provider-B public architecture is linked, gated and value-free\n'
