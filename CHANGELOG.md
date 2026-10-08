# Changelog

## Unreleased

- **Site and live demo on GitHub Pages** (`https://zyvorai.github.io/kavira/`). A landing page with an animated compile pipeline, a verdict explorer, theme-aware screenshots, and copy buttons; and `/demo/`, the *real* console running without a server on API responses that `scripts/build-site.sh` records from a throwaway local `kavira serve`, so the demo cannot drift from the API. `scripts/site-check.cjs` drives both under the real `/kavira/` sub-path in CI.
- Console: toasts no longer stretch to the width of the longest message.
- **Console rebuilt on the Zyvor UX contract.** Hash routes (`#/incidents/<id>/<tab>`) so every view is linkable and survives reload; real links with `aria-current`; a selected-incident chip; search, verdict filter, and sortable incident table; evidence now shows the captured bundle and measurements; experiments explain the verdict; repairs add Copy and download of `regression_test.json` or the whole package; a "Compile your own bundle" dialog; an audit view that verifies the hash chain; empty, loading, and error states everywhere; keyboard shortcuts (`g o/i/e/x/r/a`, `/`, `?`). A Playwright suite covers every route in light, dark, and 390px.
- **Login redesigned.** No prefilled credentials, no password in the error text, distinct "wrong password", "server unreachable", and "session expired" messages. The demo-gate hint shows only while the gate is on.
- **Theme.** Tokens shared with Netra, `prefers-color-scheme` honored, no flash of the wrong theme (`/theme.js` runs before first paint), proper focus rings, 44px targets, reduced motion.
- **Security.** Fixed stored XSS: every server string reaching the DOM is now escaped by default. Constant-time credential check, strict CSP and security headers, `Secure` cookie option, server timeouts and graceful shutdown, JSON-only login/compile, 413 on oversize bundles, sorted incidents, a warning when the default password is exposed beyond loopback.
- **HTTPS.** `kavira serve -tls-cert/-tls-key` and `-tls-self-signed DIR [-tls-host …]` (generated once, reused, regenerated when the host changes or the certificate nears expiry). Serving TLS forces a `Secure` session cookie; plain HTTP beyond loopback prints a warning. `scripts/deploy-remote.sh` deploys HTTPS by default, sends rsync deltas, and takes `--demo` (keep admin / Admin@321 on a test host) and `--http`.
- **API.** `GET /api/v1/session` (quiet auth probe), `demo_gate` in health, fixtures now describe class and symptom, `chain_ok` in audit, JSON errors.
- **Project.** README with hero, screenshots, demo, and cards; social images and favicon; landing site; release, CodeQL, and Dependabot workflows; Docker healthcheck; kustomize and compose manifests; `scripts/deploy-remote.sh`; full Apache-2.0 text; SECURITY, CONTRIBUTING, AGENTS.

## 0.1.0

- First release: three incident classes, CLI (`compile`, `test`, `serve`), embedded console, hash-chained audit log.
