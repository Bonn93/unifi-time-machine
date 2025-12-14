package database

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"
	"path/filepath"
	"strings"

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

// InitDB initializes the database connection and creates the users table if it doesn't exist.
// Kept simple with sqlite for now, can migrate to a more robust solution later if needed. TIL SQLite needs CGO...
func InitDB() {
	dbPath := filepath.Join(config.AppConfig.DataDir, "lapse.db")
	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	createUserTableSQL := `CREATE TABLE IF NOT EXISTS users (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,		
		"username" TEXT NOT NULL UNIQUE,
		"password_hash" TEXT NOT NULL,
		"is_admin" INTEGER NOT NULL DEFAULT 0
	);`

	_, err = db.Exec(createUserTableSQL)
	if err != nil {
		log.Fatalf("Failed to create users table: %v", err)
	}
	log.Println("Database initialized and users table created successfully.")

	createJobTableSQL := `CREATE TABLE IF NOT EXISTS jobs (
		"id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		"job_type" TEXT NOT NULL,
		"payload" TEXT,
		"status" TEXT NOT NULL DEFAULT 'pending',
		"error" TEXT,
		"created_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
		"updated_at" DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	_, err = db.Exec(createJobTableSQL)
	if err != nil {
		log.Fatalf("Failed to create jobs table: %v", err)
	}
	log.Println("Jobs table created successfully.")

	// Trigger to update `updated_at` timestamp on row update
	createTriggerSQL := `
	CREATE TRIGGER IF NOT EXISTS update_jobs_updated_at
	AFTER UPDATE ON jobs
	FOR EACH ROW
	BEGIN
		UPDATE jobs SET updated_at = CURRENT_TIMESTAMP WHERE id = OLD.id;
	END;`

	_, err = db.Exec(createTriggerSQL)
	if err != nil {
		log.Fatalf("Failed to create trigger for jobs table: %v", err)
	}
}

// HashPassword generates an Argon2id hash of the password.
// The format is: $argon2id$v=19$m=<memory>,t=<iterations>,p=<parallelism>$<salt>$<hash>
func HashPassword(password string) (string, error) {
	salt := make([]byte, params.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, params.keyLength)

	// Encode salt and hash to base64
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	// Format into standard string
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

	// Use constant-time comparison to prevent timing attacks
	return subtle.ConstantTimeCompare(decodedHash, comparisonHash) == 1
}

// UserExists checks if a user exists in the database.
func UserExists(username string) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreateUser creates a new user in the database.
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

// CheckUserCredentials verifies a user's credentials and returns the user object on success.
func CheckUserCredentials(username, password string) (*models.User, bool) {
	user, err := GetUserByUsername(username)
	if err != nil {
		log.Printf("Error retrieving user %s: %v", username, err)
		return nil, false
	}
	if user == nil {
		return nil, false // User not found
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

// GetUserByUsername retrieves a user from the database by their username.
func GetUserByUsername(username string) (*models.User, error) {
	var user models.User
	var isAdminInt int
	err := db.QueryRow("SELECT id, username, is_admin FROM users WHERE username = ?", username).Scan(&user.ID, &user.Username, &isAdminInt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // User not found
		}
		return nil, fmt.Errorf("failed to get user by username: %w", err)
	}
	user.IsAdmin = (isAdminInt == 1)
	return &user, nil
}

// GetDB returns the database connection pool.
func GetDB() *sql.DB {
	return db
}

