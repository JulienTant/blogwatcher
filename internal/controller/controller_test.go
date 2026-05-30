package controller

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JulienTant/blogwatcher-cli/internal/model"
	"github.com/JulienTant/blogwatcher-cli/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddBlogAndRemoveBlog(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := AddBlog(ctx, db, "Test", "https://example.com", "", "")
	require.NoError(t, err, "add blog")

	_, err = AddBlog(ctx, db, "Test", "https://other.com", "", "")
	require.Error(t, err, "expected duplicate name error")

	_, err = AddBlog(ctx, db, "Other", "https://example.com", "", "")
	require.Error(t, err, "expected duplicate url error")

	err = RemoveBlog(ctx, db, blog.Name)
	require.NoError(t, err, "remove blog")
}

func TestAddBlogInvalidURL(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	testCases := []struct {
		name    string
		url     string
		feedURL string
	}{
		{"empty URL", "", ""},
		{"invalid scheme ftp", "ftp://example.com", ""},
		{"invalid scheme file", "file:///etc/passwd", ""},
		{"missing scheme", "example.com", ""},
		{"invalid URL format", "://invalid-url", ""},
		{"scheme only", "https://", ""},
		{"empty host with path", "http:///path", ""},
		{"invalid feed URL", "https://example.com", "ftp://feed.example.com/rss"},
		{"feed URL scheme only", "https://example.com", "https://"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := AddBlog(ctx, db, "Test"+tc.name, tc.url, tc.feedURL, "")
			require.Error(t, err, "expected error for invalid URL")

			var invalidURLErr InvalidURLError
			require.ErrorAs(t, err, &invalidURLErr, "error should be InvalidURLError")
		})
	}
}

func TestAddBlogValidURL(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	testCases := []struct {
		name    string
		url     string
		feedURL string
	}{
		{"https URL", "https://example.com", ""},
		{"http URL", "http://example.com", ""},
		{"https with feed", "https://example.com", "https://example.com/feed.xml"},
		{"http with feed", "http://example.com", "http://example.com/rss"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			blogName := "Valid" + tc.name
			blog, err := AddBlog(ctx, db, blogName, tc.url, tc.feedURL, "")
			require.NoError(t, err, "expected no error for valid URL")
			require.Equal(t, tc.url, blog.URL)
			require.Equal(t, tc.feedURL, blog.FeedURL)

			// Clean up
			err = RemoveBlog(ctx, db, blogName)
			require.NoError(t, err)
		})
	}
}

func TestArticleReadUnread(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := AddBlog(ctx, db, "Test", "https://example.com", "", "")
	require.NoError(t, err, "add blog")
	article, err := db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	require.NoError(t, err, "add article")

	read, err := MarkArticleRead(ctx, db, article.ID)
	require.NoError(t, err, "mark read")
	require.False(t, read.IsRead, "expected original state unread")

	unread, err := MarkArticleUnread(ctx, db, article.ID)
	require.NoError(t, err, "mark unread")
	require.True(t, unread.IsRead, "expected original state read")
}

func TestGetArticlesFilters(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := AddBlog(ctx, db, "Test", "https://example.com", "", "")
	require.NoError(t, err, "add blog")
	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	require.NoError(t, err, "add article")

	articles, blogNames, err := GetArticles(ctx, db, storage.ArticleFilter{})
	require.NoError(t, err, "get articles")
	require.Len(t, articles, 1)
	require.Equal(t, blog.Name, blogNames[blog.ID])
}

func TestImportOPML(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	opmlData := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.0">
    <head><title>Subscriptions</title></head>
    <body>
        <outline text="Tech" title="Tech">
            <outline type="rss" text="Blog A" title="Blog A" xmlUrl="http://a.com/feed" htmlUrl="http://a.com"/>
            <outline type="rss" text="Blog B" title="Blog B" xmlUrl="http://b.com/rss" htmlUrl="http://b.com"/>
        </outline>
    </body>
</opml>`

	added, skipped, err := ImportOPML(ctx, db, strings.NewReader(opmlData))
	require.NoError(t, err)
	assert.Equal(t, 2, added)
	assert.Equal(t, 0, skipped)

	// Verify blogs were actually persisted.
	blogs, err := db.ListBlogs(ctx)
	require.NoError(t, err)
	assert.Len(t, blogs, 2)
}

func TestImportOPMLSkipsDuplicates(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	// Pre-add a blog that will conflict.
	_, err := AddBlog(ctx, db, "Blog A", "http://a.com", "http://a.com/feed", "")
	require.NoError(t, err)

	opmlData := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.0">
    <head><title>Subscriptions</title></head>
    <body>
        <outline type="rss" text="Blog A" title="Blog A" xmlUrl="http://a.com/feed" htmlUrl="http://a.com"/>
        <outline type="rss" text="Blog B" title="Blog B" xmlUrl="http://b.com/rss" htmlUrl="http://b.com"/>
    </body>
</opml>`

	added, skipped, err := ImportOPML(ctx, db, strings.NewReader(opmlData))
	require.NoError(t, err)
	assert.Equal(t, 1, added)
	assert.Equal(t, 1, skipped)
}

