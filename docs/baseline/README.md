# Production Baseline Evidence

Hexroute must be able to prove that observe-only work has not changed the
installed Twilight runtime.

`scripts/baseline/capture-inventory.sh` records a read-only snapshot containing:

- macOS and relevant executable versions;
- allowlisted launchd ownership and state fields;
- a process tree containing executable names but no command arguments;
- the IPv4 route table and interface list;
- metadata and hashes for readable installed Twilight artifacts.

The generated inventory contains live infrastructure identifiers. It is
written with mode `0600` below:

```text
~/Library/Application Support/Hexroute/baseline/
```

That directory is outside Git. Inventories must not be copied into this public
repository. Exact protected root and user restore archives are separate
artifacts and require their own dry-run validation before active cutover.
