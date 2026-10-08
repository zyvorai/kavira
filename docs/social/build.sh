#!/usr/bin/env bash
# Render the social images and icons from their HTML sources. Needs Google Chrome and macOS `sips`; installs nothing.
#   ./docs/social/build.sh
# Outputs:
#   docs/social/kavira-hero-dark.jpg    2400x1260  README hero, GitHub social preview, og:image
#   docs/social/kavira-social-card.jpg  1600x900   LinkedIn / X card
#   internal/api/static/apple-touch-icon.png  180x180
# GitHub's social preview is uploaded by hand: Settings > General > Social preview.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
CHROME="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
[[ -x "$CHROME" ]] || { echo "Google Chrome not found (set CHROME=...)" >&2; exit 1; }
TMP="$(mktemp -d "${TMPDIR:-/tmp}/kavira-social.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

shoot() { # html scale w h out
  "$CHROME" --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor="$2" \
    --window-size="$3,$4" --screenshot="$TMP/shot.png" "file://$1" >/dev/null 2>&1
  cp "$TMP/shot.png" "$5"
}
jpeg() { sips -s format jpeg -s formatOptions 90 "$1" --out "$2" >/dev/null; }

shoot "$HERE/kavira-hero-dark.html" 2 1200 630 "$TMP/hero.png"
jpeg "$TMP/hero.png" "$HERE/kavira-hero-dark.jpg"
shoot "$HERE/kavira-social-card.html" 1 1600 900 "$TMP/card.png"
jpeg "$TMP/card.png" "$HERE/kavira-social-card.jpg"

# apple-touch-icon: the SVG mark on a 180px canvas
cat > "$TMP/icon.html" <<HTML
<body style="margin:0;background:#c2410c"><img src="file://$ROOT/internal/api/static/zyvor-mark.svg" width="180" height="180" style="display:block"></body>
HTML
shoot "$TMP/icon.html" 1 180 180 "$ROOT/internal/api/static/apple-touch-icon.png"
echo "wrote docs/social/kavira-hero-dark.jpg, kavira-social-card.jpg and the apple-touch-icon"
