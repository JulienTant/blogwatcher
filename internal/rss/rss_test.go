package rss

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const sampleFeed = `<?xml version="1.0" encoding="UTF-8" ?>
<rss version="2.0">
<channel>
<title>Example Feed</title>
<item>
<title>First</title>
<link>https://example.com/1</link>
<pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
</item>
<item>
<title>Second</title>
<link>https://example.com/2</link>
</item>
</channel>
</rss>`

const testUserAgent = "test-user-agent"

func newTestFetcher() *Fetcher {
	return NewFetcher(&http.Client{Timeout: 2 * time.Second}, testUserAgent)
}

func TestParseFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, writeErr := w.Write([]byte(sampleFeed)); writeErr != nil {
			http.Error(w, writeErr.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	articles, err := newTestFetcher().ParseFeed(context.Background(), server.URL)
	require.NoError(t, err, "parse feed")
	require.Len(t, articles, 2)
	require.NotNil(t, articles[0].PublishedDate)
}

func TestParseFeedSetsUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, testUserAgent, r.Header.Get("User-Agent"))
		if _, writeErr := w.Write([]byte(sampleFeed)); writeErr != nil {
			http.Error(w, writeErr.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	_, err := newTestFetcher().ParseFeed(context.Background(), server.URL)
	require.NoError(t, err, "parse feed")
}

func TestParseFeedWithCategories(t *testing.T) {
	feedWithCategories := `<?xml version="1.0" encoding="UTF-8" ?>
<rss version="2.0">
<channel>
<title>Example Feed</title>
<item>
<title>Tagged Post</title>
<link>https://example.com/tagged</link>
<category>AI</category>
<category>Machine Learning</category>
</item>
<item>
<title>Plain Post</title>
<link>https://example.com/plain</link>
</item>
</channel>
</rss>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, writeErr := w.Write([]byte(feedWithCategories)); writeErr != nil {
			http.Error(w, writeErr.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	articles, err := newTestFetcher().ParseFeed(context.Background(), server.URL)
	require.NoError(t, err, "parse feed")
	require.Len(t, articles, 2)

	require.Equal(t, []string{"AI", "Machine Learning"}, articles[0].Categories)
	require.Nil(t, articles[1].Categories)
}

func TestDiscoverFeedURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if _, writeErr := w.Write([]byte(`<html><head><link rel="alternate" type="application/rss+xml" href="/feed.xml" /></head></html>`)); writeErr != nil {
			http.Error(w, writeErr.Error(), http.StatusInternalServerError)
			return
		}
	})
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		if _, writeErr := w.Write([]byte(sampleFeed)); writeErr != nil {
			http.Error(w, writeErr.Error(), http.StatusInternalServerError)
			return
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	feedURL, err := newTestFetcher().DiscoverFeedURL(context.Background(), server.URL)
	require.NoError(t, err, "discover feed")
	require.NotEmpty(t, feedURL, "expected feed url")
}

func TestDiscoverFeedURL_XMLContentType(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/tag/AI/feed/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=UTF-8")
		_, writeErr := w.Write([]byte(sampleFeed))
		if writeErr != nil {
			http.Error(w, writeErr.Error(), http.StatusInternalServerError)
			return
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	feedURL, err := newTestFetcher().DiscoverFeedURL(context.Background(), server.URL+"/tag/AI/feed/")
	require.NoError(t, err)
	require.Equal(t, server.URL+"/tag/AI/feed/", feedURL, "should return URL directly for feed content-type")
}

func TestDiscoverFeedURL_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := newTestFetcher().DiscoverFeedURL(context.Background(), server.URL)
	require.Error(t, err, "should return error for 5xx")
	require.Contains(t, err.Error(), "server error status 503")
	require.True(t, IsFeedError(err), "should be a FeedParseError so it's retryable")
}

func TestDiscoverFeedURL_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	feedURL, err := newTestFetcher().DiscoverFeedURL(context.Background(), server.URL)
	require.NoError(t, err, "404 should not be an error")
	require.Empty(t, feedURL, "should return empty for 404")
}

func TestDiscoverFeedURL_RelSelf(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, writeErr := w.Write([]byte(`<html><head><link rel="self" type="application/rss+xml" href="/my-feed.xml" /></head></html>`))
		if writeErr != nil {
			http.Error(w, writeErr.Error(), http.StatusInternalServerError)
			return
		}
	})
	mux.HandleFunc("/my-feed.xml", func(w http.ResponseWriter, r *http.Request) {
		_, writeErr := w.Write([]byte(sampleFeed))
		if writeErr != nil {
			http.Error(w, writeErr.Error(), http.StatusInternalServerError)
			return
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	feedURL, err := newTestFetcher().DiscoverFeedURL(context.Background(), server.URL)
	require.NoError(t, err)
	require.Equal(t, server.URL+"/my-feed.xml", feedURL, "should discover feed from rel=self link")
}
