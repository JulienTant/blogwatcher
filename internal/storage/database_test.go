package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/stretchr/testify/require"

	"github.com/JulienTant/blogwatcher-cli/internal/model"
	"github.com/JulienTant/blogwatcher-cli/internal/storage/migrations"
)

func TestDatabaseCreatesFileAndCRUD(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	_, err = os.Stat(path)
	require.NoError(t, err, "expected db file to exist")

	blog, err := db.AddBlog(ctx, model.Blog{Name: "Test", URL: "https://example.com"})
	require.NoError(t, err, "add blog")
	require.NotEqual(t, int64(0), blog.ID, "expected blog ID")

	fetched, err := db.GetBlog(ctx, blog.ID)
	require.NoError(t, err, "get blog")
	require.NotNil(t, fetched)
	require.Equal(t, "Test", fetched.Name)

	articles := []model.Article{
		{BlogID: blog.ID, Title: "One", URL: "https://example.com/1"},
		{BlogID: blog.ID, Title: "Two", URL: "https://example.com/2"},
	}
	count, err := db.AddArticlesBulk(ctx, articles)
	require.NoError(t, err, "add articles bulk")
	require.Equal(t, 2, count)

	list, err := db.ListArticles(ctx, ArticleFilter{})
	require.NoError(t, err, "list articles")
	require.Len(t, list, 2)

	ok, err := db.MarkArticleRead(ctx, list[0].ID)
	require.NoError(t, err, "mark read")
	require.True(t, ok)

	updated, err := db.GetArticle(ctx, list[0].ID)
	require.NoError(t, err, "get article")
	require.NotNil(t, updated)
	require.True(t, updated.IsRead)

	now := time.Now()
	err = db.UpdateBlogLastScanned(ctx, blog.ID, now)
	require.NoError(t, err, "update last scanned")

	deleted, err := db.RemoveBlog(ctx, blog.ID)
	require.NoError(t, err, "remove blog")
	require.True(t, deleted)
}

func TestGetExistingArticleURLs(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "Test", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "One", URL: "https://example.com/1"})
	require.NoError(t, err, "add article")

	existing, err := db.GetExistingArticleURLs(ctx, []string{"https://example.com/1", "https://example.com/2"})
	require.NoError(t, err, "get existing")
	_, ok := existing["https://example.com/1"]
	require.True(t, ok, "expected existing url")
	_, ok = existing["https://example.com/2"]
	require.False(t, ok, "did not expect url")
}

func TestDatabaseForeignKeyEnforced(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	_, err = db.AddArticle(ctx, model.Article{BlogID: 9999, Title: "Orphan", URL: "https://example.com/orphan"})
	require.Error(t, err, "expected foreign key error for missing blog")
}

func TestBlogOptionalFieldsRoundTrip(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "Test", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	fetched, err := db.GetBlog(ctx, blog.ID)
	require.NoError(t, err, "get blog")
	require.NotNil(t, fetched)
	require.Empty(t, fetched.FeedURL)
	require.Empty(t, fetched.ScrapeSelector)
}

func TestBlogTimeRoundTrip(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	// Sub-second precision is dropped on write so that the lexicographic
	// comparison in ListArticles is reliable. Use a second-precision instant
	// for the round-trip assertion.
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	blog, err := db.AddBlog(ctx, model.Blog{
		Name:        "Test",
		URL:         "https://example.com",
		LastScanned: &now,
	})
	require.NoError(t, err, "add blog")

	fetched, err := db.GetBlog(ctx, blog.ID)
	require.NoError(t, err, "get blog")
	require.NotNil(t, fetched)
	require.NotNil(t, fetched.LastScanned)
	require.True(t, fetched.LastScanned.Equal(now), "expected last scanned %s, got %s", now.Format(time.RFC3339Nano), fetched.LastScanned.Format(time.RFC3339Nano))
}

