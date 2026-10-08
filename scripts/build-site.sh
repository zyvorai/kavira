#!/usr/bin/env bash
# Assemble the Pages site into _site/:
#   /            site/ (landing) + shared images from docs/social and docs/ux
#   /demo/       the REAL console (internal/api/static) running on precompiled results, no server
# The demo data is recorded from the real API of a throwaway local server, so shapes cannot drift from it.
# Same command locally: ./scripts/build-site.sh && python3 -m http.server -d _site 8000   (then open /demo/)
# (Project Pages serve under /kavira/; every URL here is relative so it works there and at the root.)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-$ROOT/_site}"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/kavira-site.XXXXXX")"
SERVER_PID=""
# shellcheck disable=SC2329 # invoked through the EXIT trap below
cleanup() {
  if [[ -n "$SERVER_PID" ]]; then kill "$SERVER_PID" 2>/dev/null || true; fi
  rm -rf "$TMP"
}
trap cleanup EXIT

rm -rf "$OUT" && mkdir -p "$OUT/social" "$OUT/ux" "$OUT/demo/data"
cp -R "$ROOT"/site/. "$OUT/"
rm -rf "$OUT/demo-src" # demo sources are copied into demo/ below, not published as-is
cp "$ROOT/internal/api/static/favicon.svg" "$OUT/"
cp "$ROOT"/docs/social/*.jpg "$OUT/social/"
cp "$ROOT"/docs/ux/*.png "$ROOT"/docs/ux/*.jpg "$ROOT"/docs/ux/*.gif "$OUT/ux/"
touch "$OUT/.nojekyll"

# --- record the real API ----------------------------------------------------------------------
echo "building kavira and recording the API for the demo"
(cd "$ROOT" && go build -o "$TMP/kavira" ./cmd/kavira)
PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
PW="site-build-$RANDOM$RANDOM"
KAVIRA_ADMIN_PASSWORD="$PW" KAVIRA_SESSION_KEY="site-build-key" "$TMP/kavira" serve -addr "127.0.0.1:$PORT" >"$TMP/server.log" 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 50); do curl -fsS "http://127.0.0.1:$PORT/api/v1/health" >/dev/null 2>&1 && break; sleep 0.2; done
api() { curl -fsS -b "$TMP/jar" "http://127.0.0.1:$PORT/api/v1/$1"; }
curl -fsS -c "$TMP/jar" -H 'Content-Type: application/json' -d "{\"username\":\"admin\",\"password\":\"$PW\"}" \
  "http://127.0.0.1:$PORT/api/v1/session" >/dev/null
D="$OUT/demo/data"
api incidents >"$D/incidents.json"
api audit >"$D/audit.json"
api fixtures >"$D/fixtures.json"
api health >"$D/health.json"
for id in $(python3 -c 'import json,sys; [print(i["id"]) for i in json.load(open(sys.argv[1]))["incidents"]]' "$D/incidents.json"); do
  api "incidents/$id" >"$D/incident-$id.json"
done
kill "$SERVER_PID"; SERVER_PID=""

# --- the demo: the real console with a static api.js ----------------------------------------------
cp -R "$ROOT"/internal/api/static/. "$OUT/demo/"
cp "$ROOT/site/demo-src/demo-api.js" "$OUT/demo/js/api.js"
# Absolute asset paths become relative, so the demo works under any sub-path.
sed -E -i.bak 's#(src|href)="/#\1="#g' "$OUT/demo/index.html"
sed -i.bak 's#<meta name="color-scheme" content="light dark" />#&\n  <meta name="robots" content="noindex" />#' "$OUT/demo/index.html"
rm -f "$OUT/demo/index.html.bak"

# --- every local reference in the landing page and the demo shell must exist ---------------------
status=0
check_refs() { # html-file base-dir
  while IFS= read -r ref; do
    case "$ref" in http*|mailto:*|\#*|"") continue ;; esac
    [[ -e "$2/${ref%%[?#]*}" || "${ref%%[?#]*}" == "./" ]] || { echo "broken reference in ${1#"$ROOT"/}: $ref" >&2; status=1; }
  done < <(grep -oE '(src|href)="[^"]+"' "$1" | sed -E 's/^(src|href)="//; s/"$//')
}
check_refs "$ROOT/site/index.html" "$OUT"
check_refs "$OUT/demo/index.html" "$OUT/demo"
[[ $status -eq 0 ]] && echo "site built in $OUT"
exit $status
