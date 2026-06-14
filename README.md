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

```sh
templ generate            # regenerate view/*_templ.go after editing .templ
go run ./tools/genchroma  # regenerate syntax-highlighting CSS
DEV=1 go run .            # serve from disk (includes drafts), :8080
```

`DEV=1` reads `content/` and `static/` from disk and includes drafts. Without
it, both are served from the embedded copies and drafts are hidden.

## Write a post

Add `content/posts/my-post.md`:

```md
---
title: "My Post"
date: 2026-06-14
slug: my-post        # optional; defaults to filename
description: "Short summary for SEO/OG."
tags: [go, web]
draft: false
---

Body in **markdown**.
```

## Deploy (Fly.io)

```sh
fly launch --no-deploy   # first time: creates the app from fly.toml
fly deploy               # builds via Dockerfile on Fly's remote builder
```

The Dockerfile runs `templ generate` + `genchroma` then builds a static binary
into a distroless image. No local Docker required — Fly builds remotely.

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