func TestArticleTimeRoundTripAndNilDiscoveredDate(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "Test", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	// Stored at second precision (see TestBlogTimeRoundTrip for the rationale).
	published := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)
	article, err := db.AddArticle(ctx, model.Article{
		BlogID:        blog.ID,
		Title:         "Title",
		URL:           "https://example.com/1",
		PublishedDate: &published,
	})
	require.NoError(t, err, "add article")

	fetched, err := db.GetArticle(ctx, article.ID)
	require.NoError(t, err, "get article")
	require.NotNil(t, fetched)
	require.NotNil(t, fetched.PublishedDate)
	require.True(t, fetched.PublishedDate.Equal(published), "expected published date %s, got %s", published.Format(time.RFC3339Nano), fetched.PublishedDate.Format(time.RFC3339Nano))
	require.Nil(t, fetched.DiscoveredDate, "expected discovered date nil when not set")
}

func TestListArticlesFiltersAndOrdering(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	blogA, err := db.AddBlog(ctx, model.Blog{Name: "A", URL: "https://a.example.com"})
	require.NoError(t, err, "add blog")
	blogB, err := db.AddBlog(ctx, model.Blog{Name: "B", URL: "https://b.example.com"})
	require.NoError(t, err, "add blog")

	t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)

	first, err := db.AddArticle(ctx, model.Article{BlogID: blogA.ID, Title: "Old", URL: "https://a.example.com/old", DiscoveredDate: &t1})
	require.NoError(t, err, "add article")
	second, err := db.AddArticle(ctx, model.Article{BlogID: blogA.ID, Title: "New", URL: "https://a.example.com/new", DiscoveredDate: &t2})
	require.NoError(t, err, "add article")
	_, err = db.AddArticle(ctx, model.Article{BlogID: blogB.ID, Title: "Other", URL: "https://b.example.com/1", DiscoveredDate: &t2})
	require.NoError(t, err, "add article")

	_, err = db.MarkArticleRead(ctx, first.ID)
	require.NoError(t, err, "mark read")

	all, err := db.ListArticles(ctx, ArticleFilter{})
	require.NoError(t, err, "list articles")
	require.Len(t, all, 3)
	require.Equal(t, second.ID, all[0].ID, "expected newest article first")

	unread, err := db.ListArticles(ctx, ArticleFilter{UnreadOnly: true})
	require.NoError(t, err, "list unread")
	require.Len(t, unread, 2)

	blogID := blogB.ID
	filtered, err := db.ListArticles(ctx, ArticleFilter{BlogID: &blogID})
	require.NoError(t, err, "list by blog")
	require.Len(t, filtered, 1)
	require.Equal(t, blogB.ID, filtered[0].BlogID)
}

func TestBulkInsertDuplicateRollbackAndEmpty(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "Test", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	count, err := db.AddArticlesBulk(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, 0, count)

	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "Existing", URL: "https://example.com/existing"})
	require.NoError(t, err, "add article")

	dupArticles := []model.Article{
		{BlogID: blog.ID, Title: "Dup", URL: "https://example.com/dup"},
		{BlogID: blog.ID, Title: "Dup2", URL: "https://example.com/dup"},
	}
	_, err = db.AddArticlesBulk(ctx, dupArticles)
	require.Error(t, err, "expected bulk insert to fail on duplicate url")

	articles, err := db.ListArticles(ctx, ArticleFilter{})
	require.NoError(t, err, "list articles")
	require.Len(t, articles, 1, "expected rollback on duplicate")
}

