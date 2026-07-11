# AGENTS.md

Guidance for AI coding agents working in this repo. Human-oriented setup lives in `README.md`.

## Project

Personal website + blog for **arehman.dev**. A single static Go binary that renders
markdown from `content/` at startup and serves it. Deployed to Fly.io (app `arehman-dev`,
region `nrt`), DNS on Porkbun. Designed to grow beyond a blog (CV, custom pages) — keep the
content layer generic (Posts vs Pages), not blog-locked.

## Stack

Go (stdlib `net/http`) · [templ](https://templ.guide) views · [goldmark](https://github.com/yuin/goldmark)
markdown · [chroma](https://github.com/alecthomas/chroma) highlighting · `embed.FS` for content+assets ·
distroless Docker image on Fly.io.

## Layout

- `content/posts/*.md` — blog posts (frontmatter + markdown)
- `content/pages/*.md` — standalone pages (about, cv, …)
- `internal/content` — loads/renders markdown into an in-memory store
- `internal/web` — HTTP routing, security headers, static serving, `robots.txt` + `sitemap.xml`
- `view/*.templ` — HTML components (generated `*_templ.go` committed alongside)
- `view/seo.go` — `Head` struct + JSON-LD builders feeding the shared `Layout`
- `static/` — css + js (embedded); `static/css/chroma.css` is generated
- `tools/genchroma` — regenerates `static/css/chroma.css`
- `tools/genog` — regenerates `static/og/default.png` (the og:image social card)
- `tools/dns` — manages Porkbun DNS records

## Commands

```sh
make generate             # regenerate templ, Chroma CSS, and the OG image offline
make check                # formatting, module consistency, tests, and vet
make dev                  # run locally from disk (includes drafts), :8080
BASE_URL=https://arehman.dev make run   # override origin for canonical URLs + sitemap
flyctl deploy --remote-only --yes   # build + deploy via Fly remote builder
```

## Tool and network policy

- `templ` is the only local executable dependency and is pinned by a `tool`
  directive in `go.mod`. Go downloads its module once when the module cache is
  empty and reuses the module/build caches afterward.
- Vulnerability scanning is CI-only through a commit-pinned GitHub Action. Do
  not add a local `govulncheck` executable or make normal development depend on
  refreshing the online vulnerability database.
- Do not commit `vendor/` or platform-specific tool binaries. Do not use
  `@latest`, `go install`, or `curl` to fetch executables in project workflows.
  Any future Go tool must use a version-pinned `tool` directive and have a clear
  maintenance benefit.
- The Makefile sets `GOTOOLCHAIN=local`, so commands fail with a useful version
  error instead of automatically downloading another Go toolchain.

## Conventions & gotchas

- **Generated files are committed and the Docker build does NOT run codegen.** After editing
  `.templ` files or generators run `make generate`.
  Commit the resulting `view/*_templ.go`, `static/css/chroma.css`, and
  `static/og/default.png`, or the deployed build will be stale.
- The app must **listen on `0.0.0.0:$PORT`** (reads `PORT`, defaults 8080). Never bind localhost.
- Honor `SIGTERM` for graceful shutdown (already wired in `main.go`).
- Content is **author-trusted**: goldmark runs in safe mode (no `html.WithUnsafe`) and templ
  auto-escapes. If untrusted input is ever rendered, sanitize (e.g. bluemonday) before output.
- **JSON-LD is built in Go, not in templ text.** `view/seo.go` marshals each block with
  `encoding/json` (which escapes `<`/`>`/`&`, preventing a `</script>` breakout) and the layout
  emits it via `@templ.Raw`. Never interpolate JSON-LD through `{ … }` — templ entity-escapes it
  and breaks the JSON. The `<script type="application/ld+json">` block is inert data, so the
  strict `script-src 'self'` CSP does not block it (no nonce/hash needed).
- Keep the Docker `golang:1.26.x` builder pinned to a **patched** Go release; bump it when
  `govulncheck` flags stdlib advisories.

## Adding a post

Create `content/posts/<slug>.md` with frontmatter:

```md
---
title: "My Post"
date: 2026-06-14
updated: 2026-06-20 # optional; only after a substantial revision
slug: my-post        # optional; defaults to filename
description: "Short summary for SEO/OG."
image: /static/img/my-post.svg # optional; root-relative article image
tags: [go, web]
draft: false
---
Body in markdown.
```

## Deploy / DNS / TLS

- `flyctl deploy --remote-only --yes` builds via Fly's remote builder (no local Docker needed).
- Fly terminates TLS at the edge (`force_https = true`); the app speaks plain HTTP internally.
- **`.dev` is HSTS-preloaded — HTTPS is mandatory; the site is unreachable until the cert is issued.**
- DNS: apex `A`+`AAAA` → Fly IPs, `www` `CNAME` → `arehman-dev.fly.dev`, plus a `CAA` restricting
  issuance to `letsencrypt.org`. Manage via `go run ./tools/dns` (needs `PORKBUN_API_KEY` +
  `PORKBUN_SECRET_API_KEY` in env).

## SEO & indexing

The site emits the markup Google needs to crawl, canonicalize, and understand pages; the
rest is one-time Search Console setup.

**What the code does (per-page, in `view/seo.go` + `view/layout.templ`):**

- **Canonical + `og:url`** on every page (absolute, built from `BASE_URL`). 404s omit them.
- **Open Graph**: `og:title`, `og:description`, `og:type` (`article` for posts, `website`
  otherwise), `og:site_name`, `og:locale`. Posts add published/modified times and tags.
- **`og:image`**: a shared 1200×630 card (`static/og/default.png`, regenerate via `go run ./tools/genog`)
  with `og:image:width`/`height`/`alt`; Twitter card is `summary_large_image`.
- **JSON-LD**: `WebSite` + `Person` on the home page; `BlogPosting` (including an optional
  root-relative article `image` and meaningful `dateModified`) + `BreadcrumbList` on posts.
- **`/robots.txt`** (allows all, links the sitemap) and **`/sitemap.xml`** (home + every post with
  `lastmod` from `updated`, falling back to `date`, + every page), generated at request time.

`BASE_URL` (default `https://arehman.dev`, trailing slash trimmed) drives every absolute URL.
Per current Google guidance the sitemap deliberately omits `priority`/`changefreq` (ignored) and
`lastmod` reflects the real post date (never "now", or Google stops trusting it).

**One-time setup (not in code):**

1. Google Search Console → add a **Domain property** for `arehman.dev`; verify with the DNS **TXT**
   record Google provides (add it via `go run ./tools/dns`). Domain property covers apex + `www` + http/https.
2. Submit `https://arehman.dev/sitemap.xml` in the Sitemaps report.
3. Use URL Inspection → Request Indexing to nudge the home page + new posts. First indexing of a
   new domain realistically takes days–weeks; re-requesting does not speed it up.

The `og:image` is a single shared card; optional per-post `image` metadata feeds article JSON-LD.
`llms.txt`, the Indexing API (JobPosting/livestream
only), and `priority`/`changefreq` are intentionally **not** used.

## Constraints

- **Never commit secrets.** `.env` is git-ignored; only `.env.example` (empty) is tracked.
  Porkbun keys are used solely by `tools/dns` from the environment — never by the deployed binary.
- Don't introduce a database or CGO; keep it a single static binary.
