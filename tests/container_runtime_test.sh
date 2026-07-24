#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
image="${1:-hexroute-ingest:contract}"
containers=()

cleanup() {
  local container
  for container in "${containers[@]:-}"; do
    docker rm -f "$container" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT

docker build \
  --build-arg "VERSION=container-contract" \
  --build-arg "COMMIT=0000000000000000000000000000000000000000" \
  --tag "$image" \
  "$repo_root"

image_user="$(docker image inspect --format '{{.Config.User}}' "$image")"
[[ "$image_user" == "65532:65532" ]] || {
  printf 'image user is %q, expected 65532:65532\n' "$image_user" >&2
  exit 1
}
entrypoint="$(docker image inspect \
  --format '{{json .Config.Entrypoint}}' "$image")"
[[ "$entrypoint" == '["/usr/local/bin/hexroute-ingest"]' ]] || {
  printf 'unexpected image entrypoint: %s\n' "$entrypoint" >&2
  exit 1
}

for component in api worker; do
  container="hexroute-${component}-contract-$$"
  containers+=("$container")
  docker create \
    --name "$container" \
    --read-only \
    --tmpfs /tmp:rw,noexec,nosuid,nodev,size=16777216,uid=65532,gid=65532,mode=0700 \
    --user 65532:65532 \
    --cap-drop ALL \
    --security-opt no-new-privileges:true \
    --network none \
    --pids-limit 128 \
    --env "HEXROUTE_COMPONENT=$component" \
    "$image" \
    --check >/dev/null

  [[ "$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$container")" == "true" ]] || {
    printf '%s root filesystem is not read-only\n' "$component" >&2
    exit 1
  }
  [[ "$(docker inspect --format '{{.Config.User}}' "$container")" == "65532:65532" ]] || {
    printf '%s container is not non-root\n' "$component" >&2
    exit 1
  }
  [[ "$(docker inspect --format '{{json .HostConfig.CapDrop}}' "$container")" == '["ALL"]' ]] || {
    printf '%s container does not drop all capabilities\n' "$component" >&2
    exit 1
  }
  security_options="$(docker inspect \
    --format '{{json .HostConfig.SecurityOpt}}' "$container")"
  [[ "$security_options" == *no-new-privileges* ]] || {
    printf '%s container permits privilege escalation\n' "$component" >&2
    exit 1
  }
  tmpfs="$(docker inspect --format '{{json .HostConfig.Tmpfs}}' "$container")"
  [[ "$tmpfs" == *'"/tmp":'* ]] || {
    printf '%s container has no declared /tmp tmpfs\n' "$component" >&2
    exit 1
  }

  output="$(docker start --attach "$container")"
  [[ "$output" == *'"event":"startup_check"'* ]] || {
    printf '%s startup check did not complete: %s\n' "$component" "$output" >&2
    exit 1
  }
done

for component in api worker; do
  failure="hexroute-${component}-invalid-config-contract-$$"
  containers+=("$failure")
  docker create \
    --name "$failure" \
    --read-only \
    --tmpfs /tmp:rw,noexec,nosuid,nodev,size=16777216,uid=65532,gid=65532,mode=0700 \
    --user 65532:65532 \
    --cap-drop ALL \
    --security-opt no-new-privileges:true \
    --network none \
    "$image" \
    "$component" >/dev/null
  failure_output="$(docker start --attach "$failure" 2>&1 || true)"
  failure_exit="$(docker inspect --format '{{.State.ExitCode}}' "$failure")"
  [[ "$failure_exit" == "1" ]] || {
    printf 'unconfigured %s exit code is %s, expected 1\n' \
      "$component" "$failure_exit" >&2
    exit 1
  }
  [[ "$failure_output" == *'"reason":"invalid_configuration"'* ]] || {
    printf 'unconfigured %s did not emit a bounded configuration error\n' \
      "$component" >&2
    exit 1
  }
  if [[ "$failure_output" == *'DATABASE_URL'* || "$failure_output" == *'postgres://'* ]]; then
    printf 'unconfigured %s log exposed configuration detail\n' "$component" >&2
    exit 1
  fi
done

probe_container="hexroute-image-contents-contract-$$"
containers+=("$probe_container")
docker create --name "$probe_container" "$image" --check >/dev/null
archive="$(mktemp "${TMPDIR:-/tmp}/hexroute-container.XXXXXX.tar")"
trap 'rm -f "$archive"; cleanup' EXIT
docker export "$probe_container" >"$archive"
if tar -tf "$archive" | rg --quiet \
  '(^|/)(bin/(sh|bash|ash)|busybox|apk|apt|dpkg)(/|$)'; then
  printf 'runtime image contains a shell or package manager\n' >&2
  exit 1
fi

printf 'ok: api and worker containers enforce the hardened runtime contract\n'
