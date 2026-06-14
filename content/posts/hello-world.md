---
title: "Hello, World"
date: 2026-06-14
slug: hello-world
description: "The first post — a quick tour of what this site is built on."
tags: [go, meta]
draft: false
summary: "A placeholder first post to prove the pipeline works end to end."
---

Welcome. This is a placeholder post that exists to prove the whole pipeline
works: markdown in the repo, rendered to HTML at startup, served over HTTPS
on a custom domain.

## What this is built on

This site is a single static Go binary. Posts like this one are plain
markdown files committed to the repository under `content/posts/`. At startup
they're parsed, rendered, and indexed in memory — no database, no CMS.

- **Go** with the standard library router
- **templ** for type-safe HTML components
- **goldmark** for markdown rendering
- Deployed to **Fly.io**, DNS on **Porkbun**

## A little code

Because every developer blog needs a syntax-highlighted snippet:

```go
package main

import "fmt"

func main() {
	fmt.Println("Hello, world")
}
```

That's it for now. This post can be deleted once real content lands.
