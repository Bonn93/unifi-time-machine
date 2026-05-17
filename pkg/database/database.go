package database

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	_ "github.com/mattn/go-sqlite3"

	"time-machine/pkg/config"
	"time-machine/pkg/models"
)

// argon2Params holds the parameters for the Argon2id hashing algorithm.
type argon2Params struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

var params = &argon2Params{
	memory:      64 * 1024,
	iterations:  3,
	parallelism: 4,
	saltLength:  16,
	keyLength:   32,
}

var db *sql.DB

type migration struct {
	version int
	sql     string
}

var migrations = []migration{
	{1, `CREATE TABLE IF NOT EXISTS users (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"username" TEXT NOT NULL UNIQUE,
		"password_hash" TEXT NOT NULL,
		"is_admin" INTEGER NOT NULL DEFAULT 0
	)`},
	{2, `CREATE TABLE IF NOT EXISTS jobs (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"job_type" TEXT NOT NULL,
		"payload" TEXT,
		"status" TEXT NOT NULL DEFAULT 'pending',
		"error" TEXT,
		"created_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
		"updated_at" DATETIME DEFAULT CURRENT_TIMESTAMP
	)`},
	{3, `CREATE TRIGGER IF NOT EXISTS update_jobs_updated_at
		AFTER UPDATE ON jobs
		FOR EACH ROW
		BEGIN
			UPDATE jobs SET updated_at = CURRENT_TIMESTAMP WHERE id = OLD.id;
		END`},
	{4, `CREATE TABLE IF NOT EXISTS shared_links (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"token" TEXT NOT NULL UNIQUE,
		"file_path" TEXT NOT NULL,
		"expires_at" DATETIME NOT NULL
	)`},
	{5, `CREATE TABLE IF NOT EXISTS timelapse_trackers (
		"timelapse_name" TEXT NOT NULL PRIMARY KEY,
		"last_snapshot_path" TEXT NOT NULL,
		"updated_at" DATETIME DEFAULT CURRENT_TIMESTAMP
	)`},
	{6, `CREATE TABLE IF NOT EXISTS ffmpeg_logs (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"log_date" TEXT NOT NULL,
		"timelapse_name" TEXT,
		"content" TEXT NOT NULL,
		"created_at" DATETIME DEFAULT CURRENT_TIMESTAMP
	)`},
	{7, `CREATE INDEX IF NOT EXISTS idx_ffmpeg_logs_date ON ffmpeg_logs (log_date)`},
	{8, `CREATE TABLE IF NOT EXISTS settings (
		key TEXT NOT NULL PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`},
	{9, `CREATE TRIGGER IF NOT EXISTS update_settings_updated_at
		AFTER UPDATE ON settings FOR EACH ROW
		BEGIN UPDATE settings SET updated_at = CURRENT_TIMESTAMP WHERE key = OLD.key; END`},
}

