package settings

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"time-machine/pkg/database"
)

// SeedEntry describes a single operational setting with its env-var source and default.
type SeedEntry struct {
	Key    string
	EnvVar string
	DefVal string
}

// KnownSettings lists every operational setting, its optional env-var source, and its default.
var KnownSettings = []SeedEntry{
	{"snapshot.interval_sec", "TIMELAPSE_INTERVAL", "3600"},
	{"video.cron_interval_sec", "VIDEO_CRON_INTERVAL", "300"},
	{"video.format", "VIDEO_FORMAT", "webm"},
	{"video.quality", "VIDEO_QUALITY", "medium"},
	{"video.max_bitrate", "VIDEO_MAX_BITRATE", "2M"},
	{"video.encoder_preset", "", "fast"},
	{"video.hls_segment_sec", "", "4"},
	{"video.hls_qualities", "", "source,720p"},
	{"video.daily_days", "DAYS_OF_24_HOUR_SNAPSHOTS", "30"},
	{"snapshot.retention_days", "SNAPSHOT_RETENTION_DAYS", "30"},
	{"gallery.retention_days", "GALLERY_RETENTION_DAYS", "365"},
	{"share.link_expiry_hours", "SHARE_LINK_EXPIRY_HOURS", "4"},
	{"ui.date_format", "DATE_FORMAT", "DD/MM/YYYY"},
	{"ui.time_format", "TIME_FORMAT", "12h"},
	{"video.daylight_start_hour", "DAYLIGHT_START_HOUR", "7"},
	{"video.daylight_end_hour", "DAYLIGHT_END_HOUR", "19"},
	{"video.daylight_target_hour", "DAYLIGHT_TARGET_HOUR", "12"},
	{"video.weekly_keep", "WEEKLY_LAPSES_TO_KEEP", "4"},
	{"video.monthly_keep", "MONTHLY_LAPSES_TO_KEEP", "3"},
	{"snapshot.hq_params", "HQSNAP", "auto"},
	{"video.ffmpeg_threads", "FFMPEG_THREADS", "0"},
}

var (
	mu        sync.RWMutex
	cache     map[string]string
	cacheTime time.Time
	cacheTTL  = 30 * time.Second
)

// Init seeds missing settings from env vars (or defaults) and warms the cache.
// Must be called after database.InitDB().
func Init() {
	for _, e := range KnownSettings {
		val := e.DefVal
		if e.EnvVar != "" {
			if v := os.Getenv(e.EnvVar); v != "" {
				val = v
			}
		}
		// Silently ignore errors — DB might not be reachable in tests
		_ = database.InsertSettingIfAbsent(e.Key, val)
	}
	Invalidate()
}

func loadCache() {
	all, err := database.GetAllSettings()
	if err != nil {
		return
	}
	mu.Lock()
	cache = all
	cacheTime = time.Now()
	mu.Unlock()
}

// Get returns the setting value for key, or defaultVal if not found.
func Get(key, defaultVal string) string {
	mu.RLock()
	expired := cache == nil || time.Since(cacheTime) > cacheTTL
	mu.RUnlock()
	if expired {
		loadCache()
	}
	mu.RLock()
	defer mu.RUnlock()
	if v, ok := cache[key]; ok {
		return v
	}
	return defaultVal
}

// GetInt returns the setting as an integer, falling back to def.
func GetInt(key string, def int) int {
	v := Get(key, strconv.Itoa(def))
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return def
}

// Set persists a setting and invalidates the cache.
func Set(key, value string) error {
	err := database.SetSetting(key, value)
	if err == nil {
		Invalidate()
	}
	return err
}

// GetAll returns all settings currently in the DB.
func GetAll() (map[string]string, error) {
	return database.GetAllSettings()
}

// Invalidate forces the next Get to reload from the DB.
func Invalidate() {
	mu.Lock()
	cacheTime = time.Time{}
	mu.Unlock()
}

// GetCRFForQuality maps a quality name to an AV1/H.264 CRF value string.
func GetCRFForQuality(quality string) string {
	switch strings.ToLower(quality) {
	case "low":
		return "35"
	case "medium":
		return "28"
	case "high":
		return "20"
	case "ultra":
		return "15"
	default:
		return "28"
	}
}
