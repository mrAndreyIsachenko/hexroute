# Publishing the first tunnel configuration version

The tunnel's configuration becomes a signed version before ownership of the
tunnel moves. The first version is byte for byte what the machine runs today, so
that the handover changes who owns the tunnel and nothing about what it is.

This is signed with the operator's key, which requires user presence in the
operator's own session. It is the one step of the handover nobody but the
operator can take.

## What this exposes, and for how long

The configuration lives under root today and carries the tunnel's identities.
Signing needs the bytes in a session that can reach the Keychain item, which is
the operator's. So the content is readable by the operator's account between the
copy and the removal below, and by nothing else: the copy is created `0600` in
the operator's own storage and removed in the same sequence.

Signing it as root instead was rejected. The key requires user presence, and a
signer that did not would be a key that anything running as root could use.

## Before starting

Use the signed signer application, not a `go build` of it. The same executable
must have provisioned the key, verified it and signed every generation:

```sh
hexroute-policy verify-key \
  --keychain-service '<private service>' \
  --keychain-account '<private account>' \
  --public-key "$HOME/Library/Application Support/Hexroute/policy-signer/public-key"
```

## The sequence

The host names itself by the node identity its own connectivity store records.
Reading it rather than typing it keeps the two from drifting apart, and keeps it
out of any transcript.

```sh
NODE_ID="$(sudo cat '/Library/Application Support/Hexroute/observe-root/state/connectivity/node-id')"
WORK="$HOME/Library/Application Support/Hexroute/tunnel-version"
mkdir -p "$WORK" && chmod 700 "$WORK"

sudo cp '/Library/Application Support/twilight/supervisor/client/twilight-sing-box-tun.json' \
  "$WORK/content.json"
sudo chown "$(id -u):$(id -g)" "$WORK/content.json"
chmod 600 "$WORK/content.json"

# What is about to be signed, so the digest below can be compared against it.
shasum -a 256 "$WORK/content.json"
```

Sign it. This prompts for user presence:

```sh
hexroute-policy sign-config \
  --content "$WORK/content.json" \
  --target-kind node \
  --target-key "$NODE_ID" \
  --label v1 \
  --public-key "$HOME/Library/Application Support/Hexroute/policy-signer/public-key" \
  --keychain-service '<private service>' \
  --keychain-account '<private account>' \
  --out "$WORK/v1"
```

`sign-config` reports `content_sha256`. It must equal the digest printed above.
If it does not, the bytes changed between the copy and the signature and nothing
should be installed.

Place the version where the host reads it, and remove the readable copy:

```sh
sudo install -o root -g wheel -m 0600 "$WORK/v1/version.json" \
  '/Library/Application Support/Hexroute/observe-root/config/tunnel-version.json'
rm -f "$WORK/content.json"
```

## Proving it is what runs today

The verified content must equal the file the supervisor still runs, byte for
byte. Comparing digests rather than files keeps the bytes off the terminal:

```sh
sudo /usr/bin/python3 - <<'PY'
import base64, hashlib, json
artifact = json.load(open('/Library/Application Support/Hexroute/observe-root/config/tunnel-version.json'))
signed = base64.urlsafe_b64decode(artifact['content'] + '==')
running = open('/Library/Application Support/twilight/supervisor/client/twilight-sing-box-tun.json','rb').read()
print('signed digest ', hashlib.sha256(signed).hexdigest())
print('running digest', hashlib.sha256(running).hexdigest())
print('identical:', signed == running)
PY
```

`identical: True` is the whole of this step. Anything else means the first
version is not what the machine runs, and the handover would change two things
at once — which is the thing it is arranged not to do.

## Rehearsing before using it

With the version in place, the transaction can be rehearsed. A rehearsal
performs every phase except claiming the tunnel and starting the process:

```sh
sudo /Library/Application\ Support/Hexroute/observe-root/bin/hexroute-handover \
  --config '/Library/Application Support/Hexroute/observe-root/config/root-observe.json' \
  rehearse
```

It reports whether the payload traversed twice inside the deadline, which is the
same evidence the real handover completes on.
