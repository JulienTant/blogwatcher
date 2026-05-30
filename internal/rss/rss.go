package rss

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

type FeedArticle struct {
	Title         string
	URL           string
	PublishedDate *time.Time
	Categories    []string
	Description   string
	Content       string
}

type FeedParseError struct {
	Message string
}

func (e FeedParseError) Error() string {
	return e.Message
}

// Fetcher fetches and parses RSS/Atom feeds.
type Fetcher struct {
	client *http.Client
}

// NewFetcher creates a Fetcher with the given HTTP client.
func NewFetcher(client *http.Client) *Fetcher {
	return &Fetcher{client: client}
}

func (f *Fetcher) ParseFeed(ctx context.Context, feedURL string) ([]FeedArticle, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, FeedParseError{Message: fmt.Sprintf("failed to create request: %v", err)}
	}
	response, err := f.client.Do(req)
	if err != nil {
		return nil, FeedParseError{Message: fmt.Sprintf("failed to fetch feed: %v", err)}
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close: %v\n", err)
		}
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, FeedParseError{Message: fmt.Sprintf("failed to fetch feed: status %d", response.StatusCode)}
	}

	parser := gofeed.NewParser()
	feed, err := parser.Parse(response.Body)
	if err != nil {
		return nil, FeedParseError{Message: fmt.Sprintf("failed to parse feed: %v", err)}
	}

	var articles []FeedArticle
	for _, item := range feed.Items {
		title := strings.TrimSpace(item.Title)
		link := strings.TrimSpace(item.Link)
		if title == "" || link == "" {
			continue
		}
		articles = append(articles, FeedArticle{
			Title:         title,
			URL:           link,
			PublishedDate: pickPublishedDate(item),
			Categories:    item.Categories,
			Description:   item.Description,
			Content:       item.Content,
		})
	}

	return articles, nil
}

func (f *Fetcher) DiscoverFeedURL(ctx context.Context, blogURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, blogURL, nil)
	if err != nil {
		return "", fmt.Errorf("discover feed: %w", err)
	}
	response, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("discover feed: %w", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close: %v\n", err)
		}
	}()
	if response.StatusCode >= 500 {
		return "", FeedParseError{Message: fmt.Sprintf("discover feed: server error status %d", response.StatusCode)}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Client errors (4xx) are not transient — just means no feed at this URL.
		return "", nil
	}

	// If the URL already returns a feed content-type, return it directly.
	contentType := response.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		// Only accept explicit feed types, not generic XML (to avoid sitemap false positives).
		if mediaType == "application/rss+xml" || mediaType == "application/atom+xml" || mediaType == "application/feed+json" {
			return blogURL, nil
		}
	}

	base, err := url.Parse(blogURL)
	if err != nil {
		return "", fmt.Errorf("discover feed: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		return "", fmt.Errorf("discover feed: parse HTML: %w", err)
	}

	feedTypes := []string{
		"application/rss+xml",
		"application/atom+xml",
		"application/feed+json",
		"application/xml",
		"text/xml",
	}

	for _, feedType := range feedTypes {
		selection := doc.Find(fmt.Sprintf("link[rel='alternate'][type='%s']", feedType)).First()
		if selection.Length() == 0 {
			// Also check rel="self" for feeds that use self-referencing links.
			selection = doc.Find(fmt.Sprintf("link[rel='self'][type='%s']", feedType)).First()
		}
		if selection.Length() == 0 {
			continue
		}
		href, exists := selection.Attr("href")
		if !exists {
			continue
		}
		resolved := resolveURL(base, href)
		if resolved != "" {
			return resolved, nil
		}
	}

	commonPaths := []string{
		"/feed",
		"/feed/",
		"/rss",
		"/rss/",
		"/feed.xml",
		"/rss.xml",
		"/atom.xml",
		"/index.xml",
	}

	for _, path := range commonPaths {
		resolved := resolveURL(base, path)
		if resolved == "" {
			continue
		}
		ok, err := f.isValidFeed(ctx, resolved)
		if err == nil && ok {
			return resolved, nil
		}
	}

	return "", nil
}

func (f *Fetcher) isValidFeed(ctx context.Context, feedURL string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return false, err
	}
	response, err := f.client.Do(req)
	if err != nil {
		return false, err
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close: %v\n", err)
		}
	}()
	if response.StatusCode != http.StatusOK {
		return false, nil
	}

	parser := gofeed.NewParser()
	feed, err := parser.Parse(response.Body)
	if err != nil {
		return false, err
	}

	return len(feed.Items) > 0 || strings.TrimSpace(feed.Title) != "", nil
}

func resolveURL(base *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	return base.ResolveReference(parsed).String()
}

func pickPublishedDate(item *gofeed.Item) *time.Time {
	if item == nil {
		return nil
	}
	if item.PublishedParsed != nil {
		return item.PublishedParsed
	}
	if item.UpdatedParsed != nil {
		return item.UpdatedParsed
	}
	return nil
}

func IsFeedError(err error) bool {
	var parseErr FeedParseError
	return errors.As(err, &parseErr)
}
