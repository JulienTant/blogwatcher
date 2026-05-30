package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"

	"github.com/JulienTant/blogwatcher-cli/internal/model"
	"github.com/JulienTant/blogwatcher-cli/internal/storage/migrations"
)

const (
	// sqliteTimeLayout is the layout used when parsing timestamps read from
	// the database. RFC3339Nano accepts both fractional and non-fractional
	// forms so legacy rows continue to round-trip.
	sqliteTimeLayout = time.RFC3339Nano

	// sqliteWriteLayout is the canonical layout used when persisting
	// timestamps. Fixed-width, UTC, no fractional seconds — required so the
	// --since/--before lexicographic comparison in ListArticles is stable
	// regardless of the source precision.
	sqliteWriteLayout = "2006-01-02T15:04:05Z"

	// MaxListLimit is the maximum number of articles a caller may request via
	// ArticleFilter.Limit. Callers that exceed it should be rejected by the
	// CLI layer, not silently clamped.
	MaxListLimit = 100
)

func DefaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".blogwatcher-cli", "blogwatcher-cli.db"), nil
}

type Database struct {
	path string
	conn *sql.DB
}

type ArticleFilter struct {
	UnreadOnly bool
	BlogID     *int64
	Category   *string
	Since      *time.Time
	Before     *time.Time
	Search     string
	Limit      int
	Offset     int
}

func OpenDatabase(ctx context.Context, path string) (*Database, error) {
	if path == "" {
		var err error
		path, err = DefaultDBPath()
		if err != nil {
			return nil, err
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)

	db := &Database{path: path, conn: conn}
	if err := db.migrate(); err != nil {
		if cerr := conn.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "close: %v\n", cerr)
		}
		return nil, err
	}
	return db, nil
}

func (db *Database) Path() string {
	return db.path
}

func (db *Database) Close() error {
	if db.conn == nil {
		return nil
	}
	return db.conn.Close()
}

