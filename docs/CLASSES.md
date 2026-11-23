# Incident classes

KAVIRA reconstructs eight kinds of captured incident. Each is a compiler template plus a runner that executes on this machine, on loopback, without root and without any network beyond `127.0.0.1`. Execution decides the verdict, `exact_replay` is `false` everywhere, and nothing here is a reason to apply a patch to production.

| Class (`class`) | Section of the bundle | What executes | One-factor repair arms |
| --- | --- | --- | --- |
| `container_oom` | `evidence`, `workload` | Bounded allocation under the captured limit | raise limit, drop hold |
| `network_timeout` | `network` | Loopback listener with delay, jitter, loss, policy deny, bandwidth cap, reset after N bytes, slow close | the one factor that explains the failure (see below) |
| `config_regression` | `configs` | Previous and current config on one workload | revert |
| `disk_pressure` | `evidence`, `workload` | Bounded write workload under a quota | raise quota, drop hold |
| `dns_failure` | `dns` | UDP stub resolver on loopback, real DNS wire format, client with timeout, retries, cache | raise timeout, enable cache, fix names |
| `certificate_expiry` | `cert` | Throwaway CA and certificates, loopback TLS server, one real handshake per probe on a virtual clock | widen rotation window, shorter check interval, enable rotation |
| `connection_pool_exhaustion` | `pool` | Bounded pool of real loopback connections in front of a server with service time | fix the leak, raise pool size, raise acquire timeout |
| `retry_storm` | `retry` | Capacity-limited loopback server, clients that start together and retry with backoff | add jitter, jitter with a cap |

Examples: `examples/dns-failure.json`, `certificate-expiry.json`, `connection-pool.json`, `retry-storm.json` (also the console fixtures; `internal/api/fixtures/` must equal them), and `examples/analysis/network-toxics.json`.

Arms run in the order listed and each is recorded as an experiment. The first one that held (zero failures inside its latency bound) becomes the repair and later arms are not run, because the server seeds every fixture at start and these runners really sleep. If none holds the verdict is `repair_failed` and there is no repair package. If the original does not fail, the verdict is `not_reproduced`.

## How each failure is classically modelled, and what is simplified

**DNS.** A stub resolver answers a query with a response code and, for a hit, one A record. The reconstruction is the textbook client loop: send, wait `timeout_ms`, retry up to `retries`, optionally cache. Selected, not random: the stub numbers the queries it receives and answers after `latency_ms` from query `degraded_after` on (0 means from the first); the first `nxdomain_percent` of names answer NXDOMAIN. A cache stores answers and NXDOMAIN, so it removes load from a degraded resolver for names resolved before the degradation and cannot fix a name that does not exist. Not modelled: search-list expansion, EDNS, TCP fallback, truncation, DNSSEC, TTL expiry, the resolver's own recursion. Each attempt uses a new socket, so a late answer cannot satisfy a later attempt. Timing-dependent: which lookup finishes before its timeout when latency is close to `timeout_ms`.

**Certificate expiry.** A rotation job checks every `check_interval_hours` and reissues when no more than `rotation_window_hours` remain. If the window is smaller than the interval, a certificate can expire between two checks: that is the reconstructed failure. Hours are a virtual clock counted from the first issue. The CA and leaf certificates are generated in memory with ECDSA P-256 and never written to disk; each probe is a real TLS handshake on loopback whose verifier clock is set to the probe's hour, so the standard library decides validity. A certificate is valid up to and including its expiry hour. Not modelled: hostname mismatch, revocation, intermediates, clock skew between peers, TLS session reuse. Deterministic: the schedule and each verdict do not depend on the wall clock; only handshake latency does.

**Connection pool.** The standard model is a counting semaphore with an acquire timeout: a request that cannot get one of `pool_size` connections within `acquire_timeout_ms` fails. `leak_every` N makes every Nth request keep its connection forever, which drains the pool one slot at a time. Connections are real loopback TCP connections to a server that holds each request for `service_ms`. Not modelled: server-side connection limits, idle eviction, health checks, connection warm-up. Timing-dependent: a run whose queueing delay is close to `acquire_timeout_ms` is flaky, and with a leak the count of leaked connections is exact while which client loses them depends on goroutine scheduling.

