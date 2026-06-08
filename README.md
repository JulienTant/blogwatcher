# blogwatcher-cli

Fork of [Hyaxia/blogwatcher](https://github.com/Hyaxia/blogwatcher).

A Go CLI tool to track blog articles, detect new posts, and manage read/unread status. Supports both RSS/Atom feeds and HTML scraping as fallback.

## Features

-   **Dual Source Support** - Tries RSS feeds first, falls back to HTML scraping
-   **Automatic Feed Discovery** - Detects RSS/Atom URLs from blog homepages
-   **Read/Unread Management** - Track which articles you've read
-   **Blog Filtering** - View articles from specific blogs
-   **Duplicate Prevention** - Never tracks the same article twice
-   **Colored CLI Output** - User-friendly terminal interface
-   **JSON Output** - Machine-readable output for automation and agent integrations

## Installation

```bash
# Install via go
go install github.com/JulienTant/blogwatcher-cli/cmd/blogwatcher-cli@latest

# Or build locally
go build ./cmd/blogwatcher-cli

# Or run via Docker
docker run --rm -v blogwatcher-cli:/data ghcr.io/julientant/blogwatcher-cli
```

Pre-built binaries for Linux, macOS, and Windows are available on the [GitHub Releases](https://github.com/JulienTant/blogwatcher-cli/releases) page.

## Usage

### Output Format

Human-readable text output is the default. Use `--format json` for machine-readable output in scripts or agent integrations:

```bash
blogwatcher-cli add "xkcd" https://xkcd.com --feed-url https://xkcd.com/atom.xml --format json
blogwatcher-cli blogs --format json
blogwatcher-cli scan --format json
blogwatcher-cli articles --format json
blogwatcher-cli read 42 --format json
blogwatcher-cli unread 42 --format json
blogwatcher-cli read-all --yes --format json
blogwatcher-cli remove "xkcd" --yes --format json
blogwatcher-cli import subscriptions.opml --format json
```

You can also set `BLOGWATCHER_FORMAT=json`. Commands that normally prompt for confirmation (`remove`, `read-all`) require `--yes` in JSON mode.

### Adding Blogs

```bash
# Add a blog (auto-discovers RSS feed)
blogwatcher-cli add "My Favorite Blog" https://example.com/blog

# Add with explicit feed URL
blogwatcher-cli add "Tech Blog" https://techblog.com --feed-url https://techblog.com/rss.xml

# Add with HTML scraping selector (for blogs without feeds)
blogwatcher-cli add "No-RSS Blog" https://norss.com --scrape-selector "article h2 a"
```

### Managing Blogs

```bash
# List all tracked blogs
blogwatcher-cli blogs

# Remove a blog (and all its articles)
blogwatcher-cli remove "My Favorite Blog"

# Remove without confirmation
blogwatcher-cli remove "My Favorite Blog" -y
```

### Scanning for New Articles

```bash
# Scan all blogs for new articles
blogwatcher-cli scan

# Scan a specific blog
blogwatcher-cli scan "Tech Blog"
```

### Viewing Articles

```bash
# List unread articles
blogwatcher-cli articles

# List all articles (including read)
blogwatcher-cli articles --all

# List articles from a specific blog
blogwatcher-cli articles --blog "Tech Blog"

# Filter articles by publication date
blogwatcher-cli articles --since 2024-01-01          # Articles on or after Jan 1, 2024 (inclusive)
blogwatcher-cli articles --before 2024-01-15         # Articles before Jan 15, 2024 (exclusive)
blogwatcher-cli articles --since 2024-01-01 --before 2024-01-15  # Articles from Jan 1 up to but not including Jan 15
```

Date filters use the `published_date` of articles and require the format `YYYY-MM-DD`. Articles without a publication date are excluded when date filters are applied.

### Managing Read Status

```bash
# Mark an article as read (use article ID from articles list)
blogwatcher-cli read 42

# Mark an article as unread
blogwatcher-cli unread 42

# Mark all unread articles as read
blogwatcher-cli read-all

# Mark all unread articles as read for a blog (skip prompt)
blogwatcher-cli read-all --blog "Tech Blog" --yes
```

## How It Works

### Scanning Process

1. For each tracked blog, blogwatcher-cli first attempts to parse the RSS/Atom feed
2. If no feed URL is configured, it tries to auto-discover one from the blog homepage
3. If RSS parsing fails and a `scrape_selector` is configured, it falls back to HTML scraping
4. New articles are saved to the database as unread
5. Already-tracked articles are skipped

### Feed Auto-Discovery

blogwatcher-cli searches for feeds in two ways:

-   Looking for `<link rel="alternate">` tags with RSS/Atom types
-   Checking common feed paths: `/feed`, `/rss`, `/feed.xml`, `/atom.xml`, etc.

### HTML Scraping

When RSS isn't available, provide a CSS selector that matches article links:

```bash
# Example selectors
--scrape-selector "article h2 a"      # Links inside article h2 tags
--scrape-selector ".post-title a"     # Links with post-title class
--scrape-selector "#blog-posts a"     # Links inside blog-posts ID
```

## Database

blogwatcher-cli stores data in SQLite at `~/.blogwatcher-cli/blogwatcher-cli.db`.

If upgrading from the original [Hyaxia/blogwatcher](https://github.com/Hyaxia/blogwatcher), migrate your existing database:

```bash
mv ~/.blogwatcher/blogwatcher.db ~/.blogwatcher-cli/blogwatcher-cli.db
```

Tables:

-   **blogs** - Tracked blogs (name, URL, feed URL, scrape selector)
-   **articles** - Discovered articles (title, URL, dates, read status)

## Development

### Requirements

-   [mise](https://mise.jdx.dev/) (manages Go, golangci-lint, gotestsum, goreleaser)

```bash
mise install
```

### Running Tests

```bash
# Run all tests
gotestsum -- ./...

# Run e2e tests only
gotestsum -- ./e2e/ -count=1

# Update e2e expected output after intentional changes
UPDATE_EXPECTED=1 go test ./e2e/ -run TestE2E/flags
```

### Publishing

Push a tag to trigger a release (binaries + Docker images to GHCR):

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

## License

MIT
