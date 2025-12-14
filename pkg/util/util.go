package util

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"


	"time-machine/pkg/config"
)

func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// GetSnapshotFiles recursively finds all snapshot files in the structured directory.
func GetSnapshotFiles() []string {
	var files []string
	err := filepath.WalkDir(config.AppConfig.SnapshotsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".jpg") {
			files = append(files, path)
		}
		return nil
	})

	if err != nil {
		log.Printf("Error walking snapshot directory: %v", err)
		return []string{}
	}

	sort.Strings(files) // Sorts chronologically due to file path structure
	return files
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func IsFileEmpty(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return true // File doesn't exist, consider it "empty" for practical purposes
	}
	if err != nil {
		log.Printf("Error stating file %s: %v", path, err)
		return true // On error, treat as empty to prevent issues
	}
	return info.Size() == 0
}

func GetFrameCount(videoPath string) (int, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0", // Select only video stream 0
		"-show_entries", "stream=nb_read_frames", // Changed from nb_frames to nb_read_frames
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoPath,
	)

	outputBytes, err := cmd.Output()
	output := strings.TrimSpace(string(outputBytes))

	if err != nil {
		return 0, fmt.Errorf("ffprobe command failed for %s: %w. Raw output: %s", videoPath, err, output)
	}

	if output == "" || output == "N/A" {
		// If ffprobe returns empty or "N/A", it couldn't determine the frame count.
		// This can happen for new or incomplete video files.
		return 0, fmt.Errorf("ffprobe could not determine frame count for %s. Raw output: %s", videoPath, output)
	}

	frameCount, err := strconv.Atoi(output)
	if err != nil {
		// This error means ffprobe returned something unexpected that's not an int.
		return 0, fmt.Errorf("failed to parse frame count '%s' for %s: %w. Raw output: %s", output, videoPath, err, output)
	}
	return frameCount, nil
}
