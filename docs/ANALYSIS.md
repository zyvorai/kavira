# Analyses: repeat, minimize, postmortem, export

Four commands and matching API routes that sit on top of `kavira compile`. They all re-execute the reconstruction. None of them creates a repair package, changes a verdict, or writes the audit log. Exact replay is never claimed (`exact_replay` is `false` in every JSON they emit), and none of them is a reason to apply a patch to production.

| CLI | API (GET, authenticated) | Console |
| --- | --- | --- |
| `kavira repeat -f B -n 20` | `/api/v1/incidents/{id}/confidence?n=20` | Incident, Analyze |
| `kavira minimize -f B [-o minimal.json]` | `/api/v1/incidents/{id}/minimize` | Incident, Analyze |
| `kavira postmortem -f B [-o postmortem.md]` | `/api/v1/incidents/{id}/postmortem.md` | Incident, Analyze |
| `kavira export -f B -o repairs` | `/api/v1/incidents/{id}/regression.zip` | Incident, Analyze |

The API runs one `confidence` or `minimize` at a time (a second request gets `429`) and stops at a 15 second budget.

## Confidence: `kavira repeat`

Compiles the bundle up to `n` times (1 to 100, default 20) and reports:

- `reproduced`: runs where the symptom reproduced, with the rate and a 95% Wilson score interval.
- `repair_held`: among reproduced runs, how many repairs held, with its own interval.
- `reproduction`: `reproduced` (every run), `flaky` (some), `not_reproduced` (none, **and** at least 20 runs), or `inconclusive` (none, fewer than 20 runs).
- `repair`: `held_every_run`, `mixed`, `not_demonstrated`, or `none`.

Twenty clean runs put the 95% Wilson upper bound on the reproduction rate near 16%, which is why that is the floor for `not_reproduced`. There is deliberately no seed flag: the runners have no random source (OOM and disk are arithmetic, network loss is selected, config is clock-bounded), so a seed would pretend to control wall-clock timing. Exit code is 1 for `flaky` and `inconclusive`.

## Minimize: `kavira minimize`

Delta debugging (ddmin) with the runner as the oracle:

| Class | Reduced set |
| --- | --- |
| `config_regression` | keys that differ between `previous` and `current` |
| `network_timeout` | non-zero factors among `delay_ms`, `loss_percent`, `policy_deny`, `jitter_ms`, `bandwidth_bps`, `reset_after_bytes`, `slow_close_ms` (`timeout_ms` and `response_bytes` are thresholds or sizes and stay) |
| `dns_failure` | non-zero `latency_ms` and `nxdomain_percent` |
| `certificate_expiry` | the probe hours (which handshakes are needed to see an expired certificate) |
| `connection_pool_exhaustion`, `retry_storm` | the clients (the number of clients that still fails) |
| `container_oom`, `disk_pressure` | the workload's requests |

The output names what remains (`minimal`), what went away (`removed`), the number of oracle runs, `one_minimal` (the search finished inside the budget) and `verified` (the minimal input failed again when re-executed). `-o` writes a bundle holding only the minimal input; compile it for a verdict.

What it means and does not mean: **1-minimal** means removing any single remaining element stopped the failure in this reconstruction. It is not a global minimum, and it shows what is *sufficient* here, not what caused the production outage. If the full input does not fail, or the previous config already fails, it says so and stops (exit 1): there is nothing to minimize.

## Postmortem draft: `kavira postmortem`

A blameless Markdown draft with Summary, Impact, Timeline, Root cause, Mitigation and repair, Not established, Action items and Lessons learned. Every statement carries its provenance:

- **Demonstrated**: the runner executed it (verdict, experiments, measurements, a held repair).
- **Captured**: supplied in the bundle and not verified (impact numbers, notes, the optional `timeline`).
- **Hypothesis**: a proposed explanation, always labelled so.

A `not_reproduced` incident gets a draft that states no root cause and no repair. The bundle may carry an optional `timeline` of up to 100 `{at, event}` entries, kept in the order supplied. All bundle text is escaped for Markdown (no raw HTML, no table or link injection).

## Regression export: `kavira export`

Writes `repairs/<id>/regression/` with `bundle.json`, `before_test.json`, `regression_test.json` and `regress.sh`. The script needs only a POSIX shell and the kavira binary (`KAVIRA=/path/to/kavira ./regress.sh`): it fails if the captured condition stops failing, and fails if the repaired condition does not hold. A missing binary is exit 2, never a pass. Only available when the verdict is `repair_held`.

## Hostile input

Bundles are untrusted, and the runner really allocates, sleeps and dials. `internal/bundle` bounds every quantity before anything runs: raw size 1 MiB, text fields 4 KiB, ids 128 bytes, 100 timeline events, 256 config keys, 1024 requests, 512 MiB of total workload allocation, network attempts, delays and timeouts (and their product) capped at 60 s. The new classes have their own bounds (see `CLASSES.md`). No archive format is accepted anywhere, so there is no zip or tar bomb surface; the API only emits zips. Download names are sanitized, ids are checked as directory names before any file is written, and `kavira test` re-validates repaired network conditions read from a regression file.

The timing-dependent classes (DNS, pool, retry storm) make a noisy oracle. `kavira repeat` first; if the reconstruction is `flaky`, a minimized result is only as good as one sample. The retry-storm minimizer costs about a second per oracle run and normally stops at the 20 s budget with `one_minimal: false`.

## Examples

`examples/analysis/` holds `network-toxics.json` (bandwidth cap plus three harmless factors, minimizes to the cap), `config-noisy.json` (six changed keys, minimizes to two) and `oom-burst.json` (24 requests, minimizes to 4), both with a timeline. They live in a subdirectory so `kavira compile -d examples` and the console fixtures stay at the four canonical classes.

```bash
kavira repeat -f examples/analysis/oom-burst.json -n 20
kavira minimize -f examples/analysis/config-noisy.json -o /tmp/minimal.json
kavira postmortem -f examples/analysis/oom-burst.json -o /tmp/postmortem.md
kavira export -f examples/analysis/oom-burst.json -o /tmp/repairs && KAVIRA=bin/kavira sh /tmp/repairs/inc-oom-burst-001/regression/regress.sh
```
