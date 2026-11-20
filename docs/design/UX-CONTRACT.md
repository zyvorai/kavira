# KAVIRA UX contract

The KAVIRA console follows the same Apple-style contract as Netra
(`../netra/docs/design/APPLE-UX-CONTRACT.md`), so Zyvor products feel like one
family. KAVIRA keeps its own identity: **execution is the verdict**. The UI
never states more than the runner measured.

Tokens live in `internal/api/static/styles.css` (`:root` = light, the default
until the OS or the user says otherwise; `[data-theme="dark"]` = dark). The
landing site (`site/`) copies the same values.

## Surface tiers

| Tier | KAVIRA pages | Density | Layout |
|---|---|---|---|
| **Story** | Login, Overview | Very low | One eyebrow, one headline, one lede, one CTA. A hairline `.metric-band` instead of a tile grid |
| **Browse** | Incidents, Audit | Medium | `.page-hero` + `.toolbar` + `.table-wrap` (hairline table) |
| **Work** | Evidence, Experiments, Repairs | High | `.card` panels, tokens only |

## Tokens

| Token | Light | Dark |
|---|---|---|
| `--bg-page` | `#ffffff` | `#000000` |
| `--surface-1` (card) | `#ffffff` + hairline | `#1d1d1f` |
| `--text-primary` | `#1d1d1f` | `#f5f5f7` |
| `--text-secondary` | `#6e6e73` | `#a1a1a6` |
| `--apple-blue` (CTA, focus) | `#0071e3` | `#0071e3` |
| `--apple-link` | `#0066cc` | `#2997ff` |
| `--zyvor-text` (brand accent as text) | `#c2410c` | `#ff7a1a` |

Status colors come as `--success-*`, `--warning-*`, `--danger-*`, `--info-*`
(`-bg`, `-text`). The bright `--accent-*` and `--danger` values are fills and
strokes; text always uses a `-text` token that reaches 4.5:1 on its background.

## Type scale

`--fs-body` 15px, `--fs-lede` 16px, `--fs-h3` 17px, `--fs-h2` and `--fs-h1`
fluid via `clamp()`, `--fs-figure` for metrics. Do not add raw `px` font sizes
above 17px.

## Laws

1. **Elevation runs up.** Dark: page `#000` → card `#1d1d1f` → popover lighter. Light: white page, white card with a hairline, popover with a soft shadow.
2. **Color is deviation.** Nominal values are graphite. Blue means intent (CTA, link, focus). Green is a verdict that held. Amber means not reproduced or inconclusive. Red means the repair failed or the class is unsupported.
3. **One primary action per view**, the `.primary` blue pill. Everything else is neutral or ghost.
4. **No hard-coded hex or rgba outside the token blocks.** Define a token first.
5. **Never overclaim.** Copy says "reconstructed", not "replayed". A missing repair package is shown as an honest state, not an error.
6. **Targets are at least 44px** (`--hit-min`) on touch; focus is always visible (`--focus-ring`); motion respects `prefers-reduced-motion`.

## Theming

`/theme.js` is a blocking script in `<head>` so the first paint is already
correct. Order: stored choice (`kavira-theme`) → OS `prefers-color-scheme` →
light. It sets `data-theme` on `<html>` and updates `meta[name=theme-color]`.
It is an external file so the CSP can stay `script-src 'self'`.

## Load-bearing markup (tests depend on it)

- Login heading `Sign in.` and the `role="alert"` error.
- Theme toggle aria-labels `Switch to dark mode` / `Switch to light mode`, with `aria-pressed`.
- `data-theme` on `<html>`.

## Author checklist

1. Pick the tier first.
2. Story: one composition, no card grid.
3. Browse: toolbar + table wrap; search uses `.input-field`.
4. Empty lists get a title, a sentence, and a next action (`.list-empty`); loading shows a skeleton, not blank space.
5. Check light and dark at 1440px and 390px; no horizontal scroll at 390px.
6. Keyboard-only pass: every control reachable, focus ring visible.
