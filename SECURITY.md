# Security

## Reporting

Report vulnerabilities privately: use GitHub's **Report a vulnerability** (Security → Advisories) on `zyvorai/kavira`, or email the maintainers listed in the repository. Please do not open a public issue. We aim to acknowledge within three working days.

## Know before you deploy

- **The demo gate.** Until `KAVIRA_ADMIN_PASSWORD` is set, the console accepts `admin` / `Admin@321`. It exists so a fresh checkout works, and it is documented publicly, so treat it as no authentication at all. `kavira serve` prints a warning when it listens beyond loopback with the default password or session key, and `/api/v1/health` reports `"demo_gate": true`. `scripts/deploy-remote.sh` generates a random password and refuses to finish a deploy while the gate is on, unless you pass `--demo` to ask for it, as Netra's lab deploys do. Use `--demo` only on a throwaway test host: anyone who can reach the port can sign in.
- **Set both secrets** for anything other than your own laptop: `KAVIRA_ADMIN_PASSWORD` and `KAVIRA_SESSION_KEY` (for example `openssl rand -hex 32`).
- **Use TLS.** `kavira serve` terminates HTTPS itself with `-tls-cert`/`-tls-key`, or with `-tls-self-signed DIR` (a generated, reused self-signed certificate: fine for a lab, but browsers warn). When it serves TLS the session cookie is always `Secure`. Behind your own TLS proxy, set `KAVIRA_COOKIE_SECURE=1` instead. Plain HTTP beyond loopback prints a warning.
- **One user, in-memory state.** There is a single `admin` user and no rate limiting or lockout. Put it behind your own access control.
- **Bundles are untrusted input.** `/api/v1/compile` accepts arbitrary evidence bundles (≤ 1 MiB). The console escapes every value it renders, and a browser test feeds it a hostile bundle on every CI run. The runners execute bounded, in-process workloads only.

## Hardening already in place

Constant-time credential comparison, HMAC-signed `HttpOnly` `SameSite=Lax` cookies with a 12-hour lifetime, JSON-only login and compile, a strict Content-Security-Policy (`default-src 'self'`, no inline script), `X-Frame-Options: DENY`, `nosniff`, `no-referrer`, `no-store` on the API, server timeouts, and graceful shutdown.
