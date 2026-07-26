package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/JulienTant/blogwatcher-cli/internal/model"
	"github.com/JulienTant/blogwatcher-cli/internal/opml"
	"github.com/JulienTant/blogwatcher-cli/internal/storage"
)

type BlogNotFoundError struct {
	Name string
}

func (e BlogNotFoundError) Error() string {
	return fmt.Sprintf("Blog '%s' not found", e.Name)
}

type BlogAlreadyExistsError struct {
	Field string
	Value string
}

func (e BlogAlreadyExistsError) Error() string {
	return fmt.Sprintf("Blog with %s '%s' already exists", e.Field, e.Value)
}

type ArticleNotFoundError struct {
	ID int64
}

func (e ArticleNotFoundError) Error() string {
	return fmt.Sprintf("Article %d not found", e.ID)
}

type InvalidURLError struct {
	URL string
}

func (e InvalidURLError) Error() string {
	return fmt.Sprintf("Invalid URL: %s", e.URL)
}

// validateHTTPURL returns an InvalidURLError unless s parses as an absolute
// http or https URL with a non-empty host. A bare scheme like "https://" is
// rejected because url.Parse accepts it with an empty Host.
func validateHTTPURL(s string) error {
	parsed, err := url.Parse(s)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return InvalidURLError{URL: s}
	}
	return nil
}

func AddBlog(ctx context.Context, db *storage.Database, name string, urlStr string, feedURL string, scrapeSelector string, group string) (model.Blog, error) {
	if err := validateHTTPURL(urlStr); err != nil {
		return model.Blog{}, err
	}
	if feedURL != "" {
		if err := validateHTTPURL(feedURL); err != nil {
			return model.Blog{}, err
		}
	}

	if existing, err := db.GetBlogByName(ctx, name); err != nil {
		return model.Blog{}, err
	} else if existing != nil {
		return model.Blog{}, BlogAlreadyExistsError{Field: "name", Value: name}
	}
	if existing, err := db.GetBlogByURL(ctx, urlStr); err != nil {
		return model.Blog{}, err
	} else if existing != nil {
		return model.Blog{}, BlogAlreadyExistsError{Field: "URL", Value: urlStr}
	}

	blog := model.Blog{
		Name:           name,
		URL:            urlStr,
		FeedURL:        feedURL,
		ScrapeSelector: scrapeSelector,
		Group:          group,
	}
	return db.AddBlog(ctx, blog)
}

func RemoveBlog(ctx context.Context, db *storage.Database, name string) error {
	blog, err := db.GetBlogByName(ctx, name)
	if err != nil {
		return err
	}
	if blog == nil {
		return BlogNotFoundError{Name: name}
	}
	_, err = db.RemoveBlog(ctx, blog.ID)
	return err
}

func GetArticles(ctx context.Context, db *storage.Database, showAll bool, blogName string, category string, group string, since *time.Time, before *time.Time) ([]model.Article, map[int64]string, error) {
	var blogID *int64
	if blogName != "" {
		blog, err := db.GetBlogByName(ctx, blogName)
		if err != nil {
			return nil, nil, err
		}
		if blog == nil {
			return nil, nil, BlogNotFoundError{Name: blogName}
		}
		blogID = &blog.ID
	}

	var categoryPtr *string
	if category != "" {
		categoryPtr = &category
	}

	var groupPtr *string
	if group != "" {
		groupPtr = &group
	}

	articles, err := db.ListArticles(ctx, !showAll, blogID, categoryPtr, groupPtr, since, before)
	if err != nil {
		return nil, nil, err
	}
	blogs, err := db.ListBlogs(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	blogNames := make(map[int64]string)
	for _, blog := range blogs {
		blogNames[blog.ID] = blog.Name
	}

	return articles, blogNames, nil
}

func MarkArticleRead(ctx context.Context, db *storage.Database, articleID int64) (model.Article, error) {
	article, err := db.GetArticle(ctx, articleID)
	if err != nil {
		return model.Article{}, err
	}
	if article == nil {
		return model.Article{}, ArticleNotFoundError{ID: articleID}
	}
	if !article.IsRead {
		_, err = db.MarkArticleRead(ctx, articleID)
		if err != nil {
			return model.Article{}, err
		}
	}
	return *article, nil
}

func MarkAllArticlesRead(ctx context.Context, db *storage.Database, blogName string) ([]model.Article, error) {
	var blogID *int64
	if blogName != "" {
		blog, err := db.GetBlogByName(ctx, blogName)
		if err != nil {
			return nil, err
		}
		if blog == nil {
			return nil, BlogNotFoundError{Name: blogName}
		}
		blogID = &blog.ID
	}

	articles, err := db.ListArticles(ctx, true, blogID, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	for _, article := range articles {
		_, err := db.MarkArticleRead(ctx, article.ID)
		if err != nil {
			return nil, err
		}
	}

	return articles, nil
}

func ImportOPML(ctx context.Context, db *storage.Database, r io.Reader) (added int, skipped int, err error) {
	feeds, err := opml.Parse(r)
	if err != nil {
		return 0, 0, err
	}
	for _, feed := range feeds {
		siteURL := feed.SiteURL
		if siteURL == "" {
			siteURL = feed.FeedURL
		}
		title := strings.TrimSpace(feed.Title)
		if title == "" {
			title = siteURL
		}
		_, err := AddBlog(ctx, db, title, siteURL, feed.FeedURL, "", "")
		if err != nil {
			var alreadyExists BlogAlreadyExistsError
			var invalidURL InvalidURLError
			if errors.As(err, &alreadyExists) || errors.As(err, &invalidURL) {
				skipped++
				continue
			}
			return added, skipped, err
		}
		added++
	}
	return added, skipped, nil
}

func MarkArticleUnread(ctx context.Context, db *storage.Database, articleID int64) (model.Article, error) {
	article, err := db.GetArticle(ctx, articleID)
	if err != nil {
		return model.Article{}, err
	}
	if article == nil {
		return model.Article{}, ArticleNotFoundError{ID: articleID}
	}
	if article.IsRead {
		_, err = db.MarkArticleUnread(ctx, articleID)
		if err != nil {
			return model.Article{}, err
		}
	}
	return *article, nil
}
