"""Does this runtime's route list cover what the supervisor actually applies?

Run before a tunnel handover. A destination only the supervisor knows would
simply stop being routed the moment ownership moved, and the same ground is not
the same list.

It lives here rather than in a scratch directory because it decides whether a
handover is safe, and a copy nobody can see drifts: it reported five
disagreements the day a role was added to the planner and not to it.
tests/route_coverage_roles_test.sh holds the two together.

Prints counts, roles and a masked label. Addresses stay on the machine, the same
discipline the relay projection keeps and for the same reason.
"""
import hashlib, json, os, re, subprocess, sys

ENV = "/Library/Application Support/twilight/supervisor/.env"
HEXROUTE = "/Library/Application Support/Hexroute/observe-root/config/root-observe.json"


def mask(value):
    return hashlib.sha256(value.encode()).hexdigest()[:8]


def env_list(name):
    try:
        text = open(ENV, errors="replace").read()
    except Exception as error:
        raise SystemExit("cannot read the supervisor environment: %s" % error)
    found = None
    for line in text.splitlines():
        match = re.match(r"^%s=(.*)$" % re.escape(name), line)
        if match:
            found = match.group(1).strip().strip('"').strip("'")
    return [item for item in (found or "").split() if item]


scoped = env_list("TWILIGHT_SCOPED_ENDPOINTS")
direct_ips = env_list("TWILIGHT_DIRECT_IPS")
direct_hosts = env_list("TWILIGHT_DIRECT_HOSTS")

config = json.load(open(HEXROUTE))
routes = config.get("routes") or []
by_address = {route["address"]: route for route in routes}

print("the supervisor applies")
print("  through the tunnel (scoped endpoints): %d" % len(scoped))
print("  around it, by address (direct ips):    %d" % len(direct_ips))
print("  around it, by name (direct hosts):     %d" % len(direct_hosts))
print("this runtime plans")
print("  routes by address:                     %d" % len(routes))
print("  routes by name:                        0  (it has no such notion)")

supervisor = set(scoped) | set(direct_ips)
mine = set(by_address)

# Which side of the tunnel this runtime puts a destination on is decided by its
# role, not by the preferred_link field: routeplan.desiredPath sends corporate
# and gitlab_https to the TUN unconditionally and refuses a preference for them
# at all, and codex_fallback is routed only while the normal Codex path is down.
# Reading the field instead invented a disagreement about corporate traffic that
# does not exist.
def side(route):
    role = route.get("role")
    if role in ("corporate", "gitlab_https"):
        return "tunnel"
    if role == "codex_fallback":
        return "tunnel, and only while the normal Codex path is down"
    if role == "inherited":
        return "tunnel"
    if role == "ingress":
        return "tunnel" if route.get("preferred_link") == "upstream_vpn" else "direct"
    return "unknown role %s" % role


print("\nonly the supervisor knows: %d" % len(supervisor - mine))
for address in sorted(supervisor - mine):
    where = "tunnel" if address in scoped else "direct"
    print("  %s  %s" % (mask(address), where))

print("only this runtime knows: %d" % len(mine - supervisor))
for address in sorted(mine - supervisor):
    route = by_address[address]
    print("  %s  role=%-14s %s" % (mask(address), route.get("role"), side(route)))

print("both know: %d" % len(supervisor & mine))
disagree = 0
for address in sorted(supervisor & mine):
    route = by_address[address]
    mine_side = side(route)
    supervisor_side = "tunnel" if address in scoped else "direct"
    agrees = mine_side.startswith(supervisor_side)
    if not agrees:
        disagree += 1
    print("  %s  role=%-14s this runtime: %s | the supervisor: %s%s"
          % (mask(address), route.get("role"), mine_side, supervisor_side,
             "" if agrees else "   <-- disagree"))
print("  of which disagree about which side of the tunnel: %d" % disagree)

if direct_hosts:
    print("\nThe supervisor routes %d destinations by name and re-resolves them."
          % len(direct_hosts))
    print("This runtime has no route by name. Those cannot be covered by an")
    print("address list, however long, because the addresses change.")
