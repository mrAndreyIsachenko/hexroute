# Terraform Modules

This directory contains reusable, provider-scoped building blocks. It does not
contain a deployable root module.

| Module | Responsibility |
| --- | --- |
| `app-platform` | Digest-pinned API, worker and pre-deploy migration components with bounded health and secret environment contracts |
| `managed-postgresql` | Managed PostgreSQL, distinct runtime identities and an explicit source firewall |
| `private-spaces` | Private, versioned incident storage with at most 30 days of retention and a bucket-scoped runtime key |
| `dns-records` | Stable DigitalOcean DNS records |
| `uptime-checks` | A bounded UptimeRobot black-box catalog for HTTP/API/content, DNS, port and heartbeat checks with independent delivery through UptimeRobot's managed Telegram bot |
| `ingress-hosts` | Provider-neutral ingress inventory and independent provider/ASN validation |
| `lightsail-ingress` | One bounded AWS Lightsail Linux ingress, static IPv4 attachment and authoritative TCP firewall contract |

## Boundary

The public repository owns module behavior, validation and synthetic fixtures.
It must not contain provider credentials, live hostnames or addresses, state,
backend configuration, production variable files, or resources selected from
an operator account.

The private `hexroute-infra` repository owns deployable root modules, encrypted
remote-state configuration, provider configuration, live values and secret
references. Runtime services receive only their own application secrets; cloud
management tokens do not enter App Platform components.

`lightsail-ingress` is a reusable capability, not a live provider-B deployment.
It has no provider/backend configuration, account or region binding, bootstrap
payload, secret input, DNS, monitoring, qualification or failover authority.
Its default firewall exposes only IPv4 TCP 443; temporary SSH is accepted only
from explicit IPv4 `/32` networks and remains governed by private expiry policy.

## Validation

The fast contract runs as part of the normal repository checks:

```sh
make terraform-contract-test
```

The provider-backed schema and synthetic-plan test downloads the pinned
DigitalOcean and UptimeRobot providers but uses Terraform mock providers, so it
neither needs credentials nor creates resources:

```sh
make terraform-test
```

The fixture under `test-fixtures/modules` is documentation and a compatibility
test. It is not a production deployment root.

`scripts/terraform-state-policy.sh` rejects local state, backup, lock-info,
crash and conventionally named plan files anywhere outside Terraform's
ignored internal metadata directory. The regression test proves that ignored
files are still detected, so `.gitignore` is not the only control.
