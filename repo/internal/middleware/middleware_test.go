package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campus-portal/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRequireRole_Allowed(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userRole", models.RoleAdmin)
		c.Next()
	})
	r.GET("/test", RequireRole(models.RoleAdmin, models.RoleStaff), func(c *gin.Context) {
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}

func TestRequireRole_Denied(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userRole", models.RoleStudent)
		c.Next()
	})
	r.GET("/test", RequireRole(models.RoleAdmin), func(c *gin.Context) {
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 403, w.Code)
}

func TestRequireRole_MultipleRoles(t *testing.T) {
	for _, role := range []models.Role{models.RoleClinician, models.RoleAdmin} {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("userRole", role)
			c.Next()
		})
		r.GET("/test", RequireRole(models.RoleClinician, models.RoleAdmin), func(c *gin.Context) {
			c.Status(200)
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code, "role %s should be allowed", role)
	}
}

func TestDataScope_StudentSelf(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userRole", models.RoleStudent)
		c.Set("userID", uint(1))
		c.Next()
	})
	r.Use(DataScope())
	r.GET("/test", func(c *gin.Context) {
		// Student can access own record
		assert.True(t, EnforceSelfScope(c, 1))
		// Student cannot access another's record
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
		// Admin can access any record
		assert.True(t, EnforceSelfScope(c, 1))
		assert.True(t, EnforceSelfScope(c, 99))
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}

func TestDataScope_ClinicianDepartment(t *testing.T) {
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
		// EnforceSelfScope denies cross-user access for department scope
		// (callers should use EnforceDeptScope with DB for cross-user)
		assert.False(t, EnforceSelfScope(c, 99))
		// Own records always allowed
		assert.True(t, EnforceSelfScope(c, 1))
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}

func TestHMACAuth_ValidSignature(t *testing.T) {
	secret := "test-secret"
	r := gin.New()
	r.Use(HMACAuth(secret))
	r.GET("/api/internal/test", func(c *gin.Context) {
		c.Status(200)
	})

	ts := time.Now().Format(time.RFC3339)
	bodyHash := "" // empty body for GET
	message := fmt.Sprintf("GET:/api/internal/test:%s:%s", ts, bodyHash)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	sig := hex.EncodeToString(mac.Sum(nil))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/internal/test", nil)
	req.Header.Set("X-HMAC-Signature", sig)
	req.Header.Set("X-HMAC-Timestamp", ts)
	req.Header.Set("X-Body-SHA256", bodyHash)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}

func TestHMACAuth_MissingHeaders(t *testing.T) {
	r := gin.New()
	r.Use(HMACAuth("secret"))
	r.GET("/api/internal/test", func(c *gin.Context) {
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/internal/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 401, w.Code)
}

func TestHMACAuth_WrongSignature(t *testing.T) {
	r := gin.New()
	r.Use(HMACAuth("secret"))
	r.GET("/api/internal/test", func(c *gin.Context) {
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/internal/test", nil)
	req.Header.Set("X-HMAC-Signature", "bad-sig")
	req.Header.Set("X-HMAC-Timestamp", time.Now().Format(time.RFC3339))
	r.ServeHTTP(w, req)
	assert.Equal(t, 401, w.Code)
}

func TestHMACAuth_ExpiredTimestamp(t *testing.T) {
	secret := "test-secret"
	r := gin.New()
	r.Use(HMACAuth(secret))
	r.GET("/api/internal/test", func(c *gin.Context) {
		c.Status(200)
	})

	ts := time.Now().Add(-10 * time.Minute).Format(time.RFC3339)
	message := fmt.Sprintf("GET:/api/internal/test:%s", ts)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	sig := hex.EncodeToString(mac.Sum(nil))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/internal/test", nil)
	req.Header.Set("X-HMAC-Signature", sig)
	req.Header.Set("X-HMAC-Timestamp", ts)
	r.ServeHTTP(w, req)
	assert.Equal(t, 401, w.Code)
}

func TestHMACAuth_SkipsNonInternalRoutes(t *testing.T) {
	r := gin.New()
	r.Use(HMACAuth("secret"))
	r.GET("/api/public/test", func(c *gin.Context) {
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/public/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code) // No HMAC required for non-internal
}

func TestRateLimit(t *testing.T) {
	r := gin.New()
	r.Use(RateLimit(3)) // 3 requests per minute
	r.GET("/test", func(c *gin.Context) {
		c.Status(200)
	})

	// First 3 should pass
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		r.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code, "request %d should pass", i+1)
	}

	// 4th should be rate limited
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	r.ServeHTTP(w, req)
	assert.Equal(t, 429, w.Code)
}
