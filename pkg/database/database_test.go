package database

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"time-machine/pkg/config"
)

func setupTestDB(t *testing.T) *sql.DB {
	config.AppConfig.DataDir = t.TempDir()
	InitDB()
	return GetDB()
}

func TestInitDB(t *testing.T) {
	config.AppConfig.DataDir = t.TempDir()
	InitDB()
	assert.NotNil(t, db)
	db.Close()
}

func TestHashAndCheckPassword(t *testing.T) {
	password := "password123"
	hash, err := HashPassword(password)
	assert.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.True(t, CheckPasswordHash(password, hash))
	assert.False(t, CheckPasswordHash("wrongpassword", hash))
}

func TestCreateAndGetUser(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Test user creation
	err := CreateUser("testuser", "password123", true)
	assert.NoError(t, err)

	// Test user existence
	exists, err := UserExists("testuser")
	assert.NoError(t, err)
	assert.True(t, exists)

	// Test getting user
	user, err := GetUserByUsername("testuser")
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, "testuser", user.Username)
	assert.True(t, user.IsAdmin)

	// Test creating a duplicate user
	err = CreateUser("testuser", "password123", false)
	assert.Error(t, err)
}

func TestCheckUserCredentials(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	err := CreateUser("testuser", "password123", false)
	assert.NoError(t, err)

	user, authenticated := CheckUserCredentials("testuser", "password123")
	assert.True(t, authenticated)
	assert.NotNil(t, user)
	assert.Equal(t, "testuser", user.Username)

	_, authenticated = CheckUserCredentials("testuser", "wrongpassword")
	assert.False(t, authenticated)

	_, authenticated = CheckUserCredentials("nonexistentuser", "password123")
	assert.False(t, authenticated)
}

func TestUserExists(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	exists, err := UserExists("nonexistentuser")
	assert.NoError(t, err)
	assert.False(t, exists)

	err = CreateUser("testuser", "password", false)
	assert.NoError(t, err)

	exists, err = UserExists("testuser")
	assert.NoError(t, err)
	assert.True(t, exists)
}

func TestGetDB(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	assert.Equal(t, db, GetDB())
}

func TestGetAllUsers(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	err := CreateUser("user1", "pass1", false)
	assert.NoError(t, err)
	err = CreateUser("user2", "pass2", true)
	assert.NoError(t, err)

	users, err := GetAllUsers()
	assert.NoError(t, err)
	assert.Len(t, users, 2)

	assert.Equal(t, "user1", users[0].Username)
	assert.False(t, users[0].IsAdmin)
	assert.Equal(t, "user2", users[1].Username)
	assert.True(t, users[1].IsAdmin)
}

func TestDeleteUser(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	err := CreateUser("testuser", "password", false)
	assert.NoError(t, err)

	exists, err := UserExists("testuser")
	assert.NoError(t, err)
	assert.True(t, exists)

	err = DeleteUser("testuser")
	assert.NoError(t, err)

	exists, err = UserExists("testuser")
	assert.NoError(t, err)
	assert.False(t, exists)

	// Test deleting a non-existent user
	err = DeleteUser("nonexistentuser")
	assert.Error(t, err)
}

func TestUpdateUserPassword(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Create a user
	err := CreateUser("testuser", "oldpassword", false)
	assert.NoError(t, err)

	// Update the password
	err = UpdateUserPassword("testuser", "newpassword")
	assert.NoError(t, err)

	// Check credentials with the new password
	user, authenticated := CheckUserCredentials("testuser", "newpassword")
	assert.True(t, authenticated)
	assert.NotNil(t, user)

	// Check credentials with the old password
	_, authenticated = CheckUserCredentials("testuser", "oldpassword")
	assert.False(t, authenticated)

	// Test updating password for a non-existent user
	err = UpdateUserPassword("nonexistentuser", "newpassword")
	assert.Error(t, err)
}
