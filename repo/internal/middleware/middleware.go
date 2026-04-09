package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"math"
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

// ===================== SESSION AUTH (browser pages) =====================

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
		c.Set("authMethod", "session")
		c.Next()
	}
}

// ===================== API TOKEN AUTH (REST API endpoints) =====================

// APITokenRequired validates a Bearer token from the Authorization header.
// Rejects session-cookie-only access — API routes require a locally-issued token.
func APITokenRequired(authSvc *auth.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Bearer token required"})
			c.Abort()
			return
		}
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

		user, err := authSvc.ValidateAPIToken(tokenStr)
		if err != nil {
			log.Printf("TOKEN_INVALID: ip=%s path=%s error=%s", c.ClientIP(), c.Request.URL.Path, err.Error())
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			c.Abort()
			return
		}

		c.Set("user", user)
		c.Set("userID", user.ID)
		c.Set("userRole", user.Role)
		c.Set("orgID", user.OrganizationID)
		c.Set("authMethod", "token")
		c.Next()
	}
}

// SessionOrTokenAuth accepts either session cookie or Bearer token.
// Used for endpoints that serve both browser and programmatic access.
func SessionOrTokenAuth(authSvc *auth.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Try Bearer token first
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			user, err := authSvc.ValidateAPIToken(tokenStr)
			if err == nil {
				c.Set("user", user)
				c.Set("userID", user.ID)
				c.Set("userRole", user.Role)
				c.Set("orgID", user.OrganizationID)
				c.Set("authMethod", "token")
				c.Next()
				return
			}
		}
		// Fall back to session cookie
		sessionID, err := c.Cookie("session_id")
		if err != nil || sessionID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required (Bearer token or session cookie)"})
			c.Abort()
			return
		}
		user, err := authSvc.ValidateSession(sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			c.Abort()
			return
		}
		c.Set("user", user)
		c.Set("userID", user.ID)
		c.Set("userRole", user.Role)
		c.Set("orgID", user.OrganizationID)
		c.Set("authMethod", "session")
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

// ===================== DATA SCOPE =====================

func DataScope() gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, _ := c.Get("userRole")
		role := userRole.(models.Role)

		switch role {
		case models.RoleStudent, models.RoleFaculty:
			c.Set("scopeType", "self")
		case models.RoleClinician, models.RoleStaff:
			c.Set("scopeType", "department")
		case models.RoleAdmin:
			c.Set("scopeType", "organization")
		}
		c.Next()
	}
}

// EnforceSelfScope checks record access by scope. Denies cross-user for dept scope (use EnforceDeptScope for that).
func EnforceSelfScope(c *gin.Context, recordUserID uint) bool {
	scopeType, _ := c.Get("scopeType")
	userID, _ := c.Get("userID")

	switch scopeType.(string) {
	case "self":
		return recordUserID == userID.(uint)
	case "department":
		if recordUserID == userID.(uint) {
			return true
		}
		return false // Must use EnforceDeptScope for cross-user
	case "organization":
		return true
	}
	return false
}

// EnforceDeptScope verifies dept match via DB. Fail-closed: nil dept denies cross-user access.
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
		// Org-scoped: also verify same org
		orgID, _ := c.Get("orgID")
		var targetUser models.User
		if err := db.First(&targetUser, recordUserID).Error; err != nil {
			return false
		}
		return targetUser.OrganizationID == orgID.(uint)
	case "department":
		user, exists := c.Get("user")
		if !exists {
			return false
		}
		currentUser := user.(*models.User)
		// FAIL CLOSED: if requester has no department, deny cross-user access
		if currentUser.DepartmentID == nil {
			return false
		}
		var targetUser models.User
		if err := db.First(&targetUser, recordUserID).Error; err != nil {
			return false
		}
		// Also enforce same org
		if targetUser.OrganizationID != currentUser.OrganizationID {
			return false
		}
		// FAIL CLOSED: if target has no department, deny
		if targetUser.DepartmentID == nil {
			return false
		}
		return *currentUser.DepartmentID == *targetUser.DepartmentID
	}
	return false
}

// EnforceOrgScope checks that a record's org matches the requester's org.
func EnforceOrgScope(c *gin.Context, recordOrgID uint) bool {
	orgID, exists := c.Get("orgID")
	if !exists {
		return false
	}
	return recordOrgID == orgID.(uint)
}

// ===================== HMAC SIGNING =====================

