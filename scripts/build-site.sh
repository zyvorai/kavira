#!/usr/bin/env bash
# Assemble the Pages site into _site/: site/ plus the shared images from docs/social and docs/ux.
# Same command locally: ./scripts/build-site.sh && python3 -m http.server -d _site 8000
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-$ROOT/_site}"
rm -rf "$OUT" && mkdir -p "$OUT/social" "$OUT/ux"
cp "$ROOT"/site/* "$OUT/"
cp "$ROOT/internal/api/static/favicon.svg" "$OUT/"
cp "$ROOT"/docs/social/*.jpg "$OUT/social/"
cp "$ROOT"/docs/ux/*.png "$ROOT"/docs/ux/*.jpg "$ROOT"/docs/ux/*.gif "$OUT/ux/"
touch "$OUT/.nojekyll"

# Every local src/href in the page must exist in the output (the Pages equivalent of onBrokenLinks: throw).
status=0
while IFS= read -r ref; do
  case "$ref" in http*|mailto:*|\#*|"") continue ;; esac
  [[ -e "$OUT/${ref%%#*}" || "${ref%%#*}" == "./" ]] || { echo "broken reference in site/index.html: $ref" >&2; status=1; }
done < <(grep -oE '(src|href)="[^"]+"' "$ROOT/site/index.html" | sed -E 's/^(src|href)="//; s/"$//')
[[ $status -eq 0 ]] && echo "site built in $OUT"
exit $status
