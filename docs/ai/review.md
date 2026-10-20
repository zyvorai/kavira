# AI review (kavira)

_Generated 2026-10-20 by claude-opus-5-5 from a snapshot of tracked sources; verify before acting._

# Code review: kavira

## Summary

KAVIRA is a dependency-free Go CLI and embedded vanilla-JS web console. It takes an "evidence bundle" for one of three incident classes (container OOM, network timeout, config regression), runs bounded in-process experiments, and emits a verdict. When the repair holds, it also writes a repair package with a regression test.

The repo also includes:
- a GitHub Pages landing site and a static demo built from recorded API responses;
- an SSH deploy script;
- Docker, Compose and kustomize manifests;
- CI, release (cosign, SBOM) and CodeQL workflows.

## Bugs and correctness risks

**Runner crashes and resource exhaustion from bundle values (`internal/runner/runner.go`)**

- **`RunConfig` panics on a non-positive `batch_size`.**
  - `"batch_size": "0"` gives `need == 0`, so `buf[0] = 1` indexes an empty slice and panics.
  - `"-1"` gives a negative `need`, so `make([]byte, need)` panics.
  - `bundle.Validate` never inspects config values.
  - In `kavira compile` this crashes the process. Over HTTP, `net/http` recovers per request but drops the connection.
- **Unbounded allocations and sleeps.** Nothing caps the numbers a bundle controls:
  - OOM: `Workload.Requests`, `BytesPerRequest` and `MemoryLimitBytes`.
  - Network: `Network.Attempts`, `DelayMs` and `TimeoutMs`.
  - Config: `batch_size` and `memory_limit_mib`.

  In `compileOOM`, `raised = need + need/5` with `need = BytesPerRequest * Requests` (which can overflow `int64`). It then calls `RunOOM(..., raised, true)`, which actually allocates and holds the full total. `RunConfig` sleeps `batch*2` ms. A single ≤1 MiB bundle can therefore exhaust memory or pin a goroutine for hours. This contradicts the "bounded, in-process workloads" claim in `SECURITY.md`.
- **`RunNetwork` can block forever.**
  - The accept goroutine expects exactly `attempts` connections, and `<-done` waits for it.
  - If any `dialer.Dial` fails, that connection never arrives. The goroutine blocks in `Accept`, and `ln.Close()` is deferred until after `<-done`, so the run deadlocks.
  - Local dials rarely fail, so this is rare but unrecoverable.
  - Connections are also handled serially, so `attempts × delay_ms` adds up linearly.

**Verdicts and outputs (`internal/compiler`, site and docs)**

- **The `unsupported` verdict is unreachable.**
  - `bundle.Validate` rejects unknown classes, so `Compile`'s `default:` branch never runs.
  - The CLI exits 1 with `unsupported class "…"`, and the API returns 400.
  - Yet the site (`site/index.html` "verdict unsupported: no package written"), the README, and the UI pills all describe it as a verdict.
- **`reproduce()` is wrong for user bundles** (`internal/compiler/compiler.go`). It always prints `kavira compile -f examples/<class-stem>.json`, regardless of which bundle was actually compiled.
- **Timing-based verdicts are flaky.** Results depend on wall-clock timing under `-race` on shared CI runners:
  - OOM: `raisedRun.P99Ms <= bound`.
  - Config: `elapsed > timeoutMs` and the bound `prev.P99Ms*2 … +5`.
- **The config "repair" reruns the previous snapshot unchanged** (`compileConfig`). `reverted` is identical to `prev`, so it is not a one-factor change. The patch is the whole previous config, not just the drifted keys.
- **Non-bool `hold` silently becomes false** in `TestRepair`. `hold, _ = v.(bool)` turns a malformed `"hold"` into `false` instead of an error.
- **The CLI exit code ignores the verdict.** `kavira compile` returns 0 for `not_reproduced` and `repair_failed`, so scripted use cannot tell the outcomes apart.
- **Hypothesis wording breaks the project's own rule.** `internal/hypotheses/hypotheses.go` says "Replay the bounded workload…", while `AGENTS.md` says copy must say "reconstructed".

**Server and console**

- **The audit hash omits the `At` timestamp** (`internal/api/server.go`, `store` and `chainOK`). Timestamps can change without breaking the chain. The chain is also built and verified by the same in-memory process, so "chain verified" proves little.
- **Malformed hash routes break rendering** (`static/js/router.js`). `decodeURIComponent` throws on input like `#/incidents/%E0`. `render()` calls `parse()` outside its `try`, so the page stays on the skeleton with an unhandled rejection.
- **`deploy-remote.sh` verify reports a Secure-cookie pass it never checked.**
  - Without `--demo`, the login with `Admin@321` gets a 401, so there is no `Set-Cookie`.
  - `[[ -z "$sc" ]]` then passes, and the script prints "tls: ok (cookie is Secure)".
  - Each verify also generates a failed login attempt.
- **The demo-gate "refusal" happens after the service is already live.** `SECURITY.md` says the deploy "refuses to finish" while the gate is on. In fact `verify` runs only after the service has been installed and started on `0.0.0.0`.

## Security concerns