func TestLookupHelpers(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	blogByName, err := db.GetBlogByName(ctx, "missing")
	require.NoError(t, err)
	require.Nil(t, blogByName)

	blogByURL, err := db.GetBlogByURL(ctx, "https://missing.example.com")
	require.NoError(t, err)
	require.Nil(t, blogByURL)

	blog, err := db.AddBlog(ctx, model.Blog{Name: "Test", URL: "https://example.com"})
	require.NoError(t, err, "add blog")
	article, err := db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	require.NoError(t, err, "add article")

	found, err := db.GetArticleByURL(ctx, article.URL)
	require.NoError(t, err)
	require.NotNil(t, found)

	exists, err := db.ArticleExists(ctx, article.URL)
	require.NoError(t, err)
	require.True(t, exists)

	exists, err = db.ArticleExists(ctx, "https://example.com/missing")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestArticleCategoriesRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "Test", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	// Article with categories
	article, err := db.AddArticle(ctx, model.Article{
		BlogID:     blog.ID,
		Title:      "Categorized",
		URL:        "https://example.com/cat",
		Categories: []string{"Go", "Programming"},
	})
	require.NoError(t, err, "add article with categories")

	fetched, err := db.GetArticle(ctx, article.ID)
	require.NoError(t, err, "get article")
	require.NotNil(t, fetched)
	require.Equal(t, []string{"Go", "Programming"}, fetched.Categories)

	// Article without categories
	articleNoCat, err := db.AddArticle(ctx, model.Article{
		BlogID: blog.ID,
		Title:  "No Category",
		URL:    "https://example.com/nocat",
	})
	require.NoError(t, err, "add article without categories")

	fetchedNoCat, err := db.GetArticle(ctx, articleNoCat.ID)
	require.NoError(t, err, "get article")
	require.NotNil(t, fetchedNoCat)
	require.Nil(t, fetchedNoCat.Categories)
}

func TestListArticlesFilterByCategory(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "Test", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	_, err = db.AddArticle(ctx, model.Article{
		BlogID:         blog.ID,
		Title:          "Go Article",
		URL:            "https://example.com/go",
		DiscoveredDate: &t1,
		Categories:     []string{"Go", "Programming"},
	})
	require.NoError(t, err, "add go article")

	_, err = db.AddArticle(ctx, model.Article{
		BlogID:         blog.ID,
		Title:          "Rust Article",
		URL:            "https://example.com/rust",
		DiscoveredDate: &t2,
		Categories:     []string{"Rust", "Programming"},
	})
	require.NoError(t, err, "add rust article")

	_, err = db.AddArticle(ctx, model.Article{
		BlogID:         blog.ID,
		Title:          "No Category",
		URL:            "https://example.com/nocat",
		DiscoveredDate: &t3,
	})
	require.NoError(t, err, "add no-cat article")

	// Filter by "Go" - should return only the Go article
	cat := "Go"
	goArticles, err := db.ListArticles(ctx, ArticleFilter{Category: &cat})
	require.NoError(t, err, "list by category Go")
	require.Len(t, goArticles, 1)
	require.Equal(t, "Go Article", goArticles[0].Title)

	// Filter by "Programming" - should return both categorized articles
	cat = "Programming"
	progArticles, err := db.ListArticles(ctx, ArticleFilter{Category: &cat})
	require.NoError(t, err, "list by category Programming")
	require.Len(t, progArticles, 2)

	// No filter - should return all 3
	all, err := db.ListArticles(ctx, ArticleFilter{})
	require.NoError(t, err, "list all")
	require.Len(t, all, 3)

	// Case-insensitive match - "go" should match "Go"
	cat = "go"
	goLower, err := db.ListArticles(ctx, ArticleFilter{Category: &cat})
	require.NoError(t, err, "list by category go (lowercase)")
	require.Len(t, goLower, 1)
	require.Equal(t, "Go Article", goLower[0].Title)

	// Case-insensitive match - "PROGRAMMING" should match "Programming"
	cat = "PROGRAMMING"
	progUpper, err := db.ListArticles(ctx, ArticleFilter{Category: &cat})
	require.NoError(t, err, "list by category PROGRAMMING (uppercase)")
	require.Len(t, progUpper, 2)

	// Empty string category should return all
	empty := ""
	allEmpty, err := db.ListArticles(ctx, ArticleFilter{Category: &empty})
	require.NoError(t, err, "list with empty category")
	require.Len(t, allEmpty, 3)
}

