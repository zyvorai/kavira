#!/usr/bin/env bash
# Fails when the version in cmd/kavira/main.go disagrees with the changelog or the deploy manifests.
# Same command locally: ./scripts/check-version-sync.sh   (CI: ci.yml test, release.yml verify)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
code="$(sed -n 's/^var version = "\(.*\)"$/\1/p' "$ROOT/cmd/kavira/main.go")"
[[ -n "$code" ]] || { echo "could not find 'var version' in cmd/kavira/main.go" >&2; exit 1; }
fail=0
check() { if ! grep -q "$2" "$1"; then echo "$1 does not mention $2" >&2; fail=1; fi; }
check "$ROOT/CHANGELOG.md" "^## $code\b"
check "$ROOT/deploy/deployment.yaml" "kavira:$code"
check "$ROOT/deploy/docker-compose.yml" "KAVIRA_VERSION:-$code"
check "$ROOT/deploy/README.md" "kavira:$code"
if [[ -n "${GITHUB_REF_NAME:-}" && "${GITHUB_REF_TYPE:-}" == tag && "$GITHUB_REF_NAME" != "v$code" ]]; then
  echo "tag $GITHUB_REF_NAME does not match version v$code" >&2; fail=1
fi
[[ $fail -eq 0 ]] && echo "version $code is in sync"
exit $fail
