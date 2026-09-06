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
# to the documents where the mapping would be complete.
#
# docs/architecture/ingress-clients.md is the same document from the other end.
# It maps purpose to client, and must never also map a client to an identity, a
# credential or a host: the two documents read together would say who can reach
# what, which is the whole of what the private repository holds.
#
# The second check is general. No public document may carry a routable IPv4
# address. Loopback, private and documentation ranges are allowed, because
# examples need them.

set -euo pipefail
cd "$(dirname "$0")/.."

fleet=docs/architecture/ingress-fleet.md
clients=docs/architecture/ingress-clients.md
failed=0

for document in "$fleet" "$clients"; do
	[ -s "$document" ] || {
		printf '%s is missing or empty\n' "$document" >&2
		exit 1
	}
done

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

# The client document must record what it exists to record: that each client
# holds its own identity, and that removal is a published version.
for phrase in \
	'identity of its own' \
	'publishing a configuration version' \
	'derived from the configuration version'
do
	grep -qF "$phrase" "$clients" || {
		printf '%s does not record: %s\n' "$clients" "$phrase" >&2
		failed=1
	}
done

# Neither may name a provider, a region or a product.
for document in "$fleet" "$clients"; do
	hits=$(grep -inE 'digitalocean|lightsail|\baws\b|amazon|droplet|frankfurt|virginia|amsterdam|singapore|hong kong|[a-z]{2}-(east|west|north|south|central|southeast|northeast)-[0-9]' \
		"$document" || true)
	if [ -n "$hits" ]; then
		printf '%s names a provider, product or region:\n' "$document" >&2
		printf '%s\n' "$hits" | sed 's/^/  /' >&2
		failed=1
	fi
done

# The client document must not carry an identity or a credential. A client's
# identity is a UUID in the private repository; one appearing here would say
# which client is which, and the purposes above would say what that one reaches.
identities=$(grep -inoE '\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b|\b[A-Za-z0-9_-]{43}\b' \
	"$clients" || true)
if [ -n "$identities" ]; then
	printf '%s carries what could be a client identity or key:\n' "$clients" >&2
	printf '%s\n' "$identities" | sed 's/^/  /' >&2
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
printf 'ok: the fleet and client documents record purpose and publish no shape\n'
