//go:build ignore

package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campus-portal/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ===================== RBAC TESTS =====================

func TestRequireRole_Allowed(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userRole", models.RoleAdmin); c.Next() })
	r.GET("/test", RequireRole(models.RoleAdmin, models.RoleStaff), func(c *gin.Context) { c.Status(200) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}

func TestRequireRole_Denied(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userRole", models.RoleStudent); c.Next() })
	r.GET("/test", RequireRole(models.RoleAdmin), func(c *gin.Context) { c.Status(200) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 403, w.Code)
}

func TestRequireRole_MultipleRoles(t *testing.T) {
	for _, role := range []models.Role{models.RoleClinician, models.RoleAdmin} {
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set("userRole", role); c.Next() })
		r.GET("/test", RequireRole(models.RoleClinician, models.RoleAdmin), func(c *gin.Context) { c.Status(200) })

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code)
	}
}

// ===================== DATA SCOPE TESTS =====================

func TestDataScope_StudentSelf(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userRole", models.RoleStudent)
		c.Set("userID", uint(1))
		c.Next()
	})
	r.Use(DataScope())
	r.GET("/test", func(c *gin.Context) {
		assert.True(t, EnforceSelfScope(c, 1))
		assert.False(t, EnforceSelfScope(c, 2))
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}

func TestDataScope_AdminOrg(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userRole", models.RoleAdmin)
		c.Set("userID", uint(1))
		c.Next()
	})
	r.Use(DataScope())
	r.GET("/test", func(c *gin.Context) {
		assert.True(t, EnforceSelfScope(c, 1))
		assert.True(t, EnforceSelfScope(c, 99))
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}

func TestDataScope_ClinicianDepartment_DenyCrossUser(t *testing.T) {
	deptID := uint(1)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userRole", models.RoleClinician)
		c.Set("userID", uint(1))
		c.Set("user", &models.User{ID: 1, Role: models.RoleClinician, DepartmentID: &deptID})
		c.Next()
	})
	r.Use(DataScope())
	r.GET("/test", func(c *gin.Context) {
		// EnforceSelfScope denies cross-user for department scope
		assert.False(t, EnforceSelfScope(c, 99))
		// Own records always OK
		assert.True(t, EnforceSelfScope(c, 1))
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}

// Finding 4: nil department fails closed
func TestEnforceDeptScope_NilDepartment_DenyCrossUser(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userRole", models.RoleClinician)
		c.Set("userID", uint(1))
		c.Set("orgID", uint(1))
		c.Set("scopeType", "department")
		// User has nil department
		c.Set("user", &models.User{ID: 1, Role: models.RoleClinician, DepartmentID: nil, OrganizationID: 1})
		c.Next()
	})
	r.GET("/test", func(c *gin.Context) {
		// nil dept should DENY cross-user access
		assert.False(t, EnforceDeptScope(c, nil, 99))
		// Own record still OK
		assert.True(t, EnforceDeptScope(c, nil, 1))
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}

// ===================== HMAC TESTS =====================

func computeBodyHash(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

func signHMAC(secret, method, path, ts, bodyHash string) string {
	message := fmt.Sprintf("%s:%s:%s:%s", method, path, ts, bodyHash)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestHMACAuth_ValidSignature(t *testing.T) {
	secret := "test-secret"
	r := gin.New()
	r.Use(HMACAuth(secret))
	r.GET("/test", func(c *gin.Context) { c.Status(200) })

	ts := time.Now().Format(time.RFC3339)
	bodyHash := computeBodyHash("") // empty body for GET
	sig := signHMAC(secret, "GET", "/test", ts, bodyHash)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("X-HMAC-Signature", sig)
	req.Header.Set("X-HMAC-Timestamp", ts)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}

func TestHMACAuth_TamperedBody(t *testing.T) {
	secret := "test-secret"
	r := gin.New()
	r.Use(HMACAuth(secret))
	r.POST("/test", func(c *gin.Context) { c.Status(200) })

	ts := time.Now().Format(time.RFC3339)
	originalBody := `{"key":"value"}`
	bodyHash := computeBodyHash(originalBody)
	sig := signHMAC(secret, "POST", "/test", ts, bodyHash)

	// Send with different body — should fail
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/test", strings.NewReader(`{"key":"tampered"}`))
	req.Header.Set("X-HMAC-Signature", sig)
	req.Header.Set("X-HMAC-Timestamp", ts)
	r.ServeHTTP(w, req)
	assert.Equal(t, 401, w.Code)
}

func TestHMACAuth_MismatchedBodyHashHeader(t *testing.T) {
	secret := "test-secret"
	r := gin.New()
	r.Use(HMACAuth(secret))
	r.POST("/test", func(c *gin.Context) { c.Status(200) })

	ts := time.Now().Format(time.RFC3339)
	body := `{"data":"test"}`
	realHash := computeBodyHash(body)
	sig := signHMAC(secret, "POST", "/test", ts, realHash)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("X-HMAC-Signature", sig)
	req.Header.Set("X-HMAC-Timestamp", ts)
	req.Header.Set("X-Body-SHA256", "fakehash000") // Claimed hash doesn't match real body
	r.ServeHTTP(w, req)
	assert.Equal(t, 401, w.Code)
}

func TestHMACAuth_MissingHeaders(t *testing.T) {
	r := gin.New()
	r.Use(HMACAuth("secret"))
	r.GET("/test", func(c *gin.Context) { c.Status(200) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 401, w.Code)
}

func TestHMACAuth_WrongSignature(t *testing.T) {
	r := gin.New()
	r.Use(HMACAuth("secret"))
	r.GET("/test", func(c *gin.Context) { c.Status(200) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("X-HMAC-Signature", "bad-sig")
	req.Header.Set("X-HMAC-Timestamp", time.Now().Format(time.RFC3339))
	r.ServeHTTP(w, req)
	assert.Equal(t, 401, w.Code)
}

func TestHMACAuth_ExpiredTimestamp(t *testing.T) {
	secret := "test-secret"
	r := gin.New()
	r.Use(HMACAuth(secret))
	r.GET("/test", func(c *gin.Context) { c.Status(200) })

	ts := time.Now().Add(-10 * time.Minute).Format(time.RFC3339)
	bh := computeBodyHash("")
	sig := signHMAC(secret, "GET", "/test", ts, bh)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("X-HMAC-Signature", sig)
	req.Header.Set("X-HMAC-Timestamp", ts)
	r.ServeHTTP(w, req)
	assert.Equal(t, 401, w.Code)
}

func TestHMACAuth_FutureTimestamp(t *testing.T) {
	secret := "test-secret"
	r := gin.New()
	r.Use(HMACAuth(secret))
	r.GET("/test", func(c *gin.Context) { c.Status(200) })

	ts := time.Now().Add(10 * time.Minute).Format(time.RFC3339)
	bh := computeBodyHash("")
	sig := signHMAC(secret, "GET", "/test", ts, bh)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("X-HMAC-Signature", sig)
	req.Header.Set("X-HMAC-Timestamp", ts)
	r.ServeHTTP(w, req)
	assert.Equal(t, 401, w.Code)
}

func TestHMACAuth_EnforcesOnAnyRoute(t *testing.T) {
	r := gin.New()
	r.Use(HMACAuth("secret"))
	r.GET("/any/route", func(c *gin.Context) { c.Status(200) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/any/route", nil)
	r.ServeHTTP(w, req)
	// No HMAC headers → should fail
	assert.Equal(t, 401, w.Code)
}

// ===================== RATE LIMITER =====================

func TestRateLimit(t *testing.T) {
	r := gin.New()
	r.Use(RateLimit(3))
	r.GET("/test", func(c *gin.Context) { c.Status(200) })

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		r.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	r.ServeHTTP(w, req)
	assert.Equal(t, 429, w.Code)
}

// ===================== CSRF TESTS (Finding 13) =====================

func TestCSRF_ValidToken(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("authMethod", "session")
		c.Next()
	})
	r.Use(CSRFProtect())
	r.POST("/test", func(c *gin.Context) { c.Status(200) })

	// Set the CSRF cookie and include matching token in form
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/test", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test-csrf-token"})
	req.PostForm = map[string][]string{"csrf_token": {"test-csrf-token"}}
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}

func TestCSRF_MissingToken(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("authMethod", "session")
		c.Next()
	})
	r.Use(CSRFProtect())
	r.POST("/test", func(c *gin.Context) { c.Status(200) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/test", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	// No form token submitted
	r.ServeHTTP(w, req)
	assert.Equal(t, 403, w.Code)
}

func TestCSRF_SkippedForTokenAuth(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("authMethod", "token") // API token auth, not session
		c.Next()
	})
	r.Use(CSRFProtect())
	r.POST("/test", func(c *gin.Context) { c.Status(200) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/test", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// No CSRF token — should pass because auth is via API token
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}

func TestCSRF_GETRequestsSkipped(t *testing.T) {
	r := gin.New()
	r.Use(CSRFProtect())
	r.GET("/test", func(c *gin.Context) { c.Status(200) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}

// ===================== ORG SCOPE (Finding 2) =====================

func TestEnforceOrgScope_SameOrg(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("orgID", uint(1))
		c.Next()
	})
	r.GET("/test", func(c *gin.Context) {
		assert.True(t, EnforceOrgScope(c, 1))
		assert.False(t, EnforceOrgScope(c, 2))
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}
