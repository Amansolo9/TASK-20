package auth

import (
	"os"
	"testing"
	"time"

	"campus-portal/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupAuthDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "host=localhost port=5432 user=campus_admin password=campus_secret dbname=campus_portal sslmode=disable"
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Skipf("Skipping DB test — Postgres not available: %v", err)
	}
	sqlDB, _ := db.DB()
	if err := sqlDB.Ping(); err != nil {
		t.Skipf("Skipping DB test — cannot ping Postgres: %v", err)
	}
	db.AutoMigrate(&models.User{}, &models.Session{}, &models.TempAccess{})
	cleanupAuth(db) // Clean up any leftover test data from prior runs
	return db
}

func cleanupAuth(db *gorm.DB) {
	patterns := []string{"testuser_%", "dup_test_%", "login_test_%", "session_test_%", "logout_test_%", "temp_test_%", "revert_test_%", "role_test_%"}
	for _, p := range patterns {
		var users []models.User
		db.Unscoped().Where("username LIKE ?", p).Find(&users)
		for _, u := range users {
			db.Exec("DELETE FROM temp_accesses WHERE user_id = ?", u.ID)
			db.Exec("DELETE FROM sessions WHERE user_id = ?", u.ID)
			db.Unscoped().Where("id = ?", u.ID).Delete(&models.User{})
		}
	}
}

func TestRegister(t *testing.T) {
	db := setupAuthDB(t)
	defer cleanupAuth(db)
	svc := NewAuthService(db)

	user, err := svc.Register("testuser_900", "password123", "Test User", "test@test.com", models.RoleStudent, 1, nil)
	require.NoError(t, err)
	assert.Equal(t, "testuser_900", user.Username)
	assert.Equal(t, models.RoleStudent, user.Role)
	assert.True(t, user.Active)
	assert.NotEqual(t, "password123", user.PasswordHash)
}

func TestRegister_DuplicateUsername(t *testing.T) {
	db := setupAuthDB(t)
	defer cleanupAuth(db)
	svc := NewAuthService(db)

	_, err := svc.Register("dup_test_901", "pass", "A", "a@a.com", models.RoleStudent, 1, nil)
	require.NoError(t, err)

	_, err = svc.Register("dup_test_901", "pass", "B", "b@b.com", models.RoleStudent, 1, nil)
	assert.Error(t, err)
}

func TestLogin_Success(t *testing.T) {
	db := setupAuthDB(t)
	defer cleanupAuth(db)
	svc := NewAuthService(db)

	svc.Register("login_test_902", "mypassword", "User One", "", models.RoleAdmin, 1, nil)

	user, sessionID, err := svc.Login("login_test_902", "mypassword")
	require.NoError(t, err)
	assert.Equal(t, "login_test_902", user.Username)
	assert.NotEmpty(t, sessionID)
	assert.Len(t, sessionID, 64)
}

func TestLogin_WrongPassword(t *testing.T) {
	db := setupAuthDB(t)
	defer cleanupAuth(db)
	svc := NewAuthService(db)

	svc.Register("login_test_903", "mypassword", "User", "", models.RoleStudent, 1, nil)

	_, _, err := svc.Login("login_test_903", "wrongpassword")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid credentials")
}

func TestLogin_NonExistentUser(t *testing.T) {
	db := setupAuthDB(t)
	svc := NewAuthService(db)

	_, _, err := svc.Login("nobody_999", "pass")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid credentials")
}

func TestValidateSession(t *testing.T) {
	db := setupAuthDB(t)
	defer cleanupAuth(db)
	svc := NewAuthService(db)

	svc.Register("session_test_904", "pass", "User", "", models.RoleStudent, 1, nil)
	_, sessionID, _ := svc.Login("session_test_904", "pass")

	user, err := svc.ValidateSession(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "session_test_904", user.Username)
}

func TestValidateSession_Expired(t *testing.T) {
	db := setupAuthDB(t)
	defer cleanupAuth(db)
	svc := NewAuthService(db)

	svc.Register("session_test_905", "pass", "User", "", models.RoleStudent, 1, nil)
	var user models.User
	db.First(&user, "username = ?", "session_test_905")

	db.Create(&models.Session{
		ID: "expired-session-token-test", UserID: user.ID,
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	})

	_, err := svc.ValidateSession("expired-session-token-test")
	assert.Error(t, err)
}

func TestValidateSession_Invalid(t *testing.T) {
	db := setupAuthDB(t)
	svc := NewAuthService(db)

	_, err := svc.ValidateSession("nonexistent-session-token-xyz")
	assert.Error(t, err)
}

func TestLogout(t *testing.T) {
	db := setupAuthDB(t)
	defer cleanupAuth(db)
	svc := NewAuthService(db)

	svc.Register("logout_test_906", "pass", "User", "", models.RoleStudent, 1, nil)
	_, sessionID, _ := svc.Login("logout_test_906", "pass")

	err := svc.Logout(sessionID)
	assert.NoError(t, err)

	_, err = svc.ValidateSession(sessionID)
	assert.Error(t, err)
}

func TestGrantTempAccess(t *testing.T) {
	db := setupAuthDB(t)
	defer cleanupAuth(db)
	svc := NewAuthService(db)

	svc.Register("temp_test_907", "pass", "Student", "", models.RoleStudent, 1, nil)
	var user models.User
	db.First(&user, "username = ?", "temp_test_907")

	// Clear any stale temp access records
	db.Exec("DELETE FROM temp_accesses WHERE user_id = ?", user.ID)

	err := svc.GrantTempAccess(user.ID, models.RoleAdmin, user.ID, 8*time.Hour)
	require.NoError(t, err)

	// Verify temp access record was created correctly in DB
	var ta models.TempAccess
	db.Where("user_id = ? AND granted_role = ? AND expires_at > ?", user.ID, models.RoleAdmin, time.Now()).
		Order("created_at DESC").First(&ta)
	require.NotZero(t, ta.ID, "temp access record should exist")
	assert.Equal(t, models.RoleAdmin, ta.GrantedRole)
	assert.Equal(t, models.RoleStudent, ta.OriginalRole)
	assert.True(t, ta.ExpiresAt.After(time.Now()))
	assert.False(t, ta.Reverted)
}

func TestRevertExpiredAccess(t *testing.T) {
	db := setupAuthDB(t)
	defer cleanupAuth(db)
	svc := NewAuthService(db)

	svc.Register("revert_test_908", "pass", "Student", "", models.RoleStudent, 1, nil)
	var user models.User
	db.First(&user, "username = ?", "revert_test_908")

	db.Create(&models.TempAccess{
		UserID: user.ID, GrantedRole: models.RoleAdmin, OriginalRole: models.RoleStudent,
		GrantedBy: user.ID, ExpiresAt: time.Now().Add(-1 * time.Hour), Reverted: false,
	})

	svc.RevertExpiredAccess()

	var ta models.TempAccess
	db.Where("user_id = ?", user.ID).First(&ta)
	assert.True(t, ta.Reverted)
}

func TestChangeUserRole(t *testing.T) {
	db := setupAuthDB(t)
	defer cleanupAuth(db)
	svc := NewAuthService(db)

	svc.Register("role_test_909", "pass", "User", "", models.RoleStudent, 1, nil)
	var user models.User
	db.First(&user, "username = ?", "role_test_909")

	err := svc.ChangeUserRole(user.ID, models.RoleClinician)
	assert.NoError(t, err)

	db.First(&user, user.ID)
	assert.Equal(t, models.RoleClinician, user.Role)
}
