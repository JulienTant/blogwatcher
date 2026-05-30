package cli

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DataDog/go-secure-sdk/net/httpclient"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/JulienTant/blogwatcher-cli/internal/controller"
	"github.com/JulienTant/blogwatcher-cli/internal/model"
	"github.com/JulienTant/blogwatcher-cli/internal/rss"
	"github.com/JulienTant/blogwatcher-cli/internal/scanner"
	"github.com/JulienTant/blogwatcher-cli/internal/scraper"
	"github.com/JulienTant/blogwatcher-cli/internal/storage"
)

const httpTimeout = 30 * time.Second

func withDatabase(cmd *cobra.Command, fn func(db *storage.Database) error) error {
	db, err := storage.OpenDatabase(cmd.Context(), viper.GetString("db"))
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close db: %v\n", err)
		}
	}()
	return fn(db)
}

func newHTTPClient() *http.Client {
	if viper.GetBool("unsafe-client") {
		return httpclient.UnSafe(httpclient.WithTimeout(httpTimeout))
	}
	return httpclient.Safe(httpclient.WithTimeout(httpTimeout))
}

func newAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <name> <url>",
		Short: "Add a new blog to track.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			url := args[1]
			return withDatabase(cmd, func(db *storage.Database) error {
				_, err := controller.AddBlog(cmd.Context(), db, name, url, viper.GetString("feed-url"), viper.GetString("scrape-selector"))
				if err != nil {
					printError(err)
					return markError(err)
				}
				cprintf([]color.Attribute{color.FgGreen}, "Added blog '%s'\n", name)
				return nil
			})
		},
	}
	cmd.Flags().String("feed-url", "", "RSS/Atom feed URL (auto-discovered if not provided)")
	cmd.Flags().String("scrape-selector", "", "CSS selector for HTML scraping fallback")
	return cmd
}

func newRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a blog from tracking.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !viper.GetBool("yes") {
				confirmed, err := confirm(fmt.Sprintf("Remove blog '%s' and all its articles?", name))
				if err != nil {
					return err
				}
				if !confirmed {
					return nil
				}
			}
			return withDatabase(cmd, func(db *storage.Database) error {
				if err := controller.RemoveBlog(cmd.Context(), db, name); err != nil {
					printError(err)
					return markError(err)
				}
				cprintf([]color.Attribute{color.FgGreen}, "Removed blog '%s'\n", name)
				return nil
			})
		},
	}
	cmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func newBlogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blogs",
		Short: "List all tracked blogs.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDatabase(cmd, func(db *storage.Database) error {
				blogs, err := db.ListBlogs(cmd.Context())
				if err != nil {
					return err
				}
				if len(blogs) == 0 {
					fmt.Println("No blogs tracked yet. Use 'blogwatcher-cli add' to add one.")
					return nil
				}
				cprintf([]color.Attribute{color.FgCyan, color.Bold}, "Tracked blogs (%d):\n\n", len(blogs))
				for _, blog := range blogs {
					cprintf([]color.Attribute{color.FgWhite, color.Bold}, "  %s\n", blog.Name)
					fmt.Printf("    URL: %s\n", blog.URL)
					if blog.FeedURL != "" {
						fmt.Printf("    Feed: %s\n", blog.FeedURL)
					}
					if blog.ScrapeSelector != "" {
						fmt.Printf("    Selector: %s\n", blog.ScrapeSelector)
					}
					if blog.LastScanned != nil {
						fmt.Printf("    Last scanned: %s\n", blog.LastScanned.Format("2006-01-02 15:04"))
					}
					fmt.Println()
				}
				return nil
			})
		},
	}
	return cmd
}

func newScanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan [blog_name]",
		Short: "Scan blogs for new articles.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			silent := viper.GetBool("silent")
			workers := viper.GetInt("workers")

			return withDatabase(cmd, func(db *storage.Database) error {
				client := newHTTPClient()
				sc := scanner.NewScanner(rss.NewFetcher(client), scraper.NewScraper(client))

				if len(args) == 1 {
					result, err := sc.ScanBlogByName(cmd.Context(), db, args[0])
					if err != nil {
						return err
					}
					if result == nil {
						err := fmt.Errorf("blog '%s' not found", args[0])
						printError(err)
						return markError(err)
					}
					if !silent {
						printScanResult(*result)
					}
				} else {
					blogs, err := db.ListBlogs(cmd.Context())
					if err != nil {
						return err
					}
					if len(blogs) == 0 {
						fmt.Println("No blogs tracked yet. Use 'blogwatcher-cli add' to add one.")
						return nil
					}
					if !silent {
						cprintf([]color.Attribute{color.FgCyan}, "Scanning %d blog(s)...\n\n", len(blogs))
					}
					results, err := sc.ScanAllBlogs(cmd.Context(), db, workers)
					if err != nil {
						return err
					}
					totalNew := 0
					failed := 0
					for _, result := range results {
						if !silent {
							printScanResult(result)
						}
						if result.Error != "" {
							failed++
						} else {
							totalNew += result.NewArticles
						}
					}
					if !silent {
						fmt.Println()
						succeeded := len(results) - failed
						if failed > 0 {
							cprintf([]color.Attribute{color.FgYellow}, "Scanned %d blog(s): %d succeeded, %d failed\n", len(results), succeeded, failed)
						}
						if totalNew > 0 {
							cprintf([]color.Attribute{color.FgGreen, color.Bold}, "Found %d new article(s) total!\n", totalNew)
						} else if failed == 0 {
							cprintln([]color.Attribute{color.FgYellow}, "No new articles found.")
						}
					} else {
						if failed > 0 {
							fmt.Fprintf(os.Stderr, "scan: %d/%d blog(s) failed\n", failed, len(results))
						}
						if failed == len(results) {
							return fmt.Errorf("scan failed: all %d blog(s) failed", failed)
						}
					}
				}

				if silent {
					fmt.Println("scan done")
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolP("silent", "s", false, "Only output 'scan done' when complete")
	cmd.Flags().IntP("workers", "w", 8, "Number of concurrent workers when scanning all blogs")
	return cmd
}

func newArticlesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "articles",
		Short: "List articles.",
		RunE: func(cmd *cobra.Command, args []string) error {
			showAll := viper.GetBool("all")

			since, before, err := parseDateRange(viper.GetString("since"), viper.GetString("before"))
			if err != nil {
				printError(err)
				return markError(err)
			}

			limit := viper.GetInt("limit")
			if limit < 0 {
				err := fmt.Errorf("invalid --limit: %d (must be >= 0)", limit)
				printError(err)
				return markError(err)
			}
			if limit > storage.MaxListLimit {
				err := fmt.Errorf("invalid --limit: %d (maximum is %d)", limit, storage.MaxListLimit)
				printError(err)
				return markError(err)
			}

			return withDatabase(cmd, func(db *storage.Database) error {
				filter := storage.ArticleFilter{
					UnreadOnly: !showAll,
					Category:   stringPtr(viper.GetString("category")),
					Since:      since,
					Before:     before,
					Search:     viper.GetString("search"),
					Limit:      limit,
				}

				blogID, err := resolveBlogID(cmd.Context(), db, viper.GetString("blog"))
				if err != nil {
					return err
				}
				filter.BlogID = blogID

				articles, blogNames, err := controller.GetArticles(cmd.Context(), db, filter)
				if err != nil {
					printError(err)
					return markError(err)
				}
				if len(articles) == 0 {
					if viper.GetString("search") != "" {
						cprintf([]color.Attribute{color.FgYellow}, "No articles matching '%s'.\n", viper.GetString("search"))
					} else if showAll {
						fmt.Println("No articles found.")
					} else {
						cprintln([]color.Attribute{color.FgGreen}, "No unread articles!")
					}
					return nil
				}

				label := "Unread articles"
				if showAll {
					label = "All articles"
				}
				if viper.GetString("search") != "" {
					label = fmt.Sprintf("Search results for '%s'", viper.GetString("search"))
				}
				cprintf([]color.Attribute{color.FgCyan, color.Bold}, "%s (%d):\n\n", label, len(articles))
				for _, article := range articles {
					printArticle(article, blogNames[article.BlogID])
				}
				return nil
			})
		},
	}

	cmd.Flags().BoolP("all", "a", false, "Show all articles (including read)")
	cmd.Flags().StringP("blog", "b", "", "Filter by blog name")
	cmd.Flags().StringP("category", "c", "", "Filter by category")
	cmd.Flags().String("since", "", "Show articles published on or after YYYY-MM-DD")
	cmd.Flags().String("before", "", "Show articles published before YYYY-MM-DD")
	cmd.Flags().StringP("search", "s", "", "Search articles by title or content (FTS5 full-text search)")
	cmd.Flags().IntP("limit", "n", 20, "Maximum number of articles to return")
	return cmd
}

func newReadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read <article_id>",
		Short: "Mark an article as read.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			articleID, err := parseID(args[0])
			if err != nil {
				return err
			}
			return withDatabase(cmd, func(db *storage.Database) error {
				article, err := controller.MarkArticleRead(cmd.Context(), db, articleID)
				if err != nil {
					printError(err)
					return markError(err)
				}
				if article.IsRead {
					fmt.Printf("Article %d is already marked as read.\n", articleID)
				} else {
					cprintf([]color.Attribute{color.FgGreen}, "Marked article %d as read\n", articleID)
				}
				return nil
			})
		},
	}
	return cmd
}

func newReadAllCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read-all",
		Short: "Mark all unread articles as read.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDatabase(cmd, func(db *storage.Database) error {
				filter := storage.ArticleFilter{
					UnreadOnly: true,
				}
				blogID, err := resolveBlogID(cmd.Context(), db, viper.GetString("blog"))
				if err != nil {
					return err
				}
				filter.BlogID = blogID

				articles, _, err := controller.GetArticles(cmd.Context(), db, filter)
				if err != nil {
					printError(err)
					return markError(err)
				}
				if len(articles) == 0 {
					cprintln([]color.Attribute{color.FgGreen}, "No unread articles to mark as read.")
					return nil
				}

				if !viper.GetBool("yes") {
					scope := "all blogs"
					if blog := viper.GetString("blog"); blog != "" {
						scope = fmt.Sprintf("from '%s'", blog)
					}
					confirmed, err := confirm(fmt.Sprintf("Mark %d article(s) %s as read?", len(articles), scope))
					if err != nil {
						return err
					}
					if !confirmed {
						return nil
					}
				}

				marked, err := controller.MarkAllArticlesRead(cmd.Context(), db, filter)
				if err != nil {
					printError(err)
					return markError(err)
				}

				cprintf([]color.Attribute{color.FgGreen}, "Marked %d article(s) as read\n", len(marked))
				return nil
			})
		},
	}

	cmd.Flags().StringP("blog", "b", "", "Only mark articles from this blog")
	cmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func newUnreadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unread <article_id>",
		Short: "Mark an article as unread.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			articleID, err := parseID(args[0])
			if err != nil {
				return err
			}
			return withDatabase(cmd, func(db *storage.Database) error {
				article, err := controller.MarkArticleUnread(cmd.Context(), db, articleID)
				if err != nil {
					printError(err)
					return markError(err)
				}
				if !article.IsRead {
					fmt.Printf("Article %d is already marked as unread.\n", articleID)
				} else {
					cprintf([]color.Attribute{color.FgGreen}, "Marked article %d as unread\n", articleID)
				}
				return nil
			})
		},
	}
	return cmd
}

func newImportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Import blogs from an OPML file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer func() {
				if err := f.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "close file: %v\n", err)
				}
			}()
			return withDatabase(cmd, func(db *storage.Database) error {
				added, skipped, err := controller.ImportOPML(cmd.Context(), db, f)
				if err != nil {
					printError(err)
					return markError(err)
				}
				cprintf([]color.Attribute{color.FgGreen}, "Imported %d blog(s), skipped %d duplicate(s)\n", added, skipped)
				return nil
			})
		},
	}
	return cmd
}