func TestBulkInsertWithCategories(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "Test", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	articles := []model.Article{
		{BlogID: blog.ID, Title: "One", URL: "https://example.com/1", Categories: []string{"AI", "ML"}},
		{BlogID: blog.ID, Title: "Two", URL: "https://example.com/2"},
	}
	count, err := db.AddArticlesBulk(ctx, articles)
	require.NoError(t, err, "bulk insert")
	require.Equal(t, 2, count)

	list, err := db.ListArticles(ctx, ArticleFilter{})
	require.NoError(t, err, "list articles")
	require.Len(t, list, 2)

	// Find the one with categories (order is by discovered_date DESC, both nil so order may vary)
	var withCat *model.Article
	for i := range list {
		if list[i].Title == "One" {
			withCat = &list[i]
			break
		}
	}
	require.NotNil(t, withCat, "should find article with categories")
	require.Len(t, withCat.Categories, 2, "should have 2 categories")
	require.Equal(t, []string{"AI", "ML"}, withCat.Categories, "should match expected categories")
}

func TestListArticlesFilterByDate(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "TestBlog", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	date1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	date2 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	date3 := time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC)

	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "Article1", URL: "https://example.com/1", PublishedDate: &date1})
	require.NoError(t, err, "add article 1")

	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "Article2", URL: "https://example.com/2", PublishedDate: &date2})
	require.NoError(t, err, "add article 2")

	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "Article3", URL: "https://example.com/3", PublishedDate: &date3})
	require.NoError(t, err, "add article 3")

	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "NoDate", URL: "https://example.com/nodate", PublishedDate: nil})
	require.NoError(t, err, "add article without date")

	t.Run("without filters returns all articles", func(t *testing.T) {
		articles, err := db.ListArticles(ctx, ArticleFilter{})
		require.NoError(t, err, "list articles")
		require.Len(t, articles, 4, "should return all articles including no-date article")
	})

	t.Run("since filter inclusive", func(t *testing.T) {
		since := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
		articles, err := db.ListArticles(ctx, ArticleFilter{Since: &since})
		require.NoError(t, err, "list articles with since filter")
		require.Len(t, articles, 2, "should return articles on or after since date (Article2 and Article3)")
		titles := []string{articles[0].Title, articles[1].Title}
		require.Contains(t, titles, "Article2", "should include Article2 published on since date")
		require.Contains(t, titles, "Article3", "should include Article3 after since date")
	})

	t.Run("before filter exclusive", func(t *testing.T) {
		before := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
		articles, err := db.ListArticles(ctx, ArticleFilter{Before: &before})
		require.NoError(t, err, "list articles with before filter")
		require.Len(t, articles, 1, "should return articles before date (only Article1)")
		require.Equal(t, "Article1", articles[0].Title, "should only include Article1 before before-date")
	})

	t.Run("combined filters", func(t *testing.T) {
		since := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
		before := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
		articles, err := db.ListArticles(ctx, ArticleFilter{Since: &since, Before: &before})
		require.NoError(t, err, "list articles with combined filters")
		require.Len(t, articles, 1, "should return only Article2 in range")
		require.Equal(t, "Article2", articles[0].Title, "should only include Article2")
	})

	t.Run("nil published date excluded from filters", func(t *testing.T) {
		since := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		articles, err := db.ListArticles(ctx, ArticleFilter{Since: &since})
		require.NoError(t, err, "list articles with since filter")
		require.Len(t, articles, 3, "should exclude no-date article")

		for _, article := range articles {
			require.NotNil(t, article.PublishedDate, "all returned articles should have published date")
		}
	})

	t.Run("after all dates", func(t *testing.T) {
		since := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
		articles, err := db.ListArticles(ctx, ArticleFilter{Since: &since})
		require.NoError(t, err, "list articles with since filter after all dates")
		require.Empty(t, articles, "should return empty result")
	})

	t.Run("before all dates", func(t *testing.T) {
		before := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		articles, err := db.ListArticles(ctx, ArticleFilter{Before: &before})
		require.NoError(t, err, "list articles with before filter before all dates")
		require.Empty(t, articles, "should return empty result")
	})
}