- **The default session key allows cookie forgery even when a password is set.**
  - `KAVIRA_SESSION_KEY` falls back to the public constant `kavira-dev-session` (`server.go`).
  - A user who sets only `KAVIRA_ADMIN_PASSWORD` (as the README's "Set it to turn the demo gate off" implies) still accepts forged `admin|<exp>|<hmac>` cookies.
  - `/api/v1/health` reports only `demo_gate`, which tracks the password. CI and deploy verification check that flag, not the key.
  - Recommendation: refuse to bind beyond loopback with the default key, or derive a random per-process key.
- **Unauthenticated-to-authenticated DoS via `/api/v1/compile`.** Any session holder can exhaust memory or CPU with the unbounded runner inputs described above. Under the demo gate, "any session holder" means anyone who can reach the port.
- **No server-side session revocation.** Logout only clears the client cookie, and a captured token stays valid for 12h. Combined with no rate limiting or lockout (documented), brute force is unthrottled.
- **Unbounded in-memory store.** `s.reports` and `s.bundles` grow with every compile, up to 1 MiB each.
- **Possible login break under Kubernetes port-forward (uncertain).** `deploy/deployment.yaml` sets `KAVIRA_COOKIE_SECURE=1`, but the README tells users to `port-forward` over plain HTTP. Some browsers (Safari historically) may not keep `Secure` cookies on `http://localhost`, which would make login fail.
- **CSRF exposure is limited.** The JSON content-type requirement plus `SameSite=Lax` covers most cases. `/api/v1/logout` does not check content type, which allows a trivial CSRF logout.

## Test and CI gaps

**Missing tests**
- There are no unit tests in `internal/runner` or `internal/bundle`, and no fuzz test for `bundle.Parse` plus `Compile`. A fuzz test would have found the `batch_size` panic immediately.
- No test covers adversarial or huge numeric bundle values, or `repair_failed` paths in any class.
- `TestUnsupportedClass` tests the parser, not the `unsupported` verdict.

**Not reviewable from the files provided**
- `Makefile`, `scripts/e2e.cjs`, `scripts/site-check.cjs` and the `Dockerfile` were not included. I could not verify that `make check` matches CI, or what the e2e suite covers.

**CI configuration**
- **Action versions may not exist.** CI pins `actions/checkout@v7`, `setup-go@v7`, `setup-node@v6`, `upload-artifact@v6`, `docker/build-push-action@v7` and similar. Some may not exist yet; verify each, or pin by SHA.
- **Release checks are thin.** `release.yml` `verify` runs only the version sync and `go test`. It skips gofmt, vet, e2e and the docker smoke test, so a tag can ship without them.
- **Pages deploys without tests.** `pages.yml` deploys on every push to `main` without gating on CI.
- **Portability of the version check.** `scripts/check-version-sync.sh` uses `\b` in basic `grep`. That works with GNU grep but may not with BSD/macOS grep locally.
- **Brittle health assertion.** The CI and deploy checks grep for `"demo_gate": false`, which depends on the exact indentation from `writeJSON`.

## Documentation gaps

- **No bundle schema.** Field names, types, valid ranges, and which fields each class requires exist only in `bundle.go`.
- **CLI exit-code semantics** are undocumented.
- **The `unsupported` verdict** is documented as a CLI output that never happens.
- **The session-key risk is understated.** README and SECURITY should state plainly that an unset `KAVIRA_SESSION_KEY` is equivalent to no auth, even with a password set.
- **The deploy script header contradicts itself.** `scripts/deploy-remote.sh` says "cross-compile + scp … no rsync", but the script uses `rsync`. The header is also what `--help` prints.
- **macOS-only asset scripts.** The asset build scripts (`docs/social/build.sh`, `docs/ux/build-readme-cards.sh`) require Chrome on macOS plus `sips`. No Linux path is offered.
- **`docs/proposals/0001`** says "Name availability is still unchecked." That is stale for a published project.

## Top 5 recommended actions (ordered)

1. **Validate and cap every numeric bundle input** in `bundle.Validate`: requests, bytes per request, limits, attempts, delay and timeout, plus parsed config values such as `batch_size > 0` and a maximum `memory_limit_mib`. Also guard `RunConfig` against `need <= 0`. Add a `go test -fuzz` target for `Parse` plus `Compile`.
2. **Make the default session key unsafe-by-default.** Generate a random key per process when unset, or refuse non-loopback binds. Expose `default_key` in `/health` and assert it in CI and `deploy-remote.sh`.
3. **Fix `RunNetwork`'s potential deadlock.** Close the listener, or count the accept goroutine down on dial failure. Bound total runtime with a context or deadline.
4. **Fix false or vacuous checks.**
   - Make `deploy-remote.sh` actually verify the Secure flag in non-demo mode, for example with the real password read over SSH.
   - Either make `unsupported` a real verdict or remove it from docs and the site.
   - Make `reproduce()` reference the actual bundle.
   - Include `At` in the audit hash.
5. **Harden CI.**
   - Add runner, bundle and adversarial-input tests.
   - Gate `release.yml` and `pages.yml` on the full CI job.
   - Verify or SHA-pin the action versions.
   - Replace wall-clock bounds in tests with tolerant or injected clocks to avoid flaki
