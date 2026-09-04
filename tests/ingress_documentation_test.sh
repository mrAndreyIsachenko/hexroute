#!/usr/bin/env bash
# The fleet's purposes are public; its shape is not.
#
# docs/architecture/ingress-fleet.md records what each ingress host is for, so
# that a host which is slow on purpose can be told from one that is slow by
# mistake. That document is useful precisely because it maps purpose to host —
# which is why it must never also map host to provider, region or address.
# Anyone reading the three together would have the fleet's shape, and the shape
# is what the private repository exists to hold.
#
# Provider names are not banned repository-wide: docs/architecture/
# provider-b-ingress.md names them legitimately. The rule is narrow and applies
# to the one document where the mapping would be complete.
#
# The second check is general. No public document may carry a routable IPv4
# address. Loopback, private and documentation ranges are allowed, because
# examples need them.

set -euo pipefail
cd "$(dirname "$0")/.."

fleet=docs/architecture/ingress-fleet.md
failed=0

[ -s "$fleet" ] || { printf '%s is missing or empty\n' "$fleet" >&2; exit 1; }

# The document must actually record the things it exists to record.
for phrase in \
	'Primary network' \
	'Named-country address' \
	'Independent failure domain' \
	'counted in independent hosts'
do
	grep -qF "$phrase" "$fleet" || {
		printf '%s does not record: %s\n' "$fleet" "$phrase" >&2
		failed=1
	}
done

# It must not name a provider, a region or a product.
hits=$(grep -inE 'digitalocean|lightsail|\baws\b|amazon|droplet|frankfurt|virginia|amsterdam|singapore|hong kong|[a-z]{2}-(east|west|north|south|central|southeast|northeast)-[0-9]' \
	"$fleet" || true)
if [ -n "$hits" ]; then
	printf '%s names a provider, product or region:\n' "$fleet" >&2
	printf '%s\n' "$hits" | sed 's/^/  /' >&2
	failed=1
fi

# No public document may carry a routable IPv4 address.
routable=$(grep -rnoE '\b([0-9]{1,3}\.){3}[0-9]{1,3}\b' docs README.md 2>/dev/null |
	grep -vE ':(127\.|10\.|192\.168\.|192\.0\.2\.|198\.51\.100\.|203\.0\.113\.|0\.0\.0\.0|255\.255)' |
	grep -vE ':(172\.(1[6-9]|2[0-9]|3[01])\.)' || true)
if [ -n "$routable" ]; then
	printf 'a public document carries a routable address:\n' >&2
	printf '%s\n' "$routable" | sed 's/^/  /' >&2
	failed=1
fi

[ "$failed" -ne 0 ] && exit 1
printf 'ok: the fleet document records purpose and publishes no shape\n'