func TestAddArticleStoresPublishedDateInUTC(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "TZ", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	jst := time.FixedZone("JST", 9*3600)
	published := time.Date(2024, 1, 15, 10, 0, 0, 0, jst) // 2024-01-15 01:00 UTC

	_, err = db.AddArticle(ctx, model.Article{
		BlogID:        blog.ID,
		Title:         "TZ article",
		URL:           "https://example.com/tz",
		PublishedDate: &published,
	})
	require.NoError(t, err, "add article")

	var stored string
	err = sq.Select("published_date").From("articles").Where(sq.Eq{"url": "https://example.com/tz"}).
		RunWith(db.conn).QueryRowContext(ctx).Scan(&stored)
	require.NoError(t, err, "read stored published_date")
	require.Contains(t, stored, "Z", "expected UTC marker in stored value, got %q", stored)
	require.NotContains(t, stored, "+09:00", "expected offset to be stripped, got %q", stored)
}

func TestUpdateBlogLastScannedStoresInUTC(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "TZ", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	jst := time.FixedZone("JST", 9*3600)
	scanned := time.Date(2024, 5, 1, 12, 0, 0, 0, jst) // 2024-05-01 03:00 UTC
	require.NoError(t, db.UpdateBlogLastScanned(ctx, blog.ID, scanned))

	var stored string
	err = sq.Select("last_scanned").From("blogs").Where(sq.Eq{"id": blog.ID}).
		RunWith(db.conn).QueryRowContext(ctx).Scan(&stored)
	require.NoError(t, err, "read stored last_scanned")
	require.Contains(t, stored, "Z", "expected UTC marker, got %q", stored)
	require.NotContains(t, stored, "+09:00", "expected offset to be stripped, got %q", stored)
}

func TestDateFilterRespectsTimezoneEquivalence(t *testing.T) {
	// An article published 2024-01-15 02:00 in JST is 2024-01-14 17:00 UTC.
	// With --since 2024-01-15 (UTC), it should be excluded.
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "TZ", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	jst := time.FixedZone("JST", 9*3600)
	jan15JST := time.Date(2024, 1, 15, 2, 0, 0, 0, jst) // = 2024-01-14T17:00:00Z
	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "JST", URL: "https://example.com/jst", PublishedDate: &jan15JST})
	require.NoError(t, err, "add article")

	since := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	articles, err := db.ListArticles(ctx, ArticleFilter{Since: &since})
	require.NoError(t, err, "list articles")
	require.Empty(t, articles, "JST article published before UTC midnight Jan 15 should be excluded")
}

