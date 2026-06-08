---
name: blogwatcher-cli
description: Monitor blogs and RSS/Atom feeds for updates using the blogwatcher-cli tool.
homepage: https://github.com/JulienTant/blogwatcher-cli
metadata: {"clawdbot":{"emoji":"📰","requires":{"bins":["blogwatcher-cli"]},"install":[{"id":"go","kind":"go","module":"github.com/JulienTant/blogwatcher-cli/cmd/blogwatcher-cli@latest","bins":["blogwatcher-cli"],"label":"Install blogwatcher-cli (go)"},{"id":"docker","kind":"docker","image":"ghcr.io/julientant/blogwatcher-cli:latest","bins":["blogwatcher-cli"],"label":"Run via Docker"}]}}
---

# blogwatcher-cli

Track blog and RSS/Atom feed updates with the `blogwatcher-cli` tool. Supports automatic feed discovery, HTML scraping fallback, and read/unread article management.

## Install

- Go: `go install github.com/JulienTant/blogwatcher-cli/cmd/blogwatcher-cli@latest`
- Docker: `docker run --rm -v blogwatcher-cli:/data ghcr.io/julientant/blogwatcher-cli`
- Binary (Linux amd64): `curl -sL https://github.com/JulienTant/blogwatcher-cli/releases/latest/download/blogwatcher-cli_linux_amd64.tar.gz | tar xz -C /usr/local/bin blogwatcher-cli`
- Binary (Linux arm64): `curl -sL https://github.com/JulienTant/blogwatcher-cli/releases/latest/download/blogwatcher-cli_linux_arm64.tar.gz | tar xz -C /usr/local/bin blogwatcher-cli`
- Binary (macOS Apple Silicon): `curl -sL https://github.com/JulienTant/blogwatcher-cli/releases/latest/download/blogwatcher-cli_darwin_arm64.tar.gz | tar xz -C /usr/local/bin blogwatcher-cli`
- Binary (macOS Intel): `curl -sL https://github.com/JulienTant/blogwatcher-cli/releases/latest/download/blogwatcher-cli_darwin_amd64.tar.gz | tar xz -C /usr/local/bin blogwatcher-cli`
- All binaries: [GitHub Releases](https://github.com/JulienTant/blogwatcher-cli/releases)

## Quick start

- `blogwatcher-cli --help`

## Common commands

- Add a blog: `blogwatcher-cli add "My Blog" https://example.com`
- Add with explicit feed: `blogwatcher-cli add "My Blog" https://example.com --feed-url https://example.com/feed.xml`
- Add with scraping: `blogwatcher-cli add "My Blog" https://example.com --scrape-selector "article h2 a"`
- List blogs: `blogwatcher-cli blogs`
- Scan for updates: `blogwatcher-cli scan`
- Scan one blog: `blogwatcher-cli scan "My Blog"`
- List unread articles: `blogwatcher-cli articles`
- List all articles: `blogwatcher-cli articles --all`
- Filter by blog: `blogwatcher-cli articles --blog "My Blog"`
- Mark an article read: `blogwatcher-cli read 1`
- Mark an article unread: `blogwatcher-cli unread 1`
- Mark all articles read: `blogwatcher-cli read-all`
- Mark all read for a blog: `blogwatcher-cli read-all --blog "My Blog" --yes`
- Remove a blog: `blogwatcher-cli remove "My Blog" --yes`
- Import blogs from OPML: `blogwatcher-cli import subscriptions.opml`
- Filter by category: `blogwatcher-cli articles --category "Engineering"`
- Filter by date: `blogwatcher-cli articles --since 2024-01-01 --before 2024-02-01` (`--since` inclusive, `--before` exclusive, both `YYYY-MM-DD`)

## Environment variables

All flags can be set via environment variables with the `BLOGWATCHER_` prefix:

- `BLOGWATCHER_DB` - Path to SQLite database file
- `BLOGWATCHER_WORKERS` - Number of concurrent scan workers (default: 8)
- `BLOGWATCHER_SILENT` - Only output "scan done" when scanning
- `BLOGWATCHER_YES` - Skip confirmation prompts
- `BLOGWATCHER_CATEGORY` - Filter articles by category
- `BLOGWATCHER_SINCE` - Filter articles published on or after `YYYY-MM-DD`
- `BLOGWATCHER_BEFORE` - Filter articles published before `YYYY-MM-DD`
- `BLOGWATCHER_FORMAT` - Output format (`text` or `json`)

## Agent automation

When using this tool from an agent or script, prefer `--format json` for machine-readable output:

- `blogwatcher-cli add "My Blog" https://example.com --format json`
- `blogwatcher-cli blogs --format json`
- `blogwatcher-cli scan --format json`
- `blogwatcher-cli articles --format json`
- `blogwatcher-cli read 1 --format json`
- `blogwatcher-cli unread 1 --format json`
- `blogwatcher-cli read-all --yes --format json`
- `blogwatcher-cli remove "My Blog" --yes --format json`
- `blogwatcher-cli import subscriptions.opml --format json`

Commands that normally prompt for confirmation (`remove`, `read-all`) require `--yes` in JSON mode. Use text output only when the user explicitly wants terminal-style output.

## Example output

```
$ blogwatcher-cli blogs
Tracked blogs (1):

  xkcd
    URL: https://xkcd.com
    Feed: https://xkcd.com/atom.xml
    Last scanned: 2026-04-03 10:30
```

```
$ blogwatcher-cli scan
Scanning 1 blog(s)...

  xkcd
    Source: RSS | Found: 4 | New: 4

Found 4 new article(s) total!
```

```
$ blogwatcher-cli articles
Unread articles (2):

  [1] [new] Barrel - Part 13
       Blog: xkcd
       URL: https://xkcd.com/3095/
       Published: 2026-04-02
       Categories: Comics, Science

  [2] [new] Volcano Fact
       Blog: xkcd
       URL: https://xkcd.com/3094/
       Published: 2026-04-01
       Categories: Comics
```

## Notes

- Import blogs in bulk from OPML files exported by Feedly, Inoreader, NewsBlur, etc.
- Categories from RSS/Atom feeds are stored and can be used to filter articles.
- blogwatcher-cli auto-discovers RSS/Atom feeds from blog homepages when no `--feed-url` is provided.
- If RSS fails and `--scrape-selector` is configured, it falls back to HTML scraping.
- Database is stored at `~/.blogwatcher-cli/blogwatcher-cli.db` by default (override with `--db` or `BLOGWATCHER_DB`).
- Use `blogwatcher-cli <command> --help` to discover all flags and options.
