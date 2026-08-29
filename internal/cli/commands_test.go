package cli

import (
	"testing"
	"time"

	"github.com/JulienTant/blogwatcher-cli/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDateFilter(t *testing.T) {
	t.Run("empty string returns nil", func(t *testing.T) {
		got, err := parseDateFilter("")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("valid date", func(t *testing.T) {
		got, err := parseDateFilter("2024-01-15")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), *got)
	})

	t.Run("invalid format rejected", func(t *testing.T) {
		_, err := parseDateFilter("01/15/2024")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected YYYY-MM-DD")
	})

	t.Run("invalid month rejected", func(t *testing.T) {
		_, err := parseDateFilter("2024-13-01")
		require.Error(t, err)
	})
}

func TestParseDateRange(t *testing.T) {
	t.Run("both empty returns nils", func(t *testing.T) {
		since, before, err := parseDateRange("", "")
		require.NoError(t, err)
		assert.Nil(t, since)
		assert.Nil(t, before)
	})

	t.Run("only since", func(t *testing.T) {
		since, before, err := parseDateRange("2024-01-15", "")
		require.NoError(t, err)
		require.NotNil(t, since)
		assert.Nil(t, before)
		assert.Equal(t, time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), *since)
	})

	t.Run("only before", func(t *testing.T) {
		since, before, err := parseDateRange("", "2024-02-01")
		require.NoError(t, err)
		assert.Nil(t, since)
		require.NotNil(t, before)
		assert.Equal(t, time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), *before)
	})

	t.Run("since equals before is allowed", func(t *testing.T) {
		since, before, err := parseDateRange("2024-01-15", "2024-01-15")
		require.NoError(t, err)
		require.NotNil(t, since)
		require.NotNil(t, before)
	})

	t.Run("since before before is allowed", func(t *testing.T) {
		_, _, err := parseDateRange("2024-01-01", "2024-02-01")
		require.NoError(t, err)
	})

	t.Run("since after before is rejected", func(t *testing.T) {
		_, _, err := parseDateRange("2024-02-01", "2024-01-01")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--since")
		assert.Contains(t, err.Error(), "--before")
	})

	t.Run("invalid since surfaces error", func(t *testing.T) {
		_, _, err := parseDateRange("bogus", "2024-01-01")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected YYYY-MM-DD")
	})

	t.Run("invalid before surfaces error", func(t *testing.T) {
		_, _, err := parseDateRange("2024-01-01", "bogus")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected YYYY-MM-DD")
	})
}

func TestScanCommandUserAgentFlagDefaultsToDefault(t *testing.T) {
	cmd := newScanCommand()

	flag := cmd.Flags().Lookup("user-agent")
	require.NotNil(t, flag)
	assert.Equal(t, "blogwatcher-cli/"+version.Version+" (+https://github.com/JulienTant/blogwatcher-cli)", flag.DefValue)
	assert.Equal(t, defaultUserAgent(), flag.Value.String())
}

func TestResolveUserAgentRejectsEmptyValue(t *testing.T) {
	_, err := resolveUserAgent(" 	 ")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "user-agent")
}

func TestResolveUserAgentTrimsCustomValue(t *testing.T) {
	got, err := resolveUserAgent("  MyReader/1.0  ")

	require.NoError(t, err)
	assert.Equal(t, "MyReader/1.0", got)
}