func TestMigrationNormalizesLegacyTimestampsToUTC(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "Legacy", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	_, err = sq.Insert("articles").
		Columns("blog_id", "title", "url", "published_date", "discovered_date").
		Values(blog.ID, "Legacy", "https://example.com/legacy",
			"2024-01-15T10:00:00+09:00",
			"2024-01-15 09:00:00").
		RunWith(db.conn).ExecContext(ctx)
	require.NoError(t, err, "insert legacy row")

	_, err = sq.Update("blogs").
		Set("last_scanned", "2024-01-15T10:00:00+09:00").
		Where(sq.Eq{"id": blog.ID}).
		RunWith(db.conn).ExecContext(ctx)
	require.NoError(t, err, "set legacy last_scanned")

	// Replay the actual migration SQL against the rows we just injected.
	sqlBytes, err := migrations.FS.ReadFile("000003_normalize_dates_utc.up.sql")
	require.NoError(t, err, "read migration file")
	_, err = db.conn.ExecContext(ctx, string(sqlBytes))
	require.NoError(t, err, "replay migration")

	var published, discovered, lastScanned string
	err = sq.Select("published_date", "discovered_date").From("articles").
		Where(sq.Eq{"url": "https://example.com/legacy"}).
		RunWith(db.conn).QueryRowContext(ctx).Scan(&published, &discovered)
	require.NoError(t, err)
	err = sq.Select("last_scanned").From("blogs").Where(sq.Eq{"id": blog.ID}).
		RunWith(db.conn).QueryRowContext(ctx).Scan(&lastScanned)
	require.NoError(t, err)

	require.Equal(t, "2024-01-15T01:00:00Z", published, "JST 10:00 should normalize to UTC 01:00")
	require.Equal(t, "2024-01-15T09:00:00Z", discovered, "space-separated form already UTC stays at same wall time")
	require.Equal(t, "2024-01-15T01:00:00Z", lastScanned)

	// Article should be readable back through the normal API.
	article, err := db.GetArticleByURL(ctx, "https://example.com/legacy")
	require.NoError(t, err)
	require.NotNil(t, article)
	require.NotNil(t, article.PublishedDate)
	require.Equal(t, time.Date(2024, 1, 15, 1, 0, 0, 0, time.UTC), article.PublishedDate.UTC())
}

func TestMigrationPreservesUnparseableTimestamps(t *testing.T) {
	// strftime() returns NULL on inputs it can't parse. COALESCE keeps the
	// original value so we surface bad data instead of silently destroying it.
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "Bad", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	const garbage = "not-a-timestamp"
	_, err = sq.Insert("articles").
		Columns("blog_id", "title", "url", "published_date").
		Values(blog.ID, "Bad", "https://example.com/bad", garbage).
		RunWith(db.conn).ExecContext(ctx)
	require.NoError(t, err, "insert bad row")

	sqlBytes, err := migrations.FS.ReadFile("000003_normalize_dates_utc.up.sql")
	require.NoError(t, err, "read migration file")
	_, err = db.conn.ExecContext(ctx, string(sqlBytes))
	require.NoError(t, err, "replay migration")

	var stored string
	err = sq.Select("published_date").From("articles").
		Where(sq.Eq{"url": "https://example.com/bad"}).
		RunWith(db.conn).QueryRowContext(ctx).Scan(&stored)
	require.NoError(t, err)
	require.Equal(t, garbage, stored, "unparseable timestamp should be preserved verbatim")
}

