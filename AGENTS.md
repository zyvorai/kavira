# AGENTS.md

Guidance for AI coding agents and humans working in this repository.

## What KAVIRA is

A Go CLI and embedded web console that compiles captured incident evidence (container OOM, network timeout, configuration regression) into an isolated reproducer, one-factor experiments, and a repair package. Single static binary, zero third-party dependencies.

## Hard boundaries

- **Execution decides.** A hypothesis is never a verdict. Verdicts come from the runner (`repair_held`, `repair_failed`, `not_reproduced`, `unsupported`). See `internal/compiler`.
- **Never claim exact replay.** Copy says "reconstructed", not "replayed". `exact_replay` stays `false` everywhere.
- **No repair package unless the repair held.** Do not invent one for a symptom that did not reproduce.
- **No new Go dependencies.** `go.mod` has none. Keep it that way. The console is vanilla ES modules with no build step.
- **Escape by default.** Console views render through the `h` tagged template in `static/js/ui.js`, which escapes every interpolation. Use `raw()` only for markup you built yourself. Never concatenate server strings into HTML.
- **The demo gate must stay visible.** Do not make `Admin@321` work silently, and never enable it on a remote deploy.
- **Do not apply a patch to production from a KAVIRA result alone.** Say so wherever a patch is shown.

## Layout

| Path | What |
| --- | --- |
| `cmd/kavira` | CLI: `compile`, `test`, `serve`, `version` |
| `internal/{bundle,hypotheses,runner,compiler}` | The compile pipeline |
| `internal/api` | HTTP server, auth, embedded console (`static/`) and fixtures (`fixtures/`, must equal `examples/`) |
| `docs/design/UX-CONTRACT.md` | Console design rules |
| `docs/social`, `docs/ux` | Social images, README cards, screenshots, demo GIF (generated) |
| `site/` | Static landing page, published by `pages.yml` |
| `deploy/`, `scripts/deploy-remote.sh` | Compose, kustomize, and the SSH deploy |

## Validation before a PR

```bash
make check                    # gofmt, vet, race tests, build
make e2e                      # every route, light/dark/390px, XSS, auth, shortcuts
./scripts/check-version-sync.sh
```

| Command | CI job |
| --- | --- |
| `make check` | `ci.yml` / test |
| `make e2e` | `ci.yml` / e2e |
| `./scripts/check-version-sync.sh` | `ci.yml` / test, `release.yml` / verify |
| `docker build .` | `ci.yml` / docker |

When you change the console, regenerate assets with `make shots demo` and `./docs/social/build.sh`. `scripts/e2e.cjs` finds Playwright via a local install or `KAVIRA_PW_PATH`.