**Retry storm.** A server serving at most `capacity` requests at once rejects the rest immediately. Clients start together and retry a rejected request after `min(cap, backoff_ms * 2^k)` (no cap when `backoff_cap_ms` is 0), reduced by up to `jitter_percent` of that delay. Without jitter every client retries at the same instants, so each wave admits at most `capacity` clients and the rest give up after `max_retries`. The jitter is a deterministic hash of client and attempt (splitmix64), not a random draw, so there is no seed. Not modelled: retry budgets, circuit breakers, server queues, request amplification across tiers. Timing-dependent: real sleeps; with jitter a client can land on either side of a service-time boundary, so the exactly-counted unjittered failure number is stable and the jittered runs are expected to pass with margin but are not proofs. `kavira repeat` is the right tool to see that.

**Network arms.** The Toxiproxy toxics that have a loopback equivalent without a proxy process: `jitter_ms` (0 to N ms added per attempt, deterministic hash), `bandwidth_bps` (the response is written in paced chunks), `reset_after_bytes` (the server writes N bytes then closes with a TCP reset, like `reset_peer`), `slow_close_ms` (the server holds the connection open after the response and a client that reads to EOF waits for it), and `response_bytes` (response size, default 2). All are optional and zero means off, so older bundles are unchanged. Repair: a reset is repaired by removing it; a path whose delay, jitter, transfer time and slow close add up to at least the timeout is repaired by raising the timeout to three times that plus 50 ms. Not built: `slicer`, `limit_data`, `timeout` as a separate toxic (the existing `timeout_ms` is the client side of it) and `packet_loss` at packet level (the existing `loss_percent` stays connection level).

## Bounds (hostile bundles)

Bundles are untrusted and these runners really sleep, dial and allocate, so every quantity is bounded before anything runs, and a repaired condition read back from a regression file is checked again.

| Class | Limits |
| --- | --- |
| DNS | lookups 1..200, names 1..lookups, retries 0..5, timeout and latency at most 60 s, `lookups * (retries+1) * timeout_ms <= 60 s` |
| Certificate | lifetime 1..87,600 h, 1..50 probes, window at most the lifetime, interval required when rotation is on; at most one certificate generated per probe |
| Pool | size 1..64, clients 1..64, requests per client 1..50, service at most 1 s, `requests * (acquire_timeout + service) <= 60 s` |
| Retry | clients and capacity 1..200, service 1..500 ms, backoff 1..5000 ms, 0..10 retries, worst-case sum of delays and service time at most 60 s |
| Network arms | response at most 1 MiB, bandwidth at most 1 GiB/s, `attempts * max(timeout, delay + jitter + transfer + slow_close) <= 60 s`; the server stops writing after twice the timeout, so a client that gave up cannot pin it |

## Rejected, and why

- **Goroutine and file-descriptor exhaustion.** Exhausting either for real means lowering `RLIMIT_NOFILE` or spawning until the scheduler struggles. Both are process-wide, so they would degrade `kavira serve` and every other request in it, and a hostile bundle could ask for it. A simulated counter would be a model, not an execution, which is what this tool refuses to call a verdict. The connection pool class covers the part that can be reconstructed safely (a bounded number of real connections).
- **A real DNS recursor, ACME, or an OCSP responder.** Needs network beyond loopback or persistent state.
- **Packet-level loss, reordering, slicing.** Need a raw socket or a kernel facility, so root.

## Using the analyses on the new classes

- `repeat` works for all eight. For the timing-dependent classes it is the way to see flakiness: a pool or retry run that sits near a boundary will show `flaky`.
- `minimize` reduces, with the runner as the oracle: DNS to the non-zero resolver factors (`latency_ms`, `nxdomain_percent`), certificate expiry to the probe hours that fail, pool and retry storm to the number of clients (a client list is the only element that can be removed without redefining the incident), network to the non-zero factors including the new arms. Each oracle run of the retry storm costs about a second, so the 20 s budget usually ends it before 1-minimality is proven and it says so.
- `postmortem` and `export` work from the report, so they need nothing class-specific; `regress.sh` is unchanged and still needs only a POSIX shell and the kavira binary.
