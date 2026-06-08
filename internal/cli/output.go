package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"

	"github.com/JulienTant/blogwatcher-cli/internal/model"
	"github.com/JulienTant/blogwatcher-cli/internal/scanner"
)

const (
	outputFormatText = "text"
	outputFormatJSON = "json"
)

type blogOutput struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	URL            string     `json:"url"`
	FeedURL        string     `json:"feed_url,omitempty"`
	ScrapeSelector string     `json:"scrape_selector,omitempty"`
	LastScanned    *time.Time `json:"last_scanned,omitempty"`
}

type blogsOutput struct {
	Blogs []blogOutput `json:"blogs"`
}

type articleOutput struct {
	ID             int64      `json:"id"`
	BlogID         int64      `json:"blog_id"`
	Blog           string     `json:"blog"`
	Title          string     `json:"title"`
	URL            string     `json:"url"`
	PublishedDate  *time.Time `json:"published_date,omitempty"`
	DiscoveredDate *time.Time `json:"discovered_date,omitempty"`
	IsRead         bool       `json:"is_read"`
	Categories     []string   `json:"categories,omitempty"`
}

type articlesOutput struct {
	Articles []articleOutput `json:"articles"`
}

type scanResultOutput struct {
	BlogName    string `json:"blog_name"`
	NewArticles int    `json:"new_articles"`
	TotalFound  int    `json:"total_found"`
	Source      string `json:"source"`
	Error       string `json:"error,omitempty"`
}

type scanOutput struct {
	Scanned          int                `json:"scanned"`
	Succeeded        int                `json:"succeeded"`
	Failed           int                `json:"failed"`
	TotalNewArticles int                `json:"total_new_articles"`
	Results          []scanResultOutput `json:"results"`
}

type addBlogOutput struct {
	OK   bool       `json:"ok"`
	Blog blogOutput `json:"blog"`
}

type removeBlogOutput struct {
	OK   bool   `json:"ok"`
	Name string `json:"name"`
}

type articleStatusOutput struct {
	OK        bool          `json:"ok"`
	Action    string        `json:"action"`
	ArticleID int64         `json:"article_id"`
	Changed   bool          `json:"changed"`
	Article   articleOutput `json:"article"`
}

type readAllOutput struct {
	OK       bool            `json:"ok"`
	Blog     string          `json:"blog,omitempty"`
	Count    int             `json:"count"`
	Articles []articleOutput `json:"articles"`
}

type importOutput struct {
	OK      bool `json:"ok"`
	Added   int  `json:"added"`
	Skipped int  `json:"skipped"`
}

func isJSONOutput() bool {
	return viper.GetString("format") == outputFormatJSON
}

func validateOutputFormat(format string) error {
	switch format {
	case outputFormatText, outputFormatJSON:
		return nil
	default:
		return fmt.Errorf("invalid output format %q: expected text or json", format)
	}
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func newBlogOutput(blog model.Blog) blogOutput {
	return blogOutput{
		ID:             blog.ID,
		Name:           blog.Name,
		URL:            blog.URL,
		FeedURL:        blog.FeedURL,
		ScrapeSelector: blog.ScrapeSelector,
		LastScanned:    blog.LastScanned,
	}
}

func newBlogsOutput(blogs []model.Blog) blogsOutput {
	out := blogsOutput{Blogs: make([]blogOutput, 0, len(blogs))}
	for _, blog := range blogs {
		out.Blogs = append(out.Blogs, newBlogOutput(blog))
	}
	return out
}

func newArticleOutput(article model.Article, blogName string) articleOutput {
	return articleOutput{
		ID:             article.ID,
		BlogID:         article.BlogID,
		Blog:           blogName,
		Title:          article.Title,
		URL:            article.URL,
		PublishedDate:  article.PublishedDate,
		DiscoveredDate: article.DiscoveredDate,
		IsRead:         article.IsRead,
		Categories:     article.Categories,
	}
}

func newArticlesOutput(articles []model.Article, blogNames map[int64]string) articlesOutput {
	out := articlesOutput{Articles: make([]articleOutput, 0, len(articles))}
	for _, article := range articles {
		out.Articles = append(out.Articles, newArticleOutput(article, blogNames[article.BlogID]))
	}
	return out
}

func newScanOutput(results []scanner.ScanResult) scanOutput {
	out := scanOutput{Results: make([]scanResultOutput, 0, len(results))}
	for _, result := range results {
		out.Scanned++
		if result.Error != "" {
			out.Failed++
		} else {
			out.Succeeded++
			out.TotalNewArticles += result.NewArticles
		}
		out.Results = append(out.Results, scanResultOutput{
			BlogName:    result.BlogName,
			NewArticles: result.NewArticles,
			TotalFound:  result.TotalFound,
			Source:      result.Source,
			Error:       result.Error,
		})
	}
	return out
}
