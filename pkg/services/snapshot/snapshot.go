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

	"time-machine/pkg/config"
	"time-machine/pkg/services/settings"
	"time-machine/pkg/util"
)

// minSnapshotBytes is the smallest a valid camera JPEG is expected to be.
// An NVR that is up but whose camera is offline can return HTTP 200 with an empty
// or near-empty body. Snapshots below this threshold are discarded on capture.
const minSnapshotBytes int64 = 2048

// consecutiveFailureWarnThreshold is the number of back-to-back snapshot failures
// that triggers a loud log warning about potential NVR/camera connectivity issues.
const consecutiveFailureWarnThreshold = 3

// hqCapable is the camera's auto-detected HQ snapshot capability, set at startup.
// It is only written once during InitSnapshotSettings and is safe to read concurrently.
var hqCapable bool

// InitSnapshotSettings probes camera capabilities and seeds the initial HQ snapshot mode.
// The effective mode is re-evaluated dynamically at each snapshot, so admin changes to
// snapshot.hq_params take effect on the next capture without a restart.
func InitSnapshotSettings() {
	hqSetting := strings.ToLower(settings.Get("snapshot.hq_params", "auto"))

	log.Println("╔══ Snapshot Quality Configuration ════════════════════════")
	log.Printf("║  snapshot.hq_params (HQSNAP): %q", hqSetting)

	switch hqSetting {
	case "true":
		hqCapable = true
		log.Println("║  Mode: FORCED ON — high-quality snapshots enabled regardless of camera capability")
	case "false":
		hqCapable = false
		log.Println("║  Mode: FORCED OFF — standard quality snapshots (HQSNAP=false)")
	default: // "auto" or unrecognised
		log.Println("║  Mode: AUTO — probing camera for HQ snapshot support...")
		detectAndPersistHQCapability()
	}

	log.Printf("║  Effective snapshot quality: %s", GetEffectiveSnapshotQuality())
	log.Println("╚══════════════════════════════════════════════════════════")
}

// detectAndPersistHQCapability queries the camera API for supportFullHdSnapshot and
// stores the result in the settings DB so future startups have a fallback value.
func detectAndPersistHQCapability() {
	status := GetCameraStatus()

	if errMsg, ok := status["error"]; ok {
		// Fall back to the last-known persisted value so a brief offline camera
		// doesn't permanently disable HQ on the next restart.
		storedCapable := strings.ToLower(settings.Get("camera.hq_capable", "false"))
		hqCapable = storedCapable == "true"
		log.Printf("║  WARNING: Camera probe failed (%v)", errMsg)
		log.Printf("║           Using last-known stored capability: hq_capable=%v", hqCapable)
		return
	}

	flags, ok := status["featureFlags"].(map[string]interface{})
	if !ok {
		log.Println("║  WARNING: featureFlags missing from camera API response — defaulting to standard quality")
		hqCapable = false
	} else if supported, ok := flags["supportFullHdSnapshot"].(bool); ok {
		hqCapable = supported
		if supported {
			log.Println("║  Camera: supportFullHdSnapshot = true  — HQ snapshots AVAILABLE for this model")
		} else {
			log.Println("║  Camera: supportFullHdSnapshot = false — HQ snapshots NOT supported by this model")
		}
	} else {
		log.Println("║  WARNING: supportFullHdSnapshot flag absent in featureFlags — defaulting to standard quality")
		hqCapable = false
	}

	// Persist detected value as a fallback for future startups when the camera may be offline.
	if err := settings.Set("camera.hq_capable", strconv.FormatBool(hqCapable)); err != nil {
		log.Printf("║  WARNING: could not persist camera.hq_capable to DB: %v", err)
	}
}

// isHighQualityEnabled returns whether the next snapshot should use the HQ endpoint.
// It reads snapshot.hq_params from the DB on every call so admin changes are hot-reloadable
// without a service restart.
func isHighQualityEnabled() bool {
	switch strings.ToLower(settings.Get("snapshot.hq_params", "auto")) {
	case "true":
		return true
	case "false":
		return false
	default: // "auto"
		return hqCapable
	}
}

// GetHQCapable returns the auto-detected camera HQ capability (set at startup).
func GetHQCapable() bool {
	return hqCapable
}

// GetEffectiveSnapshotQuality returns a human-readable description of the snapshot
// quality that will be used for the next capture, including how the mode was selected.
func GetEffectiveSnapshotQuality() string {
	switch strings.ToLower(settings.Get("snapshot.hq_params", "auto")) {
	case "true":
		return "High Quality (forced on)"
	case "false":
		return "Standard (forced off)"
	default: // "auto"
		if hqCapable {
			return "High Quality (auto-detected)"
		}
		return "Standard (auto-detected)"
	}
}

