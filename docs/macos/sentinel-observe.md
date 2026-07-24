# Sentinel Observe-Only Runtime

`hexroute-sentinel` independently reads the root control-loop heartbeat and
performs a bounded TLS handshake through the legacy Twilight SOCKS5 data path.
It tracks heartbeat progress with its own monotonic clock, so wall-clock
changes cannot manufacture a stale-loop signal.

The initial runtime is strictly observe-only. A stale heartbeat alone and a
failed data-path probe alone are insufficient. When both signals persist, the
sentinel records `sentinel_restart_evidence`; it does not propose or perform a
restart at this stage.

```sh
go build -o bin/hexroute-sentinel ./cmd/hexroute-sentinel
bin/hexroute-sentinel \
  --check \
  --config deploy/macos/sentinel-observe.example.json
```

The synthetic example must be replaced by an untracked private config before a
live observation. No route, process, service or credential identifiers other
than the candidate heartbeat path belong in the public file.
