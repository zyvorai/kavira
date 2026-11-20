# Docs

| File | What |
| --- | --- |
| `design/UX-CONTRACT.md` | Console design rules, tokens, laws, checklist |
| `proposals/0001-kavira-incident-compiler.md` | Original proposal |
| `social/` | Hero (`kavira-hero-dark`), share card, and their HTML sources |
| `ux/` | Console screenshots, README cards, demo GIF |

## Regenerating assets

Everything in `social/` and `ux/` that is an image is generated. The HTML and scripts are the source of truth.

```bash
make shots                       # docs/ux/*.png: every page, light, dark, 390px (needs playwright-core)
make demo                        # docs/ux/kavira-demo.gif (needs playwright-core and ffmpeg)
./docs/social/build.sh           # hero, share card, apple-touch-icon (macOS: Chrome + sips)
./docs/ux/build-readme-cards.sh  # README cards
```

`scripts/e2e.cjs` and `scripts/demo.cjs` find Playwright through a local install or `KAVIRA_PW_PATH=<dir containing playwright-core>`.

GitHub's **social preview** is uploaded by hand: Settings → General → Social preview → `docs/social/kavira-hero-dark.jpg`. The `og:image` the console and the site serve points at the Pages copy of the same file.
