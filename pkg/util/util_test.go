package util

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"time-machine/pkg/config"
)

func setupTest(t *testing.T) (string, func()) {
	tempDir, err := os.MkdirTemp("", "util-test")
	assert.NoError(t, err)

	config.AppConfig.SnapshotsDir = filepath.Join(tempDir, "snapshots")
	os.MkdirAll(config.AppConfig.SnapshotsDir, 0755)

	// Create some dummy snapshot files
	for i := 0; i < 3; i++ {
		dummyFile := filepath.Join(config.AppConfig.SnapshotsDir, fmt.Sprintf("snapshot_%d.jpg", i))
		os.WriteFile(dummyFile, []byte("dummy"), 0644)
	}

	return tempDir, func() {
		os.RemoveAll(tempDir)
	}
}

func TestCopyFile(t *testing.T) {
	tempDir, cleanup := setupTest(t)
	defer cleanup()

	src := filepath.Join(tempDir, "source.txt")
	dst := filepath.Join(tempDir, "destination.txt")
	os.WriteFile(src, []byte("hello"), 0644)

	err := CopyFile(src, dst)
	assert.NoError(t, err)

	content, err := os.ReadFile(dst)
	assert.NoError(t, err)
	assert.Equal(t, "hello", string(content))
}

func TestGetSnapshotFiles(t *testing.T) {
	_, cleanup := setupTest(t)
	defer cleanup()

	files := GetSnapshotFiles()
	assert.Len(t, files, 3)
	assert.True(t, sort.StringsAreSorted(files))
}

func TestFileExists(t *testing.T) {
	tempDir, cleanup := setupTest(t)
	defer cleanup()

	existingFile := filepath.Join(tempDir, "exists.txt")
	os.WriteFile(existingFile, []byte{}, 0644)
	nonExistingFile := filepath.Join(tempDir, "non-exists.txt")

	assert.True(t, FileExists(existingFile))
	assert.False(t, FileExists(nonExistingFile))
}

func TestIsFileEmpty(t *testing.T) {
	tempDir, cleanup := setupTest(t)
	defer cleanup()

	emptyFile := filepath.Join(tempDir, "empty.txt")
	os.WriteFile(emptyFile, []byte{}, 0644)
	nonEmptyFile := filepath.Join(tempDir, "non-empty.txt")
	os.WriteFile(nonEmptyFile, []byte("not empty"), 0644)
	nonExistingFile := filepath.Join(tempDir, "non-existing.txt")

	assert.True(t, IsFileEmpty(emptyFile))
	assert.False(t, IsFileEmpty(nonEmptyFile))
	assert.True(t, IsFileEmpty(nonExistingFile))
}

func TestGetGalleryFiles(t *testing.T) {
	tempDir, cleanup := setupTest(t)
	defer cleanup()

	galleryDir := filepath.Join(tempDir, "gallery")
	os.MkdirAll(galleryDir, 0755)

	originalGalleryDir := config.AppConfig.GalleryDir
	config.AppConfig.GalleryDir = galleryDir
	defer func() { config.AppConfig.GalleryDir = originalGalleryDir }()

	// Gallery files use the 4-part YYYY-MM-DD-HH.jpg format
	names := []string{"2026-05-15-07.jpg", "2026-05-15-12.jpg", "2026-05-15-18.jpg"}
	for _, name := range names {
		os.WriteFile(filepath.Join(galleryDir, name), []byte("g"), 0644)
	}
	// Non-jpg file should be ignored
	os.WriteFile(filepath.Join(galleryDir, "notes.txt"), []byte("x"), 0644)

	files := GetGalleryFiles()
	assert.Len(t, files, 3, "should return only .jpg files")
	assert.True(t, sort.StringsAreSorted(files), "results must be sorted chronologically")

	// Verify the paths end with the expected filenames
	for i, name := range names {
		assert.Equal(t, name, filepath.Base(files[i]))
	}
}

func TestGetGalleryFiles_EmptyDir(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "empty-gallery-test")
	defer os.RemoveAll(tempDir)

	galleryDir := filepath.Join(tempDir, "gallery")
	os.MkdirAll(galleryDir, 0755)

	originalGalleryDir := config.AppConfig.GalleryDir
	config.AppConfig.GalleryDir = galleryDir
	defer func() { config.AppConfig.GalleryDir = originalGalleryDir }()

	files := GetGalleryFiles()
	assert.Empty(t, files, "empty gallery directory should return no files")
}
