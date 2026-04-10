package config

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"time"
)

type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	ServerPort string
	SessionKey string
	HMACSecret string

	UploadDir    string
	WatchedDir   string
	MaxUploadMB  int64
	AllowedTypes []string

	RateLimitPerMin int
	TempAccessDur   time.Duration
	SlowQueryMs     int64
	CacheTTL        time.Duration

	MVRefreshInterval time.Duration
	SecureCookies     bool

	SSOSyncEnabled  bool
	SSOSyncInterval time.Duration
	SSOSourcePath   string
}

func Load() *Config {
	return &Config{
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "campus_admin"),
		DBPassword: getEnv("DB_PASSWORD", "campus_secret"),
		DBName:     getEnv("DB_NAME", "campus_portal"),
		DBSSLMode:  getEnv("DB_SSLMODE", "disable"),

		ServerPort: getEnv("SERVER_PORT", "8080"),
		SessionKey: requireEnvOrGenerate("SESSION_KEY"),
		HMACSecret: requireEnvOrGenerate("HMAC_SECRET"),

		UploadDir:    getEnv("UPLOAD_DIR", "./uploads"),
		WatchedDir:   getEnv("WATCHED_DIR", "./watched_folder"),
		MaxUploadMB:  10,
		AllowedTypes: []string{"application/pdf", "image/jpeg", "image/png", "image/gif"},

		RateLimitPerMin:   60,
		TempAccessDur:     8 * time.Hour,
		SlowQueryMs:       500,
		CacheTTL:          5 * time.Minute,
		MVRefreshInterval: 15 * time.Minute,
		SSOSyncEnabled:  os.Getenv("SSO_SYNC_ENABLED") == "true",
		SSOSyncInterval: func() time.Duration {
			if v := os.Getenv("SSO_SYNC_INTERVAL"); v != "" {
				if d, err := time.ParseDuration(v); err == nil {
					return d
				}
			}
			return 15 * time.Minute
		}(),
		SSOSourcePath: getEnv("SSO_SOURCE_PATH", ""),

		// Default secure cookies to true in release mode, false in dev
		SecureCookies: func() bool {
			explicit := os.Getenv("SECURE_COOKIES")
			if explicit != "" {
				return explicit == "true"
			}
			return os.Getenv("GIN_MODE") == "release"
		}(),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// requireEnvOrGenerate returns the env var value, or generates a random secret
// and warns at startup. Production deployments MUST set these via env vars.
func requireEnvOrGenerate(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("Failed to generate random secret for %s: %v", key, err)
	}
	generated := hex.EncodeToString(b)
	log.Printf("WARNING: %s not set — generated random value. Set this env var for production.", key)
	return generated
}
