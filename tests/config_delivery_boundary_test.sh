#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

export GOCACHE="${GOCACHE:-${TMPDIR:-/tmp}/hexroute-go-cache}"

# Signed configuration delivery adds a way to change what a remote host runs.
# Three things must stay true of it, and none of them is visible in a Go test
# of any one package:
#
#   1. The cloud gained no route by which it can tell a host to change.
#   2. Nothing live — a bucket, a credential, an address, the operator public
#      key — entered this public repository.
#   3. Nothing on the operator's own machine changed.
#
# Each is checked here against the tree rather than against a promise.

status=0
fail() {
  printf '%s\n' "$1" >&2
  status=1
}

delivery_packages=(
  internal/configversion
  internal/configpublish
  internal/configprove
  internal/ingressagent
)

# 1. The cloud stores and records. It has no node-facing route, so the only
#    thing it could do with one would be to grow it here.
cloud_deps="$(go list -deps ./cmd/hexroute-ingest)"
for package in internal/ingressagent internal/configpublish internal/configprove; do
  if printf '%s\n' "$cloud_deps" | grep -q "/${package}\$"; then
    fail "the cloud runtime imports ${package}, which is a delivery path"
  fi
done

# A node obtains a version by its own action. Nothing in the publishing or
# recording path may name an operation performed on a host: a queue, a
# dispatch, a command, an instruction to apply.
if grep -RniE 'func .*(Enqueue|Dispatch|Notify|Push|Command|Instruct|Trigger)[A-Za-z]*\(' \
  --include='*.go' internal/configpublish internal/configprove \
  | grep -viE '_test\.go' | grep -q .; then
  fail 'the publishing path exports something that reads as an instruction to a host'
  grep -RniE 'func .*(Enqueue|Dispatch|Notify|Push|Command|Instruct|Trigger)[A-Za-z]*\(' \
    --include='*.go' internal/configpublish internal/configprove | grep -viE '_test\.go' >&2
fi

# A node that is off during a publish must be left with nothing queued against
# it. There is no table and no column for that, and this is what keeps it so.
if grep -RniE 'pending_(instruction|command|action)|node_(commands|instructions)' \
  --include='*.sql' internal/database/migrations | grep -q .; then
  fail 'the schema grew somewhere a node instruction could wait'
fi

# The agent runs on a host in someone else's data centre. It carries what it
# needs to fetch and verify, and nothing that talks to the cloud's database:
# it once linked a PostgreSQL driver, by way of an object store that imported
# its own first caller, purely to work out a filename.
agent_deps="$(go list -deps ./cmd/hexroute-ingress-agent)"
if printf '%s\n' "$agent_deps" | grep -qE '^github\.com/jackc/pgx'; then
  fail 'the ingress agent links a database driver'
fi
for package in internal/configpublish internal/configprove internal/cloudruntime \
  internal/cloudingest internal/incidentbundle internal/database; do
  if printf '%s\n' "$agent_deps" | grep -q "/${package}\$"; then
    fail "the ingress agent imports ${package}"
  fi
done

# Delivery does not interpret what it delivers. The version format, its
# verification and the agent handle content as bytes: the digest binds it and
# nothing else looks inside. That is what makes the path usable by a runtime it
# was not written for, and the first code to read a delivered configuration has
# to argue for itself here rather than arrive unnoticed.
#
# Two rules, both narrow enough to mean something. The artifact's content field
# is touched only where it is decoded from base64 and checked against the signed
# digest, which is the format package and nowhere else. And no package on this
# path names a runtime's configuration schema, because knowing one is the whole
# of what interpreting content would require.
content_readers="$(grep -rn '\.Content\b' internal/configversion internal/ingressagent \
  internal/configpublish internal/configprove | grep -v '_test\.go' \
  | grep -v '^internal/configversion/version.go:' || true)"
if [ -n "$content_readers" ]; then
  fail 'version content is read outside the format that binds it:'
  printf '%s\n' "$content_readers" >&2
fi

for package in "${delivery_packages[@]}"; do
  schema="$(grep -rnE '\b(inbounds|outbounds|serverNames|shortIds|dokodemo|freedom)\b' \
    "$package" | grep -v '_test\.go' || true)"
  if [ -n "$schema" ]; then
    fail "$package names a runtime configuration schema:"
    printf '%s\n' "$schema" >&2
  fi
done

# 2. Everything live arrives from the environment or from a file placed on the
#    host. A literal here would be the leak.
for package in "${delivery_packages[@]}"; do
  literals="$(grep -RnoE 'https://[A-Za-z0-9.-]+' --include='*.go' "$package" \
    | grep -v '_test\.go' || true)"
  if [ -n "$literals" ]; then
    fail "$package names an endpoint literally:"
    printf '%s\n' "$literals" >&2
  fi

  # An Ed25519 key is 32 bytes: 43 characters unpadded, 44 padded. Nothing in
  # this repository should carry one outside a test fixture.
  keys="$(grep -RnoE '"[A-Za-z0-9+/_-]{43}={0,1}"' --include='*.go' "$package" \
    | grep -v '_test\.go' || true)"
  if [ -n "$keys" ]; then
    fail "$package carries what could be a public key:"
    printf '%s\n' "$keys" >&2
  fi

  addresses="$(grep -RnoE '\b([0-9]{1,3}\.){3}[0-9]{1,3}\b' --include='*.go' "$package" \
    | grep -v '_test\.go' | grep -vE '127\.0\.0\.1|0\.0\.0\.0' || true)"
  if [ -n "$addresses" ]; then
    fail "$package names a routable address:"
    printf '%s\n' "$addresses" >&2
  fi
done

# 3. Nothing on the operator's machine changed. The local daemons, the
#    installer and the runtime cannot reach any of this.
local_packages=(
  ./cmd/hexrouted
  ./cmd/hexroute-userd
  ./cmd/hexroutectl
  ./cmd/hexroute-policy-installer
  ./cmd/hexroute-sentinel
)
local_deps="$(go list -deps "${local_packages[@]}")"
for package in "${delivery_packages[@]}" internal/objectstore; do
  if printf '%s\n' "$local_deps" | grep -q "/${package}\$"; then
    fail "a local daemon imports ${package}"
  fi
done

# The one local binary that touches any of this is the policy signer, and only
# to sign: signing a configuration is the same authority as signing a policy
# generation, which is why it is there and why it may not bring anything else
# with it.
signer_deps="$(go list -deps ./cmd/hexroute-policy)"
if ! printf '%s\n' "$signer_deps" | grep -q '/internal/configversion$'; then
  fail 'the policy signer cannot sign a configuration version'
fi
for package in internal/configpublish internal/configprove internal/ingressagent internal/objectstore; do
  if printf '%s\n' "$signer_deps" | grep -q "/${package}\$"; then
    fail "the policy signer imports ${package}"
  fi
done

# The signing path stays offline. A signer that could reach a network could
# publish, and then the separation between signing and publishing would be a
# convention rather than a fact.
if printf '%s\n' "$signer_deps" | grep -qE '^(net/http|github\.com/jackc/pgx)'; then
  fail 'the policy signer gained a network or database dependency'
fi

[ "$status" -eq 0 ] || exit 1

printf 'ok: the cloud cannot instruct a host, nothing live is in the tree, and the local machine is untouched\n'
