# arehman.dev

Personal website + blog. A single static Go binary: markdown posts in the repo
are rendered at startup and served over HTTPS. Deployed to Fly.io, DNS on Porkbun.

## Stack

- **Go** (stdlib `net/http` routing) — one static binary, assets embedded
- **[templ](https://templ.guide)** — type-safe HTML components
- **[goldmark](https://github.com/yuin/goldmark)** — markdown (GFM, footnotes, heading anchors)
- **[chroma](https://github.com/alecthomas/chroma)** — class-based syntax highlighting (light + dark)
- **Fly.io** for hosting, **Porkbun** for DNS

## Layout

```
content/posts/*.md   blog posts (frontmatter + markdown)
content/pages/*.md   standalone pages (about, cv, ...)
internal/content     loads + renders markdown into an in-memory store
internal/web         HTTP routes
view/*.templ         HTML components
static/              css + js (embedded)
tools/genchroma      generates static/css/chroma.css
tools/dns            manages Porkbun DNS records
```

## Develop

Required CLI tools:

```sh
brew install go flyctl
```

The repository pins `templ` as a Go tool dependency, so no global installation
is needed. Go downloads it once when the module cache is empty, then reuses the
local build cache. Vulnerability scanning runs only in GitHub CI, so normal
local work does not download or run a separate scanner. Automatic Go toolchain
downloads are disabled by the Makefile. Docker is optional because Fly uses a
remote builder.

```sh
make generate             # regenerate all committed generated assets
make check                # formatting, module, test, and vet checks
make dev                  # serve from disk (includes drafts), :8080
```

The first run on a fresh machine may download declared Go modules. Subsequent
runs use Go's module and build caches. Project commands never use `@latest` or
install an unversioned executable.

`DEV=1` reads `content/` and `static/` from disk and includes drafts. Without
it, both are served from the embedded copies and drafts are hidden.

## Write a post

Add `content/posts/my-post.md`:

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

Body in **markdown**.
```

Markdown images automatically receive lazy-loading and asynchronous decoding
hints. Keep SVG figures self-contained (no remote fonts or images) and include
intrinsic `width`, `height`, and `viewBox` attributes.

## Deploy (Fly.io)

```sh
fly launch --no-deploy   # first time: creates the app from fly.toml
fly deploy               # builds via Dockerfile on Fly's remote builder
```

Generated templ files, Chroma CSS, and the social image are committed;
regenerate them before deploying after source changes. The Dockerfile builds a
static binary into a distroless image. No local Docker installation is required
because Fly builds remotely.

## Maintenance

CI runs formatting, generated-file, module, race-test, vet, and vulnerability
checks for every pull request. Dependabot checks Go modules, GitHub Actions, and
Docker images weekly. Review dependency updates as normal code changes: read
release notes, run `go mod tidy` and `make generate check`, then commit
`go.mod`, `go.sum`, and regenerated assets together.

## Custom domain + TLS

`.dev` is HSTS-preloaded, so HTTPS is mandatory — the site is unreachable until
the cert is issued. Fly provisions and auto-renews Let's Encrypt certs.

```sh
fly ips allocate-v4 --shared   # free shared IPv4
fly ips allocate-v6            # free IPv6
fly ips list                   # note the addresses
fly certs add arehman.dev
fly certs add www.arehman.dev

# Point Porkbun at Fly (uses PORKBUN_API_KEY / PORKBUN_SECRET_API_KEY from env):
go run ./tools/dns plan  -v4 <IPv4> -v6 <IPv6> -www <app>.fly.dev
go run ./tools/dns apply -v4 <IPv4> -v6 <IPv6> -www <app>.fly.dev

fly certs check arehman.dev    # repeat until issued
```