func (db *Database) migrate() error {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	driver, err := migratesqlite.WithInstance(db.conn, &migratesqlite.Config{})
	if err != nil {
		return fmt.Errorf("create migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "sqlite", driver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

// Blog operations

func (db *Database) AddBlog(ctx context.Context, blog model.Blog) (model.Blog, error) {
	result, err := sq.Insert("blogs").
		Columns("name", "url", "feed_url", "scrape_selector", "last_scanned").
		Values(blog.Name, blog.URL, nullIfEmpty(blog.FeedURL), nullIfEmpty(blog.ScrapeSelector), formatTimePtr(blog.LastScanned)).
		RunWith(db.conn).
		ExecContext(ctx)
	if err != nil {
		return blog, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return blog, err
	}
	blog.ID = id
	return blog, nil
}

func (db *Database) GetBlog(ctx context.Context, id int64) (*model.Blog, error) {
	row := sq.Select("id", "name", "url", "feed_url", "scrape_selector", "last_scanned").
		From("blogs").
		Where(sq.Eq{"id": id}).
		RunWith(db.conn).
		QueryRowContext(ctx)
	return scanBlog(row)
}

func (db *Database) GetBlogByName(ctx context.Context, name string) (*model.Blog, error) {
	row := sq.Select("id", "name", "url", "feed_url", "scrape_selector", "last_scanned").
		From("blogs").
		Where(sq.Eq{"name": name}).
		RunWith(db.conn).
		QueryRowContext(ctx)
	return scanBlog(row)
}

func (db *Database) GetBlogByURL(ctx context.Context, url string) (*model.Blog, error) {
	row := sq.Select("id", "name", "url", "feed_url", "scrape_selector", "last_scanned").
		From("blogs").
		Where(sq.Eq{"url": url}).
		RunWith(db.conn).
		QueryRowContext(ctx)
	return scanBlog(row)
}

func (db *Database) ListBlogs(ctx context.Context) ([]model.Blog, error) {
	rows, err := sq.Select("id", "name", "url", "feed_url", "scrape_selector", "last_scanned").
		From("blogs").
		OrderBy("name").
		RunWith(db.conn).
		QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close rows: %v\n", err)
		}
	}()

	var blogs []model.Blog
	for rows.Next() {
		blog, err := scanBlog(rows)
		if err != nil {
			return nil, err
		}
		if blog != nil {
			blogs = append(blogs, *blog)
		}
	}
	return blogs, rows.Err()
}

func (db *Database) UpdateBlog(ctx context.Context, blog model.Blog) error {
	_, err := sq.Update("blogs").
		Set("name", blog.Name).
		Set("url", blog.URL).
		Set("feed_url", nullIfEmpty(blog.FeedURL)).
		Set("scrape_selector", nullIfEmpty(blog.ScrapeSelector)).
		Set("last_scanned", formatTimePtr(blog.LastScanned)).
		Where(sq.Eq{"id": blog.ID}).
		RunWith(db.conn).
		ExecContext(ctx)
	return err
}

func (db *Database) UpdateBlogLastScanned(ctx context.Context, id int64, lastScanned time.Time) error {
	_, err := sq.Update("blogs").
		Set("last_scanned", lastScanned.UTC().Format(sqliteWriteLayout)).
		Where(sq.Eq{"id": id}).
		RunWith(db.conn).
		ExecContext(ctx)
	return err
}

func (db *Database) RemoveBlog(ctx context.Context, id int64) (bool, error) {
	_, err := sq.Delete("articles").
		Where(sq.Eq{"blog_id": id}).
		RunWith(db.conn).
		ExecContext(ctx)
	if err != nil {
		return false, err
	}
	result, err := sq.Delete("blogs").
		Where(sq.Eq{"id": id}).
		RunWith(db.conn).
		ExecContext(ctx)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// Article operations

func (db *Database) AddArticle(ctx context.Context, article model.Article) (model.Article, error) {
	cats, err := categoriesToJSON(article.Categories)
	if err != nil {
		return article, err
	}
	result, err := sq.Insert("articles").
		Columns("blog_id", "title", "url", "published_date", "discovered_date", "is_read", "categories", "description", "content").
		Values(article.BlogID, article.Title, article.URL, formatTimePtr(article.PublishedDate), formatTimePtr(article.DiscoveredDate), article.IsRead, cats, nullIfEmpty(article.Description), nullIfEmpty(article.Content)).
		RunWith(db.conn).
		ExecContext(ctx)
	if err != nil {
		return article, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return article, err
	}
	article.ID = id
	return article, nil
}

func (db *Database) AddArticlesBulk(ctx context.Context, articles []model.Article) (int, error) {
	if len(articles) == 0 {
		return 0, nil
	}
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}

	insert := sq.Insert("articles").
		Columns("blog_id", "title", "url", "published_date", "discovered_date", "is_read", "categories", "description", "content")
	for _, article := range articles {
		cats, err := categoriesToJSON(article.Categories)
		if err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				fmt.Fprintf(os.Stderr, "rollback: %v\n", rerr)
			}
			return 0, err
		}
		insert = insert.Values(
			article.BlogID,
			article.Title,
			article.URL,
			formatTimePtr(article.PublishedDate),
			formatTimePtr(article.DiscoveredDate),
			article.IsRead,
			cats,
			nullIfEmpty(article.Description),
			nullIfEmpty(article.Content),
		)
	}

	_, err = insert.RunWith(tx).ExecContext(ctx)
	if err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			fmt.Fprintf(os.Stderr, "rollback: %v\n", rerr)
		}
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(articles), nil
}

func (db *Database) GetArticle(ctx context.Context, id int64) (*model.Article, error) {
	row := sq.Select("id", "blog_id", "title", "url", "published_date", "discovered_date", "is_read", "categories", "description", "content").
		From("articles").
		Where(sq.Eq{"id": id}).
		RunWith(db.conn).
		QueryRowContext(ctx)
	return scanArticle(row)
}

func (db *Database) GetArticleByURL(ctx context.Context, url string) (*model.Article, error) {
	row := sq.Select("id", "blog_id", "title", "url", "published_date", "discovered_date", "is_read", "categories", "description", "content").
		From("articles").
		Where(sq.Eq{"url": url}).
		RunWith(db.conn).
		QueryRowContext(ctx)
	return scanArticle(row)
}

func (db *Database) ArticleExists(ctx context.Context, url string) (bool, error) {
	row := sq.Select("1").
		From("articles").
		Where(sq.Eq{"url": url}).
		RunWith(db.conn).
		QueryRowContext(ctx)
	var one int
	switch err := row.Scan(&one); {
	case err == nil:
		return true, nil
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	default:
		return false, err
	}
}

