# Deploying KAVIRA

KAVIRA is one static binary with the console embedded. State (incidents, audit log) is in memory, so run one replica and expect a restart to clear it.

**Always set `KAVIRA_ADMIN_PASSWORD` and `KAVIRA_SESSION_KEY`**, and serve over TLS: either `kavira serve -tls-cert c.pem -tls-key k.pem`, or `-tls-self-signed DIR -tls-host <ip-or-name>` for a lab (generated once, reused, regenerated only when the host changes or it nears expiry), or a TLS proxy in front (then set `KAVIRA_COOKIE_SECURE=1`). See [SECURITY.md](../SECURITY.md).

## SSH to a Linux host (no Docker, no root)

```bash
./scripts/deploy-remote.sh user@host            # or: ./scripts/deploy-remote.sh host user
KAVIRA_REMOTE_PORT=18090 ./scripts/deploy-remote.sh user@host
./scripts/deploy-remote.sh user@host --dry-run | --verify-only | --status
./scripts/deploy-remote.sh host user --demo     # lab host: keep admin / Admin@321, like Netra
./scripts/deploy-remote.sh host user --http     # plain HTTP (default is HTTPS)
```

It cross-compiles, sends a compressed rsync delta of the binary to `~/.kavira` (slow uplinks are common), installs a systemd **user** service, enables linger, and serves **HTTPS with a self-signed certificate** that covers the host (kept in `~/.kavira/tls`, so the browser's one-time "accept the risk" sticks). It then waits for `/api/v1/health` and checks: unauthenticated API returns 401, the session cookie is `Secure`, and the demo gate is off (or on, with `--demo`, in which case it also signs in as `admin` / `Admin@321`). It refuses when the disk is 95% full, warns at 80%, and stays out of `~/.deployments`.

Without `--demo` the admin password is random, created once in `~/.kavira/env` (mode 600), and never printed: read it with `ssh user@host 'grep ADMIN ~/.kavira/env'`. `--demo` leaves it unset, so the documented demo login works and **anyone who can reach the port can sign in**. Use it for throwaway test hosts only.

## Docker

```bash
docker run -d --name kavira -p 127.0.0.1:8080:8080 \
  -e KAVIRA_ADMIN_PASSWORD='…' -e KAVIRA_SESSION_KEY="$(openssl rand -hex 32)" \
  ghcr.io/zyvorai/kavira:0.1.0
```

Or `docker compose -f deploy/docker-compose.yml up -d` (fails fast if the secrets are missing). The image is distroless, runs as non-root, and has a `HEALTHCHECK` (`kavira healthcheck`).

## Kubernetes

```bash
kubectl apply -f deploy/namespace.yaml
kubectl -n kavira create secret generic kavira-auth \
  --from-literal=admin-password="$(openssl rand -base64 24)" \
  --from-literal=session-key="$(openssl rand -hex 32)"
kubectl apply -k deploy/
kubectl -n kavira port-forward svc/kavira 8080:80
```

Use a tagged release, not `main`.

## Verify a release image

Release images are built by `.github/workflows/release.yml`, carry an SBOM and provenance, and are signed keyless with cosign:

```bash
cosign verify ghcr.io/zyvorai/kavira:0.1.0 \
  --certificate-identity https://github.com/zyvorai/kavira/.github/workflows/release.yml@refs/tags/v0.1.0 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## Release checklist

1. Bump `version` in `cmd/kavira/main.go`, move `## Unreleased` in `CHANGELOG.md` to `## X.Y.Z - date`, update the image tag in `deploy/`.
2. `./scripts/check-version-sync.sh` must pass.
3. Tag `vX.Y.Z` and push. `release.yml` verifies the tag against the code, builds multi-arch images, signs them.
4. Enable Pages once: Settings → Pages → Source: GitHub Actions. `pages.yml` publishes `site/` on every push to `main`.
