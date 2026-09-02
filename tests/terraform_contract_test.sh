#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
terraform_root="$repo_root/terraform"
lightsail="$terraform_root/modules/lightsail-ingress"

required_modules=(
  app-platform
  dns-records
  ingress-hosts
  lightsail-ingress
  managed-postgresql
  private-spaces
  uptime-checks
)

for module in "${required_modules[@]}"; do
  test -d "$terraform_root/modules/$module" || {
    printf 'terraform module %s is required here and does not exist\n' \
      "$module" >&2
    exit 1
  }
  for file in main.tf outputs.tf variables.tf versions.tf; do
    test -f "$terraform_root/modules/$module/$file" || {
      printf 'missing terraform module file: %s/%s\n' "$module" "$file" >&2
      exit 1
    }
  done
done

# And the other direction. Checking only the listed modules let a new one
# arrive with none of the four files and nobody deciding it should exist —
# the same shape as a package reaching no binary, and caught the same way.
for directory in "$terraform_root"/modules/*/; do
  module="$(basename "$directory")"
  listed=0
  for required in "${required_modules[@]}"; do
    [[ "$module" == "$required" ]] && listed=1 && break
  done
  [[ "$listed" == "1" ]] || {
    printf 'terraform module %s exists and is on no list\n' "$module" >&2
    printf '  add it to required_modules, so its shape is checked and its\n' >&2
    printf '  existence is a decision someone wrote down\n' >&2
    exit 1
  }
done

terraform fmt -check -recursive "$terraform_root"

if rg -n --glob '*.tf' \
  'backend[[:space:]]+"|provider[[:space:]]+"(aws|digitalocean)"|DIGITALOCEAN_ACCESS_TOKEN[[:space:]]*=' \
  "$terraform_root"; then
  printf 'public terraform must not define live backends, providers, or management tokens\n' >&2
  exit 1
fi
for template in \
  cloud-init.yaml.tftpl \
  install-runtime.sh.tftpl \
  hexroute-xray.service.tftpl \
  hexroute-ingress-observer.service.tftpl; do
  test -f "$lightsail/templates/$template" || {
    printf 'Lightsail ingress runtime template is missing: %s\n' "$template" >&2
    exit 1
  }
done
rg -Fq 'user_data         = local.runtime_bootstrap' "$lightsail/main.tf"
rg -Fq 'runtime_bootstrap_sha256' "$lightsail/outputs.tf"
rg -Fq 'sha256(local.runtime_bootstrap)' "$lightsail/outputs.tf"
rg -Fq -- "--proto '=https' --tlsv1.2" \
  "$lightsail/templates/install-runtime.sh.tftpl"
rg -Fq 'sha256sum --check --status' \
  "$lightsail/templates/install-runtime.sh.tftpl"
for unit in \
  hexroute-xray.service.tftpl \
  hexroute-ingress-observer.service.tftpl; do
  rg -Fq 'User=hexroute-ingress' "$lightsail/templates/$unit"
  rg -Fq 'Group=hexroute-ingress' "$lightsail/templates/$unit"
  rg -Fq 'NoNewPrivileges=true' "$lightsail/templates/$unit"
  rg -Fq 'ProtectSystem=strict' "$lightsail/templates/$unit"
  rg -Fq 'ProtectHome=true' "$lightsail/templates/$unit"
  rg -Fq 'PrivateDevices=true' "$lightsail/templates/$unit"
  rg -Fq 'ConditionPathExists=/etc/hexroute/runtime/' \
    "$lightsail/templates/$unit"
done
rg -Fq 'CapabilityBoundingSet=CAP_NET_BIND_SERVICE' \
  "$lightsail/templates/hexroute-xray.service.tftpl"
rg -Fxq 'CapabilityBoundingSet=' \
  "$lightsail/templates/hexroute-ingress-observer.service.tftpl"
if rg -ni \
  'vless|reality|private[_ -]?key|uuid|sni|signing[_ -]?secret|password|bearer[_ -]?token' \
  "$lightsail/templates"; then
  printf 'Lightsail runtime templates contain transport or heartbeat secret material\n' >&2
  exit 1
fi

rg -q 'digest[[:space:]]*=[[:space:]]*var.image.digest' \
  "$terraform_root/modules/app-platform/main.tf"
rg -q 'provider management credentials cannot enter' \
  "$terraform_root/modules/app-platform/main.tf"
rg -q 'HEXROUTE_COMPONENT.*reserved' \
  "$terraform_root/modules/app-platform/main.tf"
if rg -n 'disable_edge_cache' \
  "$terraform_root/modules/app-platform/main.tf"; then
  printf 'App Platform cache flags require a directly attached custom domain\n' >&2
  exit 1
fi
for component in service worker job; do
  rg -q "spec\\[0\\]\\.${component}\\[0\\]\\.image\\[0\\]\\.registry," \
    "$terraform_root/modules/app-platform/main.tf"
  rg -q "spec\\[0\\]\\.${component}\\[0\\]\\.image\\[0\\]\\.registry_credentials," \
    "$terraform_root/modules/app-platform/main.tf"
done
rg -q 'kind[[:space:]]*=[[:space:]]*"PRE_DEPLOY"' \
  "$terraform_root/modules/app-platform/main.tf"
rg -q 'migration job must receive exactly the migrator database URL secret' \
  "$terraform_root/modules/app-platform/main.tf"
[[ "$(rg -F -c '  lifecycle {' \
  "$terraform_root/modules/app-platform/main.tf")" == 1 ]] || {
  printf 'App Platform resource must have exactly one lifecycle block\n' >&2
  exit 1
}
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

[[ "$(rg -c '^resource "aws_lightsail_' "$lightsail/main.tf")" == 4 ]] || {
  printf 'Lightsail ingress module must own exactly four Lightsail resources\n' >&2
  exit 1
}
for resource in \
  aws_lightsail_instance \
  aws_lightsail_static_ip \
  aws_lightsail_static_ip_attachment \
  aws_lightsail_instance_public_ports; do
  rg -Fq "resource \"$resource\"" "$lightsail/main.tf"
done
rg -Fq 'ip_address_type   = "ipv4"' "$lightsail/main.tf"
rg -Fq 'from_port = 443' "$lightsail/variables.tf"
rg -Fq 'cidrs     = ["0.0.0.0/0"]' "$lightsail/variables.tf"
rg -Fq 'endswith(cidr, "/32")' "$lightsail/variables.tf"
if rg -n --glob '*.tf' \
  'variable "([^" ]*(secret|credential|password|token|uuid|private_key)[^" ]*|sni|user_data)"' \
  "$lightsail"; then
  printf 'Lightsail ingress module accepts a secret-bearing input\n' >&2
  exit 1
fi
if rg -n --glob '*.tf' 'resource "' "$lightsail" |
  rg -v 'resource "aws_lightsail_(instance|static_ip|static_ip_attachment|instance_public_ports)"'; then
  printf 'Lightsail ingress module owns an unrelated provider resource\n' >&2
  exit 1
fi
if rg -n --glob '*.tf' '[0-9]{12}|arn:aws:|AKIA[0-9A-Z]{16}' "$lightsail"; then
  printf 'Lightsail ingress module contains a live AWS identity or credential\n' >&2
  exit 1
fi

rg -q 'source[[:space:]]*=[[:space:]]*"uptimerobot/uptimerobot"' \
  "$terraform_root/modules/uptime-checks/versions.tf"
rg -q 'resource "uptimerobot_monitor"' \
  "$terraform_root/modules/uptime-checks/main.tf"
rg -q 'resource "uptimerobot_integration" "telegram"' \
  "$terraform_root/modules/uptime-checks/main.tf"
rg -q 'assigned_alert_contacts' \
  "$terraform_root/modules/uptime-checks/main.tf"
rg -q 'value[[:space:]]*=[[:space:]]*"uptimerobot-managed-telegram"' \
  "$terraform_root/modules/uptime-checks/main.tf"
rg -Fq 'custom_value             = sensitive("")' \
  "$terraform_root/modules/uptime-checks/main.tf"
rg -q 'ignore_changes[[:space:]]*=[[:space:]]*\[custom_value\]' \
  "$terraform_root/modules/uptime-checks/main.tf"
if rg -n 'bot_token|chat_id' \
  "$terraform_root/modules/uptime-checks"; then
  printf 'UptimeRobot managed Telegram integration must not accept Telegram credentials\n' >&2
  exit 1
fi
if rg -n --glob '*.tf' 'digitalocean_uptime_(check|alert)' \
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
