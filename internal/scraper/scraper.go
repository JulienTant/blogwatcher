package scraper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type ScrapedArticle struct {
	Title         string
	URL           string
	PublishedDate *time.Time
}

type ScrapeError struct {
	Message string
}

func (e ScrapeError) Error() string {
	return e.Message
}

// Scraper scrapes HTML pages for article links.
type Scraper struct {
	client    *http.Client
	userAgent string
}

// NewScraper creates a Scraper with the given HTTP client and User-Agent.
func NewScraper(client *http.Client, userAgent string) *Scraper {
	return &Scraper{client: client, userAgent: userAgent}
}

func (s *Scraper) ScrapeBlog(ctx context.Context, blogURL string, selector string) ([]ScrapedArticle, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, blogURL, nil)
	if err != nil {
		return nil, ScrapeError{Message: fmt.Sprintf("failed to create request: %v", err)}
	}
	req.Header.Set("User-Agent", s.userAgent)
	response, err := s.client.Do(req)
	if err != nil {
		return nil, ScrapeError{Message: fmt.Sprintf("failed to fetch page: %v", err)}
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close: %v\n", err)
		}
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, ScrapeError{Message: fmt.Sprintf("failed to fetch page: status %d", response.StatusCode)}
	}

	base, err := url.Parse(blogURL)
	if err != nil {
		return nil, ScrapeError{Message: "invalid blog url"}
	}

	doc, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		return nil, ScrapeError{Message: fmt.Sprintf("failed to parse page: %v", err)}
	}

	seen := make(map[string]struct{})
	var articles []ScrapedArticle

	doc.Find(selector).Each(func(_ int, selection *goquery.Selection) {
		link := selection
		if goquery.NodeName(selection) != "a" {
			link = selection.Find("a").First()
		}
		if link.Length() == 0 {
			return
		}
		href, exists := link.Attr("href")
		if !exists {
			return
		}
		resolved := resolveURL(base, href)
		if resolved == "" {
			return
		}
		if _, ok := seen[resolved]; ok {
			return
		}
		seen[resolved] = struct{}{}

		title := extractTitle(link, selection)
		if title == "" {
			return
		}
		articles = append(articles, ScrapedArticle{
			Title: title,
			URL:   resolved,
		})
	})

	return articles, nil
}

func extractTitle(link *goquery.Selection, parent *goquery.Selection) string {
	text := strings.TrimSpace(link.Text())
	if text != "" {
		return text
	}
	if title, exists := link.Attr("title"); exists {
		title = strings.TrimSpace(title)
		if title != "" {
			return title
		}
	}
	if parent != nil && parent != link {
		text = strings.TrimSpace(parent.Text())
		if text != "" {
			return text
		}
	}
	return ""
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

func IsScrapeError(err error) bool {
	var scrapeErr ScrapeError
	return errors.As(err, &scrapeErr)
}