// HMACAuth verifies HMAC signatures on API requests.
// It reads the actual request body, computes SHA-256, and verifies the signature.
// The body is restored for downstream handlers.
func HMACAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		signature := c.GetHeader("X-HMAC-Signature")
		timestamp := c.GetHeader("X-HMAC-Timestamp")
		if signature == "" || timestamp == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing HMAC headers"})
			c.Abort()
			return
		}

		// ABSOLUTE skew validation: reject both old AND future timestamps beyond 5 min
		ts, err := time.Parse(time.RFC3339, timestamp)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid timestamp format"})
			c.Abort()
			return
		}
		skew := math.Abs(time.Since(ts).Seconds())
		if skew > 300 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "timestamp outside allowed skew (±5 min)"})
			c.Abort()
			return
		}

		// Read actual body bytes and compute SHA-256
		var bodyBytes []byte
		if c.Request.Body != nil {
			bodyBytes, err = io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read request body"})
				c.Abort()
				return
			}
			// Restore body for downstream handlers
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
		computedHash := sha256.Sum256(bodyBytes)
		computedHashHex := hex.EncodeToString(computedHash[:])

		// If client provided X-Body-SHA256, it must match the computed hash
		claimedHash := c.GetHeader("X-Body-SHA256")
		if claimedHash != "" && claimedHash != computedHashHex {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "body hash mismatch"})
			c.Abort()
			return
		}

		// Compute HMAC over method:path:timestamp:bodyHash
		message := fmt.Sprintf("%s:%s:%s:%s", c.Request.Method, c.Request.URL.Path, timestamp, computedHashHex)
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
	mu      sync.Mutex
	clients map[string]*clientWindow
	limit   int
	window  time.Duration
}

type clientWindow struct {
	count   int
	resetAt time.Time
}

func NewRateLimiter(limit int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		clients: make(map[string]*clientWindow),
		limit:   limit,
		window:  window,
	}
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
			accept := c.GetHeader("Accept")
			if strings.Contains(accept, "text/html") {
				c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
				c.Data(http.StatusTooManyRequests, "text/html; charset=utf-8",
					[]byte(fmt.Sprintf(`<!DOCTYPE html><html><head><title>Too Many Requests</title><link rel="stylesheet" href="/static/css/style.css"></head><body class="login-body"><div class="login-container"><div class="login-card"><div class="login-header"><h1>Too Many Requests</h1></div><div style="padding:2rem"><div class="alert alert-error">Rate limit exceeded. Please wait %d seconds.</div><a href="javascript:history.back()" class="btn btn-primary btn-full">Go Back</a></div></div></div></body></html>`, retryAfter)))
			} else {
				c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded", "retry_after": retryAfter})
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

func CSRFProtect() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := ensureCSRFCookie(c)
		c.Set("csrf_token", token)

		if c.Request.Method == "GET" || c.Request.Method == "HEAD" || c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		// Skip CSRF for token-authenticated API requests (tokens are not cookie-based)
		if method, exists := c.Get("authMethod"); exists && method == "token" {
			c.Next()
			return
		}

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
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("csrf_token", token, 86400, "/", "", false, false)
	return token
}

func getCSRFFromCookie(c *gin.Context) string {
	token, _ := c.Cookie("csrf_token")
	return token
}

func isValidOrigin(c *gin.Context) bool {
	host := c.Request.Host
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
	if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
		return true
	}
	if c.Request.Method == "GET" || c.Request.Method == "HEAD" {
		return true
	}
	return false
}

// ===================== SLOW QUERY LOGGER =====================

// SlowQueryLogger instruments both HTTP request duration and GORM query duration.
// The GORM callback uses the statement's context to track timing.
func SlowQueryLogger(db *gorm.DB, thresholdMs int64) gin.HandlerFunc {
	// Register a GORM "before" callback to record start time
	_ = db.Callback().Query().Before("gorm:query").Register("slow_query_start", func(d *gorm.DB) {
		d.Set("slow_query_start", time.Now())
	})

	// Register a GORM "after" callback to measure elapsed
	_ = db.Callback().Query().After("gorm:query").Register("slow_query_log", func(d *gorm.DB) {
		val, ok := d.Get("slow_query_start")
		if !ok {
			return
		}
		start, ok := val.(time.Time)
		if !ok {
			return
		}
		elapsed := time.Since(start).Milliseconds()
		if elapsed > thresholdMs {
			sql := d.Statement.SQL.String()
			log.Printf("SLOW QUERY [%dms]: %s", elapsed, sql)
			db.Exec("INSERT INTO slow_query_logs (query, duration, caller, created_at) VALUES (?, ?, ?, ?)",
				sql, elapsed, d.Statement.Table, time.Now())
		}
	})

	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		elapsed := time.Since(start).Milliseconds()
		if elapsed > thresholdMs {
			log.Printf("SLOW REQUEST [%dms]: %s %s", elapsed, c.Request.Method, c.Request.URL.Path)
		}
	}
}