// --- CORE LOGIC (Scheduler and API calls) ---

func StartSnapshotScheduler() {
	var consecutiveFailures int
	for {
		if TakeSnapshot() {
			consecutiveFailures = 0
		} else {
			consecutiveFailures++
			if consecutiveFailures >= consecutiveFailureWarnThreshold {
				log.Printf("WARNING: %d consecutive snapshot failures — NVR may be unreachable or returning invalid data; check connectivity", consecutiveFailures)
			}
		}
		time.Sleep(time.Duration(settings.GetInt("snapshot.interval_sec", 3600)) * time.Second)
	}
}

func TakeSnapshot() bool {
	if config.AppConfig.UFPHost == "" || config.AppConfig.UFPAPIKey == "" || config.AppConfig.TargetCameraID == "" {
		log.Println("Snapshot Error: UniFi Protect credentials missing.")
		return false
	}

	apiURL := fmt.Sprintf("%s/proxy/protect/integration/v1/cameras/%s/snapshot", config.AppConfig.UFPHost, config.AppConfig.TargetCameraID)
	if isHighQualityEnabled() {
		apiURL += "?highQuality=true"
	}

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
		return false
	}
	req.Header.Set("X-Api-Key", config.AppConfig.UFPAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Snapshot API request failed: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("UniFi API returned status code %d: %s", resp.StatusCode, string(bodyBytes))
		return false
	}

	now := time.Now()

	// Path: snapshots/YYYY-MM/DD/HH/
	snapshotDir := filepath.Join(config.AppConfig.SnapshotsDir, now.Format("2006-01"), now.Format("02"), now.Format("15"))
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		log.Printf("Error creating snapshot directory %s: %v", snapshotDir, err)
		return false
	}

	fileName := now.Format("2006-01-02-15-04-05") + ".jpg"
	snapshotPath := filepath.Join(snapshotDir, fileName)
	out, err := os.Create(snapshotPath)
	if err != nil {
		log.Printf("Error creating file %s: %v", snapshotPath, err)
		return false
	}

	_, copyErr := io.Copy(out, resp.Body)
	out.Close() // close before stat so the OS flushes metadata

	if copyErr != nil {
		log.Printf("Error saving snapshot to file %s: %v", snapshotPath, copyErr)
		os.Remove(snapshotPath)
		return false
	}

	// Reject snapshots that are too small to be a real camera JPEG.
	// An NVR that is up but whose camera is offline can return HTTP 200
	// with an empty or near-empty body that would produce corrupt video frames.
	info, statErr := os.Stat(snapshotPath)
	if statErr != nil {
		log.Printf("Snapshot %s: stat failed after write: %v", snapshotPath, statErr)
		os.Remove(snapshotPath)
		return false
	}
	if info.Size() < minSnapshotBytes {
		log.Printf("Snapshot %s discarded: %d bytes is below minimum threshold (%d) — NVR may be returning empty/placeholder data",
			snapshotPath, info.Size(), minSnapshotBytes)
		os.Remove(snapshotPath)
		return false
	}

	log.Printf("Snapshot saved: %s", snapshotPath)

	// Save the first snapshot of the hour to the gallery.
	galleryFileName := now.Format("2006-01-02-15") + ".jpg"
	galleryPath := filepath.Join(config.AppConfig.GalleryDir, galleryFileName)

	if !util.FileExists(galleryPath) {
		if err := util.CopyFile(snapshotPath, galleryPath); err != nil {
			log.Printf("Error copying snapshot to gallery %s: %v", galleryPath, err)
		} else {
			log.Printf("Saved new gallery image: %s", galleryPath)
		}
	}

	// Update the latest_snapshot.jpg for the video player poster.
	latestPath := filepath.Join(config.AppConfig.DataDir, "latest_snapshot.jpg")
	if err := util.CopyFile(snapshotPath, latestPath); err != nil {
		log.Printf("Error copying snapshot to latest_snapshot.jpg: %v", err)
	}
	return true
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

var GetFormattedCameraStatus = func() map[string]string {
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

	hqEnabled := isHighQualityEnabled()
	return map[string]string{
		"Name":            name,
		"Model":           model,
		"Status":          status,
		"UpSince":         uptimeStr,
		"Connected":       strconv.FormatBool(status == "CONNECTED"),
		"HQCapable":       strconv.FormatBool(hqCapable),
		"HQSetting":       strings.ToLower(settings.Get("snapshot.hq_params", "auto")),
		"HQEnabled":       strconv.FormatBool(hqEnabled),
		"SnapshotQuality": GetEffectiveSnapshotQuality(),
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