// RunMigrations creates the schema_migrations table if needed and applies any
// pending migrations in version order. Existing tables are untouched because
// all DDL uses IF NOT EXISTS / IF NOT EXISTS semantics.
func RunMigrations() {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER NOT NULL PRIMARY KEY
	)`)
	if err != nil {
		log.Fatalf("Failed to create schema_migrations table: %v", err)
	}

	var currentVersion int
	db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentVersion)

	for _, m := range migrations {
		if m.version <= currentVersion {
			continue
		}
		if _, err := db.Exec(m.sql); err != nil {
			log.Fatalf("Migration %d failed: %v", m.version, err)
		}
		if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", m.version); err != nil {
			log.Fatalf("Failed to record migration %d: %v", m.version, err)
		}
		log.Printf("Applied DB migration %d.", m.version)
	}
}

// MigrateTrackerFiles imports any existing timelapse_*.last_snapshot.txt sidecar
// files into the timelapse_trackers table, then deletes them.
func MigrateTrackerFiles() {
	files, err := filepath.Glob(filepath.Join(config.AppConfig.DataDir, "timelapse_*.last_snapshot.txt"))
	if err != nil || len(files) == 0 {
		return
	}
	log.Printf("Migrating %d timelapse tracker file(s) to database...", len(files))
	for _, file := range files {
		name := filepath.Base(file)
		timelapseName := strings.TrimSuffix(strings.TrimPrefix(name, "timelapse_"), ".last_snapshot.txt")

		content, err := os.ReadFile(file)
		if err != nil {
			log.Printf("Warning: could not read tracker file %s: %v", file, err)
			continue
		}
		snapshotPath := strings.TrimSpace(string(content))
		if snapshotPath == "" {
			os.Remove(file)
			continue
		}

		_, err = db.Exec(
			"INSERT OR IGNORE INTO timelapse_trackers (timelapse_name, last_snapshot_path) VALUES (?, ?)",
			timelapseName, snapshotPath,
		)
		if err != nil {
			log.Printf("Warning: could not import tracker for %s: %v", timelapseName, err)
			continue
		}

		if err := os.Remove(file); err != nil {
			log.Printf("Warning: could not delete migrated tracker file %s: %v", file, err)
		} else {
			log.Printf("Migrated tracker for %s → database, deleted %s", timelapseName, name)
		}
	}
}

// MigrateLogFiles imports any existing ffmpeg_log_*.txt files into the
// ffmpeg_logs table (one row per file), then deletes them.
func MigrateLogFiles() {
	files, err := filepath.Glob(filepath.Join(config.AppConfig.DataDir, "ffmpeg_log_*.txt"))
	if err != nil || len(files) == 0 {
		return
	}
	log.Printf("Migrating %d FFmpeg log file(s) to database...", len(files))
	for _, file := range files {
		name := filepath.Base(file)
		dateStr := strings.TrimSuffix(strings.TrimPrefix(name, "ffmpeg_log_"), ".txt")

		// Skip if any rows already exist for this date (idempotent restart safety).
		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM ffmpeg_logs WHERE log_date = ?)", dateStr).Scan(&exists)
		if exists {
			os.Remove(file)
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			log.Printf("Warning: could not read log file %s: %v", file, err)
			continue
		}
		if len(content) == 0 {
			os.Remove(file)
			continue
		}

		_, err = db.Exec(
			"INSERT INTO ffmpeg_logs (log_date, timelapse_name, content) VALUES (?, NULL, ?)",
			dateStr, string(content),
		)
		if err != nil {
			log.Printf("Warning: could not import log for %s: %v", dateStr, err)
			continue
		}

		if err := os.Remove(file); err != nil {
			log.Printf("Warning: could not delete migrated log file %s: %v", file, err)
		} else {
			log.Printf("Migrated FFmpeg log for %s → database, deleted %s", dateStr, name)
		}
	}
}

// InitDB opens the database, runs migrations, and imports any legacy .txt files.
func InitDB() {
	dbPath := filepath.Join(config.AppConfig.DataDir, "lapse.db")
	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	RunMigrations()
	MigrateTrackerFiles()
	MigrateLogFiles()

	log.Println("Database initialized successfully.")
}

// --- Settings ---

// InsertSettingIfAbsent inserts a setting only if the key doesn't already exist.
func InsertSettingIfAbsent(key, value string) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(
		"INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)",
		key, value,
	)
	return err
}

// SetSetting upserts a setting value.
func SetSetting(key, value string) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

// GetAllSettings returns all settings as a key→value map.
func GetAllSettings() (map[string]string, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := db.Query("SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// --- Timelapse tracker ---

// GetTimelapseTracker returns the last snapshot path recorded for timelapseName,
// or "" if no record exists.
func GetTimelapseTracker(timelapseName string) (string, error) {
	var path string
	err := db.QueryRow(
		"SELECT last_snapshot_path FROM timelapse_trackers WHERE timelapse_name = ?",
		timelapseName,
	).Scan(&path)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return path, err
}

// SetTimelapseTracker upserts the last snapshot path for timelapseName.
func SetTimelapseTracker(timelapseName, snapshotPath string) error {
	_, err := db.Exec(
		`INSERT INTO timelapse_trackers (timelapse_name, last_snapshot_path, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(timelapse_name) DO UPDATE SET
		     last_snapshot_path = excluded.last_snapshot_path,
		     updated_at = CURRENT_TIMESTAMP`,
		timelapseName, snapshotPath,
	)
	return err
}

// --- FFmpeg logs ---

// AppendFFmpegLog inserts a log entry for the given date. timelapseName may be
// empty for entries that don't relate to a specific timelapse.
func AppendFFmpegLog(date, timelapseName, content string) error {
	if content == "" {
		return nil
	}
	var name interface{}
	if timelapseName != "" {
		name = timelapseName
	}
	_, err := db.Exec(
		"INSERT INTO ffmpeg_logs (log_date, timelapse_name, content) VALUES (?, ?, ?)",
		date, name, content,
	)
	return err
}

// GetFFmpegLogDates returns distinct log dates in descending order.
func GetFFmpegLogDates() ([]string, error) {
	rows, err := db.Query("SELECT DISTINCT log_date FROM ffmpeg_logs ORDER BY log_date DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		dates = append(dates, d)
	}
	return dates, rows.Err()
}

// GetFFmpegLogContent concatenates all log entries for the given date,
// ordered by creation time.
func GetFFmpegLogContent(date string) (string, error) {
	rows, err := db.Query(
		"SELECT content FROM ffmpeg_logs WHERE log_date = ? ORDER BY created_at ASC",
		date,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var sb strings.Builder
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return "", err
		}
		sb.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			sb.WriteByte('\n')
		}
	}
	return sb.String(), rows.Err()
}

// --- Password hashing ---

// HashPassword generates an Argon2id hash of the password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, params.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, params.keyLength)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	format := "$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s"
	return fmt.Sprintf(format, argon2.Version, params.memory, params.iterations, params.parallelism, b64Salt, b64Hash), nil
}

// CheckPasswordHash compares a password with an Argon2id hash.
func CheckPasswordHash(password, hash string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		log.Println("Warning: Invalid hash format provided to checkPasswordHash")
		return false
	}

	var version int
	_, err := fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil || version != argon2.Version {
		log.Println("Warning: Incompatible Argon2 version")
		return false
	}

	p := &argon2Params{}
	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism)
	if err != nil {
		log.Printf("Warning: Failed to parse Argon2 params: %v", err)
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		log.Printf("Warning: Failed to decode salt: %v", err)
		return false
	}
	p.saltLength = uint32(len(salt))

	decodedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		log.Printf("Warning: Failed to decode hash: %v", err)
		return false
	}
	p.keyLength = uint32(len(decodedHash))

	comparisonHash := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLength)

	return subtle.ConstantTimeCompare(decodedHash, comparisonHash) == 1
}

// --- User management ---

func UserExists(username string) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func CreateUser(username, password string, isAdmin bool) error {
	exists, err := UserExists(username)
	if err != nil {
		return fmt.Errorf("failed to check if user exists: %w", err)
	}
	if exists {
		return fmt.Errorf("user '%s' already exists", username)
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	_, err = db.Exec("INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, ?)", username, passwordHash, isAdmin)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	log.Printf("Successfully created user: %s (Admin: %t)", username, isAdmin)
	return nil
}

func CheckUserCredentials(username, password string) (*models.User, bool) {
	user, err := GetUserByUsername(username)
	if err != nil {
		log.Printf("Error retrieving user %s: %v", username, err)
		return nil, false
	}
	if user == nil {
		return nil, false
	}

	var passwordHash string
	err = db.QueryRow("SELECT password_hash FROM users WHERE username = ?", username).Scan(&passwordHash)
	if err != nil {
		log.Printf("Error querying for password hash of user %s: %v", username, err)
		return nil, false
	}

	if CheckPasswordHash(password, passwordHash) {
		return user, true
	}

	return nil, false
}

func GetUserByUsername(username string) (*models.User, error) {
	var user models.User
	var isAdminInt int
	err := db.QueryRow("SELECT id, username, is_admin FROM users WHERE username = ?", username).Scan(&user.ID, &user.Username, &isAdminInt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by username: %w", err)
	}
	user.IsAdmin = (isAdminInt == 1)
	return &user, nil
}

func GetAllUsers() ([]models.User, error) {
	rows, err := db.Query("SELECT id, username, is_admin FROM users ORDER BY username")
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		var isAdminInt int
		if err := rows.Scan(&user.ID, &user.Username, &isAdminInt); err != nil {
			return nil, fmt.Errorf("failed to scan user row: %w", err)
		}
		user.IsAdmin = (isAdminInt == 1)
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during user rows iteration: %w", err)
	}

	return users, nil
}

func DeleteUser(username string) error {
	result, err := db.Exec("DELETE FROM users WHERE username = ?", username)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user '%s' not found", username)
	}

	log.Printf("Successfully deleted user: %s", username)
	return nil
}

func UpdateUserPassword(username, newPassword string) error {
	passwordHash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	result, err := db.Exec("UPDATE users SET password_hash = ? WHERE username = ?", passwordHash, username)
	if err != nil {
		return fmt.Errorf("failed to update password for user '%s': %w", username, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user '%s' not found", username)
	}

	log.Printf("Successfully updated password for user: %s", username)
	return nil
}

// GetDB returns the database connection pool.
func GetDB() *sql.DB {
	return db
}

// --- Share links ---

func CreateShareLink(filePath string, duration time.Duration) (string, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	hash := sha256.Sum256(randomBytes)
	token := hex.EncodeToString(hash[:])

	var expiresAt time.Time
	if duration > 0 {
		expiresAt = time.Now().Add(duration)
	} else {
		expiresAt = time.Now().AddDate(100, 0, 0)
	}

	_, err := db.Exec("INSERT INTO shared_links (token, file_path, expires_at) VALUES (?, ?, ?)", token, filePath, expiresAt)
	if err != nil {
		return "", fmt.Errorf("failed to create share link: %w", err)
	}

	return token, nil
}

func GetSharedFilePath(token string) (string, error) {
	var filePath string
	var expiresAt time.Time
	err := db.QueryRow("SELECT file_path, expires_at FROM shared_links WHERE token = ?", token).Scan(&filePath, &expiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to get shared file path: %w", err)
	}

	if time.Now().After(expiresAt) {
		return "", nil
	}

	return filePath, nil
}

func DeleteExpiredShareLinks() error {
	result, err := db.Exec("DELETE FROM shared_links WHERE expires_at < ?", time.Now())
	if err != nil {
		return fmt.Errorf("failed to delete expired share links: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected > 0 {
		log.Printf("Deleted %d expired share links", rowsAffected)
	}

	return nil
}
