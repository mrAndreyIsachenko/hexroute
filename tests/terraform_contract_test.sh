#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
terraform_root="$repo_root/terraform"

required_modules=(
  app-platform
  dns-records
  ingress-hosts
  managed-postgresql
  private-spaces
  uptime-checks
)

for module in "${required_modules[@]}"; do
  for file in main.tf outputs.tf variables.tf versions.tf; do
    test -f "$terraform_root/modules/$module/$file" || {
      printf 'missing terraform module file: %s/%s\n' "$module" "$file" >&2
      exit 1
    }
  done
done

terraform fmt -check -recursive "$terraform_root"

if rg -n --glob '*.tf' \
  'backend[[:space:]]+"|provider[[:space:]]+"digitalocean"|DIGITALOCEAN_ACCESS_TOKEN[[:space:]]*=' \
  "$terraform_root"; then
  printf 'public terraform must not define live backends, providers, or management tokens\n' >&2
  exit 1
fi

rg -q 'digest[[:space:]]*=[[:space:]]*var.image.digest' \
  "$terraform_root/modules/app-platform/main.tf"
rg -q 'provider management credentials cannot enter' \
  "$terraform_root/modules/app-platform/main.tf"
rg -q 'HEXROUTE_COMPONENT.*reserved' \
  "$terraform_root/modules/app-platform/main.tf"
rg -q 'digitalocean_database_firewall' \
  "$terraform_root/modules/managed-postgresql/main.tf"
rg -q 'hexroute_ingest_runtime' \
  "$terraform_root/modules/managed-postgresql/variables.tf"
rg -q 'output "bootstrap_connection"' \
  "$terraform_root/modules/managed-postgresql/outputs.tf"
rg -q 'ignore_changes = \[settings\]' \
  "$terraform_root/modules/managed-postgresql/main.tf"
rg -q 'acl[[:space:]]*=[[:space:]]*"private"' \
  "$terraform_root/modules/private-spaces/main.tf"
rg -q 'force_destroy[[:space:]]*=[[:space:]]*false' \
  "$terraform_root/modules/private-spaces/main.tf"
rg -q 'retention_days must be between 1 and 30' \
  "$terraform_root/modules/private-spaces/variables.tf"
rg -q 'secret_reference.*"secret://"' \
  "$terraform_root/modules/ingress-hosts/variables.tf"

if rg -n --glob '*.tf' 'digitalocean_uptime_alert' \
  "$terraform_root/modules/uptime-checks" ||
  rg -n 'email' "$terraform_root/modules/uptime-checks/main.tf"; then
  printf 'uptime module must not create implicit email alerts\n' >&2
  exit 1
fi

if find "$terraform_root" \
  \( -name '*.tfstate' -o -name '*.tfstate.*' -o -name '*.tfvars' -o -name '.terraform' \) \
  -print -quit | grep -q .; then
  printf 'terraform source tree contains local state or live variable files\n' >&2
  exit 1
fi

printf 'terraform public-boundary contract passed\n'
