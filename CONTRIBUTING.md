# Contributing

1. **Open an issue first** for anything beyond a typo, so we agree on the shape.
2. **Respect the boundaries** in [AGENTS.md](AGENTS.md): execution decides, exact replay is never claimed, no new dependencies.
3. **Add tests.** Server behavior goes in `internal/api/server_test.go`; console behavior goes in `scripts/e2e.cjs`.
4. **Run the same checks CI runs:**
   ```bash
   make check      # gofmt, go vet, go test -race, build
   make e2e        # browser suite (needs playwright-core)
   ```
5. **Console changes** follow [docs/design/UX-CONTRACT.md](docs/design/UX-CONTRACT.md): tokens only, light and dark, 390px with no horizontal scroll. Run `make shots` and look at the result.
6. Add a line under `## Unreleased` in [CHANGELOG.md](CHANGELOG.md).

By contributing you agree your work is licensed under Apache-2.0.
