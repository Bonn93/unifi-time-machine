package video

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"time-machine/pkg/config"
	"time-machine/pkg/database"
	"time-machine/pkg/services/settings"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupHLSTest(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	config.AppConfig.DataDir = tempDir
	database.InitDB()
	settings.Init()
	return tempDir
}

func TestParseHLSQualities_Default(t *testing.T) {
	setupHLSTest(t)
	settings.Set("video.quality", "medium")
	settings.Invalidate()

	qs := parseHLSQualities("source,720p")
	require.Len(t, qs, 2)

	assert.Equal(t, "source", qs[0].Label)
	assert.Equal(t, 0, qs[0].Height, "source height should be 0 (pass-through)")
	assert.Equal(t, 28, qs[0].CRF, "medium quality = CRF 28")

	assert.Equal(t, "720p", qs[1].Label)
	assert.Equal(t, 720, qs[1].Height)
	assert.Equal(t, 30, qs[1].CRF, "720p adds +2 to base CRF 28")
}

func TestParseHLSQualities_Single(t *testing.T) {
	setupHLSTest(t)
	qs := parseHLSQualities("source")
	require.Len(t, qs, 1)
	assert.Equal(t, "source", qs[0].Label)
	assert.Equal(t, 4_000_000, qs[0].Bandwidth)
}

func TestParseHLSQualities_Empty(t *testing.T) {
	setupHLSTest(t)
	qs := parseHLSQualities("")
	require.Len(t, qs, 1, "empty string should fall back to source-only quality")
	assert.Equal(t, "source", qs[0].Label)
}

func TestParseHLSQualities_AllLevels(t *testing.T) {
	setupHLSTest(t)
	qs := parseHLSQualities("source,1080p,720p,480p")
	require.Len(t, qs, 4)
	assert.Equal(t, 0, qs[0].Height)
	assert.Equal(t, 1080, qs[1].Height)
	assert.Equal(t, 720, qs[2].Height)
	assert.Equal(t, 480, qs[3].Height)
}

func TestWriteMasterPlaylist(t *testing.T) {
	tempDir := t.TempDir()
	qs := []HLSQuality{
		{Label: "720p", Height: 720, CRF: 30, Bandwidth: 2_000_000},
		{Label: "source", Height: 0, CRF: 28, Bandwidth: 4_000_000},
	}

	err := writeMasterPlaylist(tempDir, qs)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tempDir, "master.m3u8"))
	require.NoError(t, err)

	text := string(content)
	assert.True(t, strings.HasPrefix(text, "#EXTM3U"), "must start with #EXTM3U")
	assert.Contains(t, text, "#EXT-X-VERSION:3")
	assert.Contains(t, text, "720p/index.m3u8")
	assert.Contains(t, text, "source/index.m3u8")
	assert.Contains(t, text, "BANDWIDTH=2000000")
	assert.Contains(t, text, "RESOLUTION=1280x720")
	// Source entry should not carry a RESOLUTION tag — verify source line
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "NAME=\"source\"") {
			assert.NotContains(t, line, "RESOLUTION=", "source EXT-X-STREAM-INF must not have RESOLUTION")
		}
	}
}
