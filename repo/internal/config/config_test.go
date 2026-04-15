package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear env vars to test defaults
	envVars := []string{"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME",
		"SERVER_PORT", "SESSION_KEY", "HMAC_SECRET", "UPLOAD_DIR", "WATCHED_DIR",
		"SSO_SYNC_ENABLED", "SSO_SYNC_INTERVAL", "SSO_SOURCE_PATH"}
	saved := make(map[string]string)
	for _, k := range envVars {
		saved[k] = os.Getenv(k)
		os.Unsetenv(k)
	}
	defer func() {
		for k, v := range saved {
			if v != "" {
				os.Setenv(k, v)
			}
		}
	}()

	cfg := Load()

	assert.Equal(t, "localhost", cfg.DBHost)
	assert.Equal(t, "5432", cfg.DBPort)
	assert.Equal(t, "campus_admin", cfg.DBUser)
	assert.Equal(t, "campus_secret", cfg.DBPassword)
	assert.Equal(t, "campus_portal", cfg.DBName)
	assert.Equal(t, "disable", cfg.DBSSLMode)
	assert.Equal(t, "8080", cfg.ServerPort)
	assert.Equal(t, "./uploads", cfg.UploadDir)
	assert.Equal(t, "./watched_folder", cfg.WatchedDir)
	assert.Equal(t, int64(10), cfg.MaxUploadMB)
	assert.Equal(t, 60, cfg.RateLimitPerMin)
	assert.Equal(t, int64(500), cfg.SlowQueryMs)
	assert.Equal(t, 5*time.Minute, cfg.CacheTTL)
	assert.Equal(t, 15*time.Minute, cfg.MVRefreshInterval)

	// Session key and HMAC secret should be auto-generated
	assert.NotEmpty(t, cfg.SessionKey, "SessionKey should be auto-generated")
	assert.NotEmpty(t, cfg.HMACSecret, "HMACSecret should be auto-generated")
	assert.Len(t, cfg.SessionKey, 64, "auto-generated key should be 64 hex chars")

	// SSO defaults
	assert.False(t, cfg.SSOSyncEnabled)
	assert.Equal(t, 15*time.Minute, cfg.SSOSyncInterval)
	assert.Empty(t, cfg.SSOSourcePath)
}

func TestLoad_EnvOverrides(t *testing.T) {
	os.Setenv("DB_HOST", "custom-host")
	os.Setenv("DB_PORT", "5433")
	os.Setenv("SERVER_PORT", "9090")
	os.Setenv("SESSION_KEY", "custom-session-key")
	os.Setenv("HMAC_SECRET", "custom-hmac-secret")
	os.Setenv("SSO_SYNC_ENABLED", "true")
	os.Setenv("SSO_SYNC_INTERVAL", "30m")
	os.Setenv("SSO_SOURCE_PATH", "/shared/sso.json")
	defer func() {
		os.Unsetenv("DB_HOST")
		os.Unsetenv("DB_PORT")
		os.Unsetenv("SERVER_PORT")
		os.Unsetenv("SESSION_KEY")
		os.Unsetenv("HMAC_SECRET")
		os.Unsetenv("SSO_SYNC_ENABLED")
		os.Unsetenv("SSO_SYNC_INTERVAL")
		os.Unsetenv("SSO_SOURCE_PATH")
	}()

	cfg := Load()

	assert.Equal(t, "custom-host", cfg.DBHost)
	assert.Equal(t, "5433", cfg.DBPort)
	assert.Equal(t, "9090", cfg.ServerPort)
	assert.Equal(t, "custom-session-key", cfg.SessionKey)
	assert.Equal(t, "custom-hmac-secret", cfg.HMACSecret)
	assert.True(t, cfg.SSOSyncEnabled)
	assert.Equal(t, 30*time.Minute, cfg.SSOSyncInterval)
	assert.Equal(t, "/shared/sso.json", cfg.SSOSourcePath)
}

func TestLoad_AllowedTypes(t *testing.T) {
	cfg := Load()

	assert.Contains(t, cfg.AllowedTypes, "application/pdf")
	assert.Contains(t, cfg.AllowedTypes, "image/jpeg")
	assert.Contains(t, cfg.AllowedTypes, "image/png")
	assert.Contains(t, cfg.AllowedTypes, "image/gif")
	assert.Len(t, cfg.AllowedTypes, 4)
}

func TestLoad_SecureCookies_ReleaseMode(t *testing.T) {
	os.Setenv("GIN_MODE", "release")
	os.Unsetenv("SECURE_COOKIES")
	defer func() {
		os.Unsetenv("GIN_MODE")
	}()

	cfg := Load()
	assert.True(t, cfg.SecureCookies, "release mode should enable secure cookies")
}

func TestLoad_SecureCookies_ExplicitOverride(t *testing.T) {
	os.Setenv("SECURE_COOKIES", "false")
	os.Setenv("GIN_MODE", "release")
	defer func() {
		os.Unsetenv("SECURE_COOKIES")
		os.Unsetenv("GIN_MODE")
	}()

	cfg := Load()
	assert.False(t, cfg.SecureCookies, "explicit false should override release mode")
}

func TestLoad_SSOSyncInterval_InvalidDuration(t *testing.T) {
	os.Setenv("SSO_SYNC_INTERVAL", "not-a-duration")
	defer os.Unsetenv("SSO_SYNC_INTERVAL")

	cfg := Load()
	assert.Equal(t, 15*time.Minute, cfg.SSOSyncInterval, "invalid duration should fall back to default")
}
