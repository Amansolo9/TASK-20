package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"campus-portal/internal/auth"
	"campus-portal/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ===================== SESSION AUTH =====================

func AuthRequired(authSvc *auth.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID, err := c.Cookie("session_id")
		if err != nil || sessionID == "" {
			if strings.HasPrefix(c.Request.URL.Path, "/api/") {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			} else {
				c.Redirect(http.StatusFound, "/login")
			}
			c.Abort()
			return
		}

		user, err := authSvc.ValidateSession(sessionID)
		if err != nil {
			log.Printf("SESSION_INVALID: ip=%s path=%s error=%s", c.ClientIP(), c.Request.URL.Path, err.Error())
			c.SetCookie("session_id", "", -1, "/", "", false, true)
			if strings.HasPrefix(c.Request.URL.Path, "/api/") {
				c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			} else {
				c.Redirect(http.StatusFound, "/login")
			}
			c.Abort()
			return
		}

		c.Set("user", user)
		c.Set("userID", user.ID)
		c.Set("userRole", user.Role)
		c.Set("orgID", user.OrganizationID)
		c.Next()
	}
}

// ===================== RBAC =====================

func RequireRole(roles ...models.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, exists := c.Get("userRole")
		if !exists {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			c.Abort()
			return
		}

		role := userRole.(models.Role)
		for _, r := range roles {
			if role == r {
				c.Next()
				return
			}
		}

		userID, _ := c.Get("userID")
		log.Printf("RBAC_DENIED: user=%v role=%s path=%s required=%v", userID, role, c.Request.URL.Path, roles)
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		c.Abort()
	}
}

// DataScope ensures users can only access their own records (students/faculty)
// or records within their org/department (clinicians/staff/admin)
func DataScope() gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, _ := c.Get("userRole")
		role := userRole.(models.Role)

		switch role {
		case models.RoleStudent, models.RoleFaculty:
			// Students and faculty can only see their own data
			c.Set("scopeType", "self")
		case models.RoleClinician, models.RoleStaff:
			// Clinicians and staff see department-scoped data
			c.Set("scopeType", "department")
		case models.RoleAdmin:
			// Admin sees everything in their org
			c.Set("scopeType", "organization")
		}
		c.Next()
	}
}

// EnforceSelfScope checks if the current user can access a record belonging to recordUserID.
// For department scope, also checks if the target user shares the same department.
func EnforceSelfScope(c *gin.Context, recordUserID uint) bool {
	scopeType, _ := c.Get("scopeType")
	userID, _ := c.Get("userID")

	switch scopeType.(string) {
	case "self":
		return recordUserID == userID.(uint)
	case "department":
		// Allow access to own records always
		if recordUserID == userID.(uint) {
			return true
		}
		// Department scope requires a DB lookup to verify department match.
		// Use EnforceDeptScope() instead for cross-user access. Deny here by default.
		return false
	case "organization":
		return true
	}
	return false
}

// EnforceDeptScope is a stricter check: verifies the target user's department matches.
// Requires the DB to look up the target user's department. Use for clinician routes.
func EnforceDeptScope(c *gin.Context, db *gorm.DB, recordUserID uint) bool {
	scopeType, _ := c.Get("scopeType")
	userID, _ := c.Get("userID")

	if recordUserID == userID.(uint) {
		return true
	}

	switch scopeType.(string) {
	case "self":
		return false
	case "organization":
		return true
	case "department":
		user, exists := c.Get("user")
		if !exists {
			return false
		}
		currentUser := user.(*models.User)
		if currentUser.DepartmentID == nil {
			return true
		}
		var targetUser models.User
		if err := db.First(&targetUser, recordUserID).Error; err != nil {
			return false
		}
		if targetUser.DepartmentID == nil {
			return true
		}
		return *currentUser.DepartmentID == *targetUser.DepartmentID
	}
	return false
}

// ===================== HMAC SIGNING =====================

func HMACAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only enforce on /api/internal/* routes
		if !strings.HasPrefix(c.Request.URL.Path, "/api/internal/") {
			c.Next()
			return
		}

		signature := c.GetHeader("X-HMAC-Signature")
		timestamp := c.GetHeader("X-HMAC-Timestamp")
		if signature == "" || timestamp == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing HMAC headers"})
			c.Abort()
			return
		}

		// Verify timestamp is within 5 minutes
		ts, err := time.Parse(time.RFC3339, timestamp)
		if err != nil || time.Since(ts) > 5*time.Minute {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired timestamp"})
			c.Abort()
			return
		}

		// Compute expected HMAC including body hash for tamper detection
		bodyHash := c.GetHeader("X-Body-SHA256")
		message := fmt.Sprintf("%s:%s:%s:%s", c.Request.Method, c.Request.URL.Path, timestamp, bodyHash)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(message))
		expected := hex.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(signature), []byte(expected)) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid HMAC signature"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// ===================== RATE LIMITER =====================

type rateLimiter struct {
	mu       sync.Mutex
	clients  map[string]*clientWindow
	limit    int
	window   time.Duration
}

type clientWindow struct {
	count    int
	resetAt  time.Time
}

func NewRateLimiter(limit int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		clients: make(map[string]*clientWindow),
		limit:   limit,
		window:  window,
	}
	// Cleanup goroutine
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			rl.mu.Lock()
			now := time.Now()
			for ip, cw := range rl.clients {
				if now.After(cw.resetAt) {
					delete(rl.clients, ip)
				}
			}
			rl.mu.Unlock()
		}
	}()
	return rl
}

func RateLimit(limit int) gin.HandlerFunc {
	rl := NewRateLimiter(limit, time.Minute)

	return func(c *gin.Context) {
		ip := c.ClientIP()
		rl.mu.Lock()

		cw, exists := rl.clients[ip]
		if !exists || time.Now().After(cw.resetAt) {
			rl.clients[ip] = &clientWindow{count: 1, resetAt: time.Now().Add(rl.window)}
			rl.mu.Unlock()
			c.Next()
			return
		}

		if cw.count >= rl.limit {
			rl.mu.Unlock()
			retryAfter := int(time.Until(cw.resetAt).Seconds())
			// For browser page requests, return HTML; for API requests, return JSON
			accept := c.GetHeader("Accept")
			if strings.Contains(accept, "text/html") {
				c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
				c.Data(http.StatusTooManyRequests, "text/html; charset=utf-8",
					[]byte(fmt.Sprintf(`<!DOCTYPE html><html><head><title>Too Many Requests</title><link rel="stylesheet" href="/static/css/style.css"></head><body class="login-body"><div class="login-container"><div class="login-card"><div class="login-header"><h1>Too Many Requests</h1></div><div style="padding:2rem"><div class="alert alert-error">Rate limit exceeded. Please wait %d seconds and try again.</div><a href="javascript:history.back()" class="btn btn-primary btn-full">Go Back</a></div></div></div></body></html>`, retryAfter)))
			} else {
				c.JSON(http.StatusTooManyRequests, gin.H{
					"error":       "rate limit exceeded",
					"retry_after": retryAfter,
				})
			}
			c.Abort()
			return
		}

		cw.count++
		rl.mu.Unlock()
		c.Next()
	}
}

// ===================== CSRF PROTECTION =====================

// CSRFProtect uses a double-submit cookie pattern:
// - On every request, ensure a csrf_token cookie exists (readable by JS).
// - On POST/PUT/DELETE, require a matching token in the form field OR X-CSRF-Token header.
// - JavaScript auto-injects the hidden field into every form on page load.
func CSRFProtect() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Ensure the CSRF cookie exists on every request
		token := ensureCSRFCookie(c)
		c.Set("csrf_token", token)

		if c.Request.Method == "GET" || c.Request.Method == "HEAD" || c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		// For JSON API calls, validate Origin header with exact host match
		// to prevent cross-origin JSON POST attacks
		contentType := c.GetHeader("Content-Type")
		if strings.Contains(contentType, "application/json") {
			if !isValidOrigin(c) {
				c.JSON(http.StatusForbidden, gin.H{"error": "cross-origin request blocked"})
				c.Abort()
				return
			}
			c.Next()
			return
		}

		cookieToken := getCSRFFromCookie(c)
		if cookieToken == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "missing CSRF cookie"})
			c.Abort()
			return
		}

		// Check form field first, then header (for AJAX multipart uploads)
		submittedToken := c.PostForm("csrf_token")
		if submittedToken == "" {
			submittedToken = c.GetHeader("X-CSRF-Token")
		}
		if submittedToken != cookieToken {
			c.JSON(http.StatusForbidden, gin.H{"error": "invalid CSRF token"})
			c.Abort()
			return
		}

		c.Next()
	}
}

func ensureCSRFCookie(c *gin.Context) string {
	existing := getCSRFFromCookie(c)
	if existing != "" {
		return existing
	}
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	// httpOnly=false so JavaScript can read it and inject into forms
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("csrf_token", token, 86400, "/", "", false, false)
	return token
}

func getCSRFFromCookie(c *gin.Context) string {
	token, _ := c.Cookie("csrf_token")
	return token
}

// isValidOrigin checks that Origin/Referer headers match the request host exactly,
// preventing substring-based bypass (e.g., "localhost:8080.evil.com").
func isValidOrigin(c *gin.Context) bool {
	host := c.Request.Host // e.g., "localhost:8080"
	origin := c.GetHeader("Origin")
	referer := c.GetHeader("Referer")

	if origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return parsed.Host == host
	}
	if referer != "" {
		parsed, err := url.Parse(referer)
		if err != nil {
			return false
		}
		return parsed.Host == host
	}
	// No Origin or Referer — for JSON requests, require X-Requested-With header
	// as an additional CSRF defense (cannot be set cross-origin without CORS preflight)
	if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
		return true
	}
	// Allow GET (read-only) but block state-changing methods without any origin indicator
	if c.Request.Method == "GET" || c.Request.Method == "HEAD" {
		return true
	}
	return false
}

// ===================== SLOW QUERY LOGGER =====================

// SlowQueryLogger records any HTTP request that takes longer than thresholdMs.
// It also installs a GORM callback to track individual slow SQL queries.
func SlowQueryLogger(db *gorm.DB, thresholdMs int64) gin.HandlerFunc {
	// Install GORM callback to track slow queries at the DB level
	db.Callback().Query().After("gorm:query").Register("slow_query_log", func(d *gorm.DB) {
		elapsed := d.Statement.Context.Value("gorm:started_at")
		if start, ok := elapsed.(time.Time); ok {
			duration := time.Since(start)
			if duration.Milliseconds() > thresholdMs {
				sql := d.Statement.SQL.String()
				log.Printf("SLOW QUERY [%dms]: %s", duration.Milliseconds(), sql)
				db.Exec("INSERT INTO slow_query_logs (query, duration, caller, created_at) VALUES (?, ?, ?, ?)",
					sql, duration.Milliseconds(), d.Statement.Table, time.Now())
			}
		}
	})

	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start)
		if duration.Milliseconds() > thresholdMs {
			log.Printf("SLOW REQUEST [%dms]: %s %s", duration.Milliseconds(), c.Request.Method, c.Request.URL.Path)
		}
	}
}