func TestImportOPMLSkipsInvalidURLs(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	// One feed has an unsupported scheme (ftp), one is valid. The bad one
	// must not halt the import of the good one.
	opmlData := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.0">
    <head><title>Subscriptions</title></head>
    <body>
        <outline type="rss" text="Bad" title="Bad" xmlUrl="ftp://bad.example.com/feed" htmlUrl="ftp://bad.example.com"/>
        <outline type="rss" text="Good" title="Good" xmlUrl="http://good.example.com/rss" htmlUrl="http://good.example.com"/>
    </body>
</opml>`

	added, skipped, err := ImportOPML(ctx, db, strings.NewReader(opmlData))
	require.NoError(t, err)
	assert.Equal(t, 1, added)
	assert.Equal(t, 1, skipped)

	good, err := db.GetBlogByName(ctx, "Good")
	require.NoError(t, err)
	require.NotNil(t, good, "valid feed should still be imported after an invalid one")

	bad, err := db.GetBlogByName(ctx, "Bad")
	require.NoError(t, err)
	assert.Nil(t, bad, "invalid feed should not be persisted")
}

func TestImportOPMLFallbackSiteURL(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	// Feed with no htmlUrl -- siteURL should fall back to feedURL.
	opmlData := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.0">
    <head><title>Test</title></head>
    <body>
        <outline type="rss" text="NoSite" title="NoSite" xmlUrl="http://nosite.com/feed"/>
    </body>
</opml>`

	added, skipped, err := ImportOPML(ctx, db, strings.NewReader(opmlData))
	require.NoError(t, err)
	assert.Equal(t, 1, added)
	assert.Equal(t, 0, skipped)

	blog, err := db.GetBlogByName(ctx, "NoSite")
	require.NoError(t, err)
	require.NotNil(t, blog)
	assert.Equal(t, "http://nosite.com/feed", blog.URL, "site URL should fall back to feed URL")
}

func TestImportOPMLInvalidXML(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	_, _, err := ImportOPML(ctx, db, strings.NewReader("not xml"))
	require.Error(t, err)
}

func TestImportOPMLEmptyTitleFallback(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	opmlData := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.0">
    <head><title>Test</title></head>
    <body>
        <outline type="rss" text="" title="" xmlUrl="http://a.com/feed" htmlUrl="http://a.com"/>
        <outline type="rss" text="" title="" xmlUrl="http://b.com/feed" htmlUrl="http://b.com"/>
    </body>
</opml>`

	added, skipped, err := ImportOPML(ctx, db, strings.NewReader(opmlData))
	require.NoError(t, err)
	assert.Equal(t, 2, added, "both feeds should be added with fallback names")
	assert.Equal(t, 0, skipped)

	blogA, err := db.GetBlogByURL(ctx, "http://a.com")
	require.NoError(t, err)
	require.NotNil(t, blogA)
	assert.Equal(t, "http://a.com", blogA.Name, "name should fall back to site URL")

	blogB, err := db.GetBlogByURL(ctx, "http://b.com")
	require.NoError(t, err)
	require.NotNil(t, blogB)
	assert.Equal(t, "http://b.com", blogB.Name, "name should fall back to site URL")
}

func TestGetArticlesFilterByCategory(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := AddBlog(ctx, db, "Test", "https://example.com", "", "")
	require.NoError(t, err, "add blog")

	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "Go Post", URL: "https://example.com/1", Categories: []string{"Go", "Programming"}})
	require.NoError(t, err, "add article")
	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "Rust Post", URL: "https://example.com/2", Categories: []string{"Rust"}})
	require.NoError(t, err, "add article")

	// Filter by Go
	cat := "Go"
	articles, _, err := GetArticles(ctx, db, storage.ArticleFilter{Category: &cat})
	require.NoError(t, err, "get articles by category")
	require.Len(t, articles, 1)
	require.Equal(t, "Go Post", articles[0].Title)

	// No filter returns all
	all, _, err := GetArticles(ctx, db, storage.ArticleFilter{})
	require.NoError(t, err, "get all articles")
	require.Len(t, all, 2)
}

func openTestDB(t *testing.T) *storage.Database {
	t.Helper()
	path := filepath.Join(t.TempDir(), "blogwatcher-cli.db")
	db, err := storage.OpenDatabase(context.Background(), path)
	require.NoError(t, err, "open database")
	return db
}