func (db *Database) GetExistingArticleURLs(ctx context.Context, urls []string) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	if len(urls) == 0 {
		return result, nil
	}

	chunkSize := 900
	for start := 0; start < len(urls); start += chunkSize {
		end := min(start+chunkSize, len(urls))
		chunk := urls[start:end]

		rows, err := sq.Select("url").
			From("articles").
			Where(sq.Eq{"url": chunk}).
			RunWith(db.conn).
			QueryContext(ctx)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var url string
			if err := rows.Scan(&url); err != nil {
				if cerr := rows.Close(); cerr != nil {
					fmt.Fprintf(os.Stderr, "close: %v\n", cerr)
				}
				return nil, err
			}
			result[url] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			if cerr := rows.Close(); cerr != nil {
				fmt.Fprintf(os.Stderr, "close: %v\n", cerr)
			}
			return nil, err
		}
		if err := rows.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close: %v\n", err)
		}
	}
	return result, nil
}

func (db *Database) ListArticles(ctx context.Context, filter ArticleFilter) ([]model.Article, error) {
	if filter.Search != "" {
		return db.searchArticles(ctx, filter)
	}

	query := sq.Select("id", "blog_id", "title", "url", "published_date", "discovered_date", "is_read", "categories", "description", "content").
		From("articles").
		OrderBy("discovered_date DESC")

	if filter.UnreadOnly {
		query = query.Where(sq.Eq{"is_read": false})
	}
	if filter.BlogID != nil {
		query = query.Where(sq.Eq{"blog_id": *filter.BlogID})
	}
	if filter.Category != nil && *filter.Category != "" {
		query = query.Where("EXISTS (SELECT 1 FROM json_each(categories) WHERE LOWER(json_each.value) = LOWER(?))", *filter.Category)
	}
	if filter.Since != nil {
		query = query.Where(sq.GtOrEq{"published_date": filter.Since.UTC().Format(sqliteWriteLayout)})
	}
	if filter.Before != nil {
		query = query.Where(sq.Lt{"published_date": filter.Before.UTC().Format(sqliteWriteLayout)})
	}
	if filter.Limit > 0 {
		query = query.Limit(uint64(filter.Limit))
	}
	if filter.Offset > 0 {
		query = query.Offset(uint64(filter.Offset))
	}

	rows, err := query.RunWith(db.conn).QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close rows: %v\n", err)
		}
	}()

	var articles []model.Article
	for rows.Next() {
		article, err := scanArticle(rows)
		if err != nil {
			return nil, err
		}
		if article != nil {
			articles = append(articles, *article)
		}
	}
	return articles, rows.Err()
}

func (db *Database) searchArticles(ctx context.Context, filter ArticleFilter) ([]model.Article, error) {
	query := sq.Select("a.id", "a.blog_id", "a.title", "a.url", "a.published_date", "a.discovered_date", "a.is_read", "a.categories", "a.description", "a.content").
		From("articles a").
		Join("articles_fts f ON a.id = f.rowid").
		Where("articles_fts MATCH ?", escapeFTS5Query(filter.Search)).
		OrderBy("f.rank")

	if filter.UnreadOnly {
		query = query.Where(sq.Eq{"a.is_read": false})
	}
	if filter.BlogID != nil {
		query = query.Where(sq.Eq{"a.blog_id": *filter.BlogID})
	}
	if filter.Category != nil && *filter.Category != "" {
		query = query.Where("EXISTS (SELECT 1 FROM json_each(a.categories) WHERE LOWER(json_each.value) = LOWER(?))", *filter.Category)
	}
	if filter.Since != nil {
		query = query.Where(sq.GtOrEq{"a.published_date": filter.Since.UTC().Format(sqliteWriteLayout)})
	}
	if filter.Before != nil {
		query = query.Where(sq.Lt{"a.published_date": filter.Before.UTC().Format(sqliteWriteLayout)})
	}

	if filter.Limit > 0 {
		query = query.Limit(uint64(filter.Limit))
	}
	if filter.Offset > 0 {
		query = query.Offset(uint64(filter.Offset))
	}

	rows, err := query.RunWith(db.conn).QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close rows: %v\n", err)
		}
	}()

	var articles []model.Article
	for rows.Next() {
		article, err := scanArticle(rows)
		if err != nil {
			return nil, err
		}
		if article != nil {
			articles = append(articles, *article)
		}
	}
	return articles, rows.Err()
}