func printScanResult(result scanner.ScanResult) {
	cprintf([]color.Attribute{color.FgWhite, color.Bold}, "  %s\n", result.BlogName)
	if result.Error != "" {
		cprintf([]color.Attribute{color.FgRed}, "    Error: %s\n", result.Error)
		return
	}
	if result.Source == "none" {
		cprintln([]color.Attribute{color.FgYellow}, "    No feed or scraper configured")
		return
	}
	statusColor := []color.Attribute{color.FgWhite}
	if result.NewArticles > 0 {
		statusColor = []color.Attribute{color.FgGreen}
	}
	sourceLabel := "HTML"
	if result.Source == "rss" {
		sourceLabel = "RSS"
	}
	fmt.Printf("    Source: %s | Found: %d | ", sourceLabel, result.TotalFound)
	cprintf(statusColor, "New: %d\n", result.NewArticles)
}

func printArticle(article model.Article, blogName string) {
	status := csprint([]color.Attribute{color.FgYellow}, "[new]")
	if article.IsRead {
		status = csprint([]color.Attribute{color.FgHiBlack}, "[read]")
	}
	idStr := csprintf([]color.Attribute{color.FgCyan}, "[%d]", article.ID)
	fmt.Printf("  %s %s %s\n", idStr, status, article.Title)
	fmt.Printf("       Blog: %s\n", blogName)
	fmt.Printf("       URL: %s\n", article.URL)
	if article.PublishedDate != nil {
		fmt.Printf("       Published: %s\n", article.PublishedDate.Format("2006-01-02"))
	}
	if len(article.Categories) > 0 {
		fmt.Printf("       Categories: %s\n", strings.Join(article.Categories, ", "))
	}
	fmt.Println()
}

func printError(err error) {
	cprintfErr(color.FgRed, "Error: %s\n", err.Error())
}

func cprintf(attrs []color.Attribute, format string, a ...any) {
	if _, err := color.New(attrs...).Printf(format, a...); err != nil {
		fmt.Printf(format, a...)
	}
}

func cprintfErr(attr color.Attribute, format string, a ...any) {
	if _, err := color.New(attr).Fprintf(os.Stderr, format, a...); err != nil {
		fmt.Fprintf(os.Stderr, format, a...)
	}
}

func cprintln(attrs []color.Attribute, msg string) {
	if _, err := color.New(attrs...).Println(msg); err != nil {
		fmt.Println(msg)
	}
}

func csprint(attrs []color.Attribute, a ...any) string {
	return color.New(attrs...).Sprint(a...)
}

func csprintf(attrs []color.Attribute, format string, a ...any) string {
	return color.New(attrs...).Sprintf(format, a...)
}

func parseID(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid article id: %s", value)
	}
	return parsed, nil
}

func parseDateFilter(dateStr string) (*time.Time, error) {
	if dateStr == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, fmt.Errorf("invalid date format: %s, expected YYYY-MM-DD", dateStr)
	}
	return &parsed, nil
}

func parseDateRange(sinceStr, beforeStr string) (*time.Time, *time.Time, error) {
	since, err := parseDateFilter(sinceStr)
	if err != nil {
		return nil, nil, err
	}
	before, err := parseDateFilter(beforeStr)
	if err != nil {
		return nil, nil, err
	}
	if since != nil && before != nil && since.After(*before) {
		return nil, nil, fmt.Errorf("--since (%s) must be on or before --before (%s)", sinceStr, beforeStr)
	}
	return since, before, nil
}

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func resolveBlogID(ctx context.Context, db *storage.Database, blogName string) (*int64, error) {
	if blogName == "" {
		return nil, nil
	}
	blog, err := db.GetBlogByName(ctx, blogName)
	if err != nil {
		return nil, err
	}
	if blog == nil {
		err := fmt.Errorf("blog '%s' not found", blogName)
		printError(err)
		return nil, markError(err)
	}
	return &blog.ID, nil
}

func confirm(prompt string) (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [y/N]: ", prompt)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes", nil
}

func init() {
	cobra.EnableCommandSorting = false
	cobra.AddTemplateFunc("now", func() string { return time.Now().Format(time.RFC3339) })
}
