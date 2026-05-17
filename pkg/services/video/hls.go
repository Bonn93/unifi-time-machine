package video

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"time-machine/pkg/config"
	"time-machine/pkg/database"
	"time-machine/pkg/services/settings"
)

// HLSQuality describes a single quality level within an HLS stream.
type HLSQuality struct {
	Label     string // "source", "720p", "480p"
	Height    int    // 0 = pass-through (source resolution)
	CRF       int
	Bandwidth int // approximate bps for the master playlist
}

// parseHLSQualities converts a comma-separated quality string (e.g. "source,720p") into HLSQuality entries.
func parseHLSQualities(raw string) []HLSQuality {
	baseCRF := 23
	if n, err := strconv.Atoi(settings.GetCRFForQuality(settings.Get("video.quality", "medium"))); err == nil {
		baseCRF = n
	}

	heightFor := map[string]int{"source": 0, "1080p": 1080, "720p": 720, "480p": 480}
	bwFor := map[string]int{"source": 4_000_000, "1080p": 3_000_000, "720p": 2_000_000, "480p": 800_000}
	crfDelta := map[string]int{"source": 0, "1080p": 0, "720p": 2, "480p": 4}

	var qualities []HLSQuality
	for _, label := range strings.Split(raw, ",") {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		delta := crfDelta[label] // 0 for unknown labels
		qualities = append(qualities, HLSQuality{
			Label:     label,
			Height:    heightFor[label],
			CRF:       baseCRF + delta,
			Bandwidth: bwFor[label],
		})
	}
	if len(qualities) == 0 {
		qualities = []HLSQuality{{Label: "source", Height: 0, CRF: baseCRF, Bandwidth: 4_000_000}}
	}
	return qualities
}

// generateHLS encodes all quality levels in one FFmpeg pass using filter_complex.
// Segments land in {DataDir}/hls/timelapse_{name}/{label}/ and a master.m3u8 is written.
func generateHLS(name, concatListPath string, qualities []HLSQuality) error {
	dataDir := config.AppConfig.DataDir
	hlsDir := filepath.Join(dataDir, "hls", "timelapse_"+name)

	if err := os.MkdirAll(hlsDir, 0755); err != nil {
		return fmt.Errorf("failed to create HLS dir: %w", err)
	}
	for _, q := range qualities {
		if err := os.MkdirAll(filepath.Join(hlsDir, q.Label), 0755); err != nil {
			return fmt.Errorf("failed to create quality dir %s: %w", q.Label, err)
		}
	}

	segSec := settings.GetInt("video.hls_segment_sec", 4)
	preset := settings.Get("video.encoder_preset", "fast")
	maxBitrate := settings.Get("video.max_bitrate", "2M")
	bufSize := computeBufSize(maxBitrate)

	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "concat", "-safe", "0", "-i", concatListPath,
		"-threads", fmt.Sprintf("%d", ffmpegThreads),
	}

	if len(qualities) > 1 {
		// Build filter_complex: split into N, then scale each non-source stream
		splitExpr := fmt.Sprintf("[0:v]split=%d", len(qualities))
		for i := range qualities {
			splitExpr += fmt.Sprintf("[v%d]", i)
		}
		var scaleParts []string
		scaleParts = append(scaleParts, splitExpr)
		for i, q := range qualities {
			if q.Height > 0 {
				scaleParts = append(scaleParts, fmt.Sprintf("[v%d]scale=-2:%d[s%d]", i, q.Height, i))
			} else {
				scaleParts = append(scaleParts, fmt.Sprintf("[v%d]null[s%d]", i, i))
			}
		}
		args = append(args, "-filter_complex", strings.Join(scaleParts, "; "))
	}

	for i, q := range qualities {
		mapVal := "0:v"
		if len(qualities) > 1 {
			mapVal = fmt.Sprintf("[s%d]", i)
		}
		segFile := filepath.Join(hlsDir, q.Label, "seg_%04d.ts")
		playlist := filepath.Join(hlsDir, q.Label, "index.m3u8")
		args = append(args,
			"-map", mapVal,
			"-c:v", "libx264",
			"-preset", preset,
			"-crf", fmt.Sprintf("%d", q.CRF),
			"-g", "48", "-sc_threshold", "0",
			"-maxrate", maxBitrate, "-bufsize", bufSize,
			"-hls_time", fmt.Sprintf("%d", segSec),
			"-hls_playlist_type", "vod",
			"-hls_segment_filename", segFile,
			playlist,
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.RemoveAll(hlsDir)
		today := time.Now().Format("2006-01-02")
		_ = database.AppendFFmpegLog(today, name, fmt.Sprintf("--- HLS Error for %s: %s ---\n%s\n", name, time.Now(), stderr.String()))
		return fmt.Errorf("ffmpeg HLS encode failed for %s: %w", name, err)
	}

	if err := writeMasterPlaylist(hlsDir, qualities); err != nil {
		return err
	}
	log.Printf("Generated HLS: %s", hlsDir)
	return nil
}

// writeMasterPlaylist writes an HLS master.m3u8 referencing each quality level.
func writeMasterPlaylist(hlsDir string, qualities []HLSQuality) error {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	for _, q := range qualities {
		resTag := ""
		if q.Height > 0 {
			w := q.Height * 16 / 9
			resTag = fmt.Sprintf(",RESOLUTION=%dx%d", w, q.Height)
		}
		sb.WriteString(fmt.Sprintf(
			"#EXT-X-STREAM-INF:BANDWIDTH=%d%s,NAME=\"%s\"\n%s/index.m3u8\n",
			q.Bandwidth, resTag, q.Label, q.Label,
		))
	}
	return os.WriteFile(filepath.Join(hlsDir, "master.m3u8"), []byte(sb.String()), 0644)
}

// generateMP4 encodes a single H.264 MP4 with fast-start from the concat list.
func generateMP4(name, concatListPath string) error {
	dataDir := config.AppConfig.DataDir
	outputPath := filepath.Join(dataDir, fmt.Sprintf("timelapse_%s.mp4", name))
	tempPath := outputPath + ".tmp.mp4"

	preset := settings.Get("video.encoder_preset", "fast")
	crf := settings.GetCRFForQuality(settings.Get("video.quality", "medium"))
	maxBitrate := settings.Get("video.max_bitrate", "2M")
	bufSize := computeBufSize(maxBitrate)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-f", "concat", "-safe", "0", "-i", concatListPath,
		"-vf", "scale=out_color_matrix=bt709:out_range=tv,format=yuv420p",
		"-c:v", "libx264",
		"-preset", preset,
		"-crf", crf,
		"-maxrate", maxBitrate, "-bufsize", bufSize,
		"-movflags", "+faststart",
		"-threads", fmt.Sprintf("%d", ffmpegThreads),
		"-an", "-y", tempPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.Remove(tempPath)
		today := time.Now().Format("2006-01-02")
		_ = database.AppendFFmpegLog(today, name, fmt.Sprintf("--- MP4 Error for %s: %s ---\n%s\n", name, time.Now(), stderr.String()))
		return fmt.Errorf("ffmpeg MP4 encode failed for %s: %w", name, err)
	}

	os.Remove(outputPath)
	if err := os.Rename(tempPath, outputPath); err != nil {
		return fmt.Errorf("failed to rename MP4 temp file: %w", err)
	}
	log.Printf("Generated MP4: %s", outputPath)
	return nil
}
