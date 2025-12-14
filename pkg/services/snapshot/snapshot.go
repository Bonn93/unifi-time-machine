package snapshot

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"time-machine/pkg/util"
	"time-machine/pkg/config"
)

// --- CORE LOGIC (Scheduler and API calls) ---

func StartSnapshotScheduler() {
	for {
		TakeSnapshot()
		time.Sleep(time.Duration(config.AppConfig.SnapshotIntervalSec) * time.Second)
	}
}

func TakeSnapshot() {
	if config.AppConfig.UFPHost == "" || config.AppConfig.UFPAPIKey == "" || config.AppConfig.TargetCameraID == "" {
		log.Println("Snapshot Error: UniFi Protect credentials missing.")
		return
	}

	apiURL := fmt.Sprintf("%s/proxy/protect/integration/v1/cameras/%s/snapshot", config.AppConfig.UFPHost, config.AppConfig.TargetCameraID)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: tr,
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("Error creating snapshot request: %v", err)
		return
	}
	req.Header.Set("X-Api-Key", config.AppConfig.UFPAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Snapshot API request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("UniFi API returned status code %d: %s", resp.StatusCode, string(bodyBytes))
		return
	}

	now := time.Now()

	// --- New Directory Structure Logic ---
	// Path: snapshots/YYYY-MM/DD/HH/
	snapshotDir := filepath.Join(config.AppConfig.SnapshotsDir, now.Format("2006-01"), now.Format("02"), now.Format("15"))
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		log.Printf("Error creating snapshot directory %s: %v", snapshotDir, err)
		return
	}

	// Save the snapshot for the timelapse
	fileName := now.Format("2006-01-02-15-04-05") + ".jpg"
	snapshotPath := filepath.Join(snapshotDir, fileName)
	out, err := os.Create(snapshotPath)
	if err != nil {
		log.Printf("Error creating file %s: %v", snapshotPath, err)
		return
	}
	defer out.Close()

	// Tee the response body to write to multiple places if needed
	if _, err = io.Copy(out, resp.Body); err != nil {
		log.Printf("Error saving snapshot to file %s: %v", snapshotPath, err)
		return
	}
	log.Printf("Snapshot saved: %s", snapshotPath)

	// --- New Gallery Logic ---
	// Save the first snapshot of the hour to the gallery
	galleryFileName := now.Format("2006-01-02-15") + ".jpg"
	galleryPath := filepath.Join(config.AppConfig.GalleryDir, galleryFileName)

	if !util.FileExists(galleryPath) {
		if err := util.CopyFile(snapshotPath, galleryPath); err != nil {
			log.Printf("Error copying snapshot to gallery %s: %v", galleryPath, err)
		} else {
			log.Printf("Saved new gallery image: %s", galleryPath)
		}
	}

	// Update the latest_snapshot.jpg for the video player poster
	latestPath := filepath.Join(config.AppConfig.DataDir, "latest_snapshot.jpg")
	if err := util.CopyFile(snapshotPath, latestPath); err != nil {
		log.Printf("Error copying snapshot to latest_snapshot.jpg: %v", err)
	}
}

func GetCameraStatus() map[string]interface{} {
	if config.AppConfig.UFPHost == "" || config.AppConfig.UFPAPIKey == "" || config.AppConfig.TargetCameraID == "" {
		return map[string]interface{}{"error": "UniFi Protect credentials missing from environment."}
	}

	apiURL := fmt.Sprintf("%s/proxy/protect/integration/v1/cameras/%s", config.AppConfig.UFPHost, config.AppConfig.TargetCameraID)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: tr,
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		log.Printf("Error creating camera status request: %v", err)
		return map[string]interface{}{"error": fmt.Sprintf("Request creation error: %v", err)}
	}

	req.Header.Set("X-Api-Key", config.AppConfig.UFPAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Camera Status API request failed: %v", err)
		return map[string]interface{}{"error": fmt.Sprintf("API request failed: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("UniFi API returned status code %d: %s", resp.StatusCode, string(bodyBytes))
		return map[string]interface{}{"error": fmt.Sprintf("API returned status code %d", resp.StatusCode)}
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Error decoding camera status JSON: %v", err)
		return map[string]interface{}{"error": "Failed to decode API response."}
	}

	return result
}

func GetFormattedCameraStatus() map[string]string {
	rawStatus := GetCameraStatus()

	if rawStatus == nil {
		return map[string]string{"Status": "ERROR: Connection Failed"}
	}
	if errMsg, ok := rawStatus["error"]; ok {
		return map[string]string{"Status": fmt.Sprintf("API ERROR: %s", errMsg)}
	}

	status := "Unknown"
	if state, ok := rawStatus["state"].(string); ok {
		status = state
	}

	uptimeStr := "N/A"
	if uptimeFloat, ok := rawStatus["upSince"].(float64); ok {
		upSince := time.Unix(int64(uptimeFloat/1000), 0)
		uptimeStr = upSince.Format("2006-01-02 15:04:05")
	}

	model := "N/A"
	if modelStr, ok := rawStatus["modelKey"].(string); ok {
		model = strings.ReplaceAll(modelStr, "UVC G", "G")
	}

	name := "N/A"
	if nameStr, ok := rawStatus["name"].(string); ok {
		name = nameStr
	}

	return map[string]string{
		"Name":      name,
		"Model":     model,
		"Status":    status,
		"UpSince":   uptimeStr,
		"Connected": strconv.FormatBool(status == "CONNECTED"),
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func copyFile(src, dst string) error {
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