// escapeFTS5Query prepares a user-supplied search string for use in an FTS5
// MATCH clause. Words containing characters that are not valid in bare FTS5
// tokens (single quotes, double quotes) are wrapped in double quotes so they
// are treated as phrase tokens rather than causing a syntax error.
func escapeFTS5Query(q string) string {
	words := strings.Fields(q)
	for i, w := range words {
		if strings.ContainsAny(w, "'\"") {
			words[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
		}
	}
	return strings.Join(words, " ")
}

func (db *Database) MarkArticleRead(ctx context.Context, id int64) (bool, error) {
	result, err := sq.Update("articles").
		Set("is_read", true).
		Where(sq.Eq{"id": id}).
		RunWith(db.conn).
		ExecContext(ctx)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (db *Database) MarkArticlesRead(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := sq.Update("articles").
		Set("is_read", true).
		Where(sq.Eq{"id": ids}).
		RunWith(db.conn).
		ExecContext(ctx)
	return err
}

func (db *Database) MarkArticleUnread(ctx context.Context, id int64) (bool, error) {
	result, err := sq.Update("articles").
		Set("is_read", false).
		Where(sq.Eq{"id": id}).
		RunWith(db.conn).
		ExecContext(ctx)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// Scan helpers

func scanBlog(scanner interface{ Scan(dest ...any) error }) (*model.Blog, error) {
	var (
		id             int64
		name           string
		url            string
		feedURL        sql.NullString
		scrapeSelector sql.NullString
		lastScanned    sql.NullString
	)
	if err := scanner.Scan(&id, &name, &url, &feedURL, &scrapeSelector, &lastScanned); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	blog := &model.Blog{
		ID:             id,
		Name:           name,
		URL:            url,
		FeedURL:        feedURL.String,
		ScrapeSelector: scrapeSelector.String,
	}
	if lastScanned.Valid {
		if parsed, err := parseTime(lastScanned.String); err == nil {
			blog.LastScanned = &parsed
		}
	}
	return blog, nil
}

func scanArticle(scanner interface{ Scan(dest ...any) error }) (*model.Article, error) {
	var (
		id            int64
		blogID        int64
		title         string
		url           string
		publishedDate sql.NullString
		discovered    sql.NullString
		isRead        bool
		categories    sql.NullString
		description   sql.NullString
		content       sql.NullString
	)
	if err := scanner.Scan(&id, &blogID, &title, &url, &publishedDate, &discovered, &isRead, &categories, &description, &content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	cats, err := categoriesFromJSON(categories)
	if err != nil {
		return nil, err
	}

	article := &model.Article{
		ID:          id,
		BlogID:      blogID,
		Title:       title,
		URL:         url,
		IsRead:      isRead,
		Categories:  cats,
		Description: description.String,
		Content:     content.String,
	}
	if publishedDate.Valid {
		if parsed, err := parseTime(publishedDate.String); err == nil {
			article.PublishedDate = &parsed
		}
	}
	if discovered.Valid {
		if parsed, err := parseTime(discovered.String); err == nil {
			article.DiscoveredDate = &parsed
		}
	}

	return article, nil
}

// Value helpers

func formatTimePtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(sqliteWriteLayout)
	return &formatted
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("empty time")
	}
	parsed, err := time.Parse(sqliteTimeLayout, value)
	if err == nil {
		return parsed, nil
	}
	return time.Parse("2006-01-02 15:04:05", value)
}

func nullIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func categoriesToJSON(categories []string) (*string, error) {
	if len(categories) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(categories)
	if err != nil {
		return nil, fmt.Errorf("marshal categories: %w", err)
	}
	s := string(b)
	return &s, nil
}

func categoriesFromJSON(s sql.NullString) ([]string, error) {
	if !s.Valid || s.String == "" {
		return nil, nil
	}
	var cats []string
	if err := json.Unmarshal([]byte(s.String), &cats); err != nil {
		return nil, fmt.Errorf("unmarshal categories: %w", err)
	}
	return cats, nil
}