func TestListArticlesFilterBySearch(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "Test", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "Golang concurrency patterns", URL: "https://example.com/go-concurrency"})
	require.NoError(t, err, "add article")
	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "Rust memory safety", URL: "https://example.com/rust-memory"})
	require.NoError(t, err, "add article")
	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "Python async pitfalls", URL: "https://example.com/python-async"})
	require.NoError(t, err, "add article")

	t.Run("search by single word", func(t *testing.T) {
		articles, err := db.ListArticles(ctx, ArticleFilter{Search: "golang"})
		require.NoError(t, err)
		require.Len(t, articles, 1)
		require.Equal(t, "Golang concurrency patterns", articles[0].Title)
	})

	t.Run("search is case-insensitive", func(t *testing.T) {
		articles, err := db.ListArticles(ctx, ArticleFilter{Search: "GOLANG"})
		require.NoError(t, err)
		require.Len(t, articles, 1)
		require.Equal(t, "Golang concurrency patterns", articles[0].Title)
	})

	t.Run("search with no matches returns empty", func(t *testing.T) {
		articles, err := db.ListArticles(ctx, ArticleFilter{Search: "nonexistent"})
		require.NoError(t, err)
		require.Empty(t, articles)
	})

	t.Run("search combined with blog filter", func(t *testing.T) {
		blogID := blog.ID
		articles, err := db.ListArticles(ctx, ArticleFilter{Search: "rust", BlogID: &blogID})
		require.NoError(t, err)
		require.Len(t, articles, 1)
		require.Equal(t, "Rust memory safety", articles[0].Title)
	})

	t.Run("search with apostrophe does not need escaping", func(t *testing.T) {
		_, err := db.AddArticle(ctx, model.Article{
			BlogID:      blog.ID,
			Title:       "Don't fear the goroutine",
			URL:         "https://example.com/dont",
			Description: "You don't need to worry about goroutines",
		})
		require.NoError(t, err, "add article with apostrophe")

		articles, err := db.ListArticles(ctx, ArticleFilter{Search: "don't"})
		require.NoError(t, err)
		require.Len(t, articles, 1, "should find article with apostrophe in query")
		require.Equal(t, "Don't fear the goroutine", articles[0].Title)
	})

	t.Run("search matches body text", func(t *testing.T) {
		_, err := db.AddArticle(ctx, model.Article{
			BlogID:      blog.ID,
			Title:       "Deep dive into channels",
			URL:         "https://example.com/channels",
			Description: "This article discusses golang concurrency channels in detail",
		})
		require.NoError(t, err, "add article with body text")

		articles, err := db.ListArticles(ctx, ArticleFilter{Search: "channels"})
		require.NoError(t, err)
		require.Len(t, articles, 1, "should find article by body text match")
		require.Equal(t, "Deep dive into channels", articles[0].Title)
	})

	t.Run("search combined with unread filter", func(t *testing.T) {
		golangArticles, err := db.ListArticles(ctx, ArticleFilter{Search: "golang"})
		require.NoError(t, err)
		require.NotEmpty(t, golangArticles, "expected at least one article matching 'golang'")

		for _, a := range golangArticles {
			_, err = db.MarkArticleRead(ctx, a.ID)
			require.NoError(t, err)
		}

		articles, err := db.ListArticles(ctx, ArticleFilter{Search: "golang", UnreadOnly: true})
		require.NoError(t, err)
		require.Empty(t, articles, "expected no unread articles matching 'golang' after marking all matching articles as read")
	})
}

func TestListArticlesWithLimit(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer func() { require.NoError(t, db.Close()) }()

	blog, err := db.AddBlog(ctx, model.Blog{Name: "Test", URL: "https://example.com"})
	require.NoError(t, err, "add blog")

	t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 3, 10, 0, 0, 0, time.UTC)

	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "First", URL: "https://example.com/1", DiscoveredDate: &t1})
	require.NoError(t, err, "add article 1")
	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "Second", URL: "https://example.com/2", DiscoveredDate: &t2})
	require.NoError(t, err, "add article 2")
	_, err = db.AddArticle(ctx, model.Article{BlogID: blog.ID, Title: "Third", URL: "https://example.com/3", DiscoveredDate: &t3})
	require.NoError(t, err, "add article 3")

	t.Run("limit returns fewer results", func(t *testing.T) {
		articles, err := db.ListArticles(ctx, ArticleFilter{Limit: 2})
		require.NoError(t, err)
		require.Len(t, articles, 2)
	})

	t.Run("limit of 0 returns all", func(t *testing.T) {
		articles, err := db.ListArticles(ctx, ArticleFilter{Limit: 0})
		require.NoError(t, err)
		require.Len(t, articles, 3)
	})

	t.Run("limit with offset", func(t *testing.T) {
		articles, err := db.ListArticles(ctx, ArticleFilter{Limit: 1, Offset: 1})
		require.NoError(t, err)
		require.Len(t, articles, 1)
		require.Equal(t, "Second", articles[0].Title)
	})
}

func openTestDB(t *testing.T) *Database {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "blogwatcher-cli.db")
	db, err := OpenDatabase(ctx, path)
	require.NoError(t, err, "open database")
	return db
}
