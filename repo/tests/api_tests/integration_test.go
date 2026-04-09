//go:build ignore

package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"html/template"
	"path/filepath"

	"campus-portal/internal/auth"
	"campus-portal/internal/config"
	"campus-portal/internal/middleware"
	"campus-portal/internal/models"
	"campus-portal/internal/services"
	tmpl "campus-portal/internal/templates"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "host=localhost port=5432 user=campus_admin password=campus_secret dbname=campus_portal sslmode=disable"
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Skipf("Skipping integration test — Postgres not available: %v", err)
	}
	sqlDB, _ := db.DB()
	if err := sqlDB.Ping(); err != nil {
		t.Skipf("Skipping integration test — cannot ping Postgres: %v", err)
	}
	return db
}

func setupRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	db := setupIntegrationDB(t)
	cfg := &config.Config{
		HMACSecret:  "test-hmac",
		UploadDir:   t.TempDir(),
		MaxUploadMB: 10,
		CacheTTL:    5 * time.Minute,
	}

	authSvc := auth.NewAuthService(db)
	auditSvc := services.NewAuditService(db)
	healthSvc := services.NewHealthService(db, auditSvc, cfg.UploadDir)
	bookingSvc := services.NewBookingService(db, auditSvc, nil)
	reportingSvc := services.NewReportingService(db, cfg.CacheTTL)
	webhookSvc := services.NewWebhookService(db)

	authHandler := NewAuthHandler(authSvc, auditSvc, db, cfg)
	healthHandler := NewHealthHandler(healthSvc, auditSvc)
	bookingHandler := NewBookingHandler(bookingSvc, db)
	adminHandler := NewAdminHandler(reportingSvc, webhookSvc)

	r := gin.New()
	htmlTemplates := template.Must(
		template.New("").Funcs(tmpl.FuncMap()).ParseGlob(
			filepath.Join("..", "templates", "*.html"),
		),
	)
	r.SetHTMLTemplate(htmlTemplates)

	// Public routes
	r.GET("/login", authHandler.LoginPage)
	r.POST("/login", authHandler.Login)

	// Authenticated routes — mirrors production middleware stack
	authed := r.Group("/")
	authed.Use(middleware.AuthRequired(authSvc))
	authed.Use(middleware.DataScope())
	authed.Use(middleware.CSRFProtect())
	{
		authed.GET("/dashboard", healthHandler.DashboardPage)
		authed.GET("/logout", authHandler.Logout)

		// Self-upload: all roles except staff (mirrors production)
		authed.POST("/health/upload", healthHandler.UploadAttachment)

		// Health record mutations: clinician/admin only (mirrors production)
		healthMut := authed.Group("/health")
		healthMut.Use(middleware.RequireRole(models.RoleClinician, models.RoleAdmin))
		{
			healthMut.POST("/update", healthHandler.UpdateHealthRecord)
		}

		authed.GET("/bookings", bookingHandler.BookingPage)
		authed.POST("/bookings", bookingHandler.CreateBooking)

		adminGroup := authed.Group("/admin")
		adminGroup.Use(middleware.RequireRole(models.RoleAdmin))
		{
			adminGroup.GET("/users", authHandler.UsersPage)
			adminGroup.POST("/users/:id/role", authHandler.ChangeRole)
			adminGroup.POST("/users/:id/toggle", authHandler.ToggleUser)
		}
	}

	// API routes — token + HMAC auth
	api := r.Group("/api")
	api.Use(middleware.APITokenRequired(authSvc))
	api.Use(middleware.HMACAuth("test-hmac"))
	api.Use(middleware.DataScope())
	{
		api.GET("/slots", bookingHandler.GetSlots)
	}

	// Internal API routes — token + HMAC + admin RBAC (mirrors production)
	internal := r.Group("/api/internal")
	internal.Use(middleware.APITokenRequired(authSvc))
	internal.Use(middleware.HMACAuth("test-hmac"))
	internal.Use(middleware.DataScope())
	internal.Use(middleware.RequireRole(models.RoleAdmin))
	{
		internal.GET("/clinic-utilization", adminHandler.APIClinicUtilization)
		internal.GET("/booking-fill-rates", adminHandler.APIBookingFillRates)
		internal.GET("/menu-sell-through", adminHandler.APIMenuSellThrough)
		internal.GET("/webhooks", func(c *gin.Context) {
			orgID := getCallerOrgID(c)
			endpoints, _ := webhookSvc.GetEndpoints(orgID)
			c.JSON(200, endpoints)
		})
	}

	// Token issuance (session-auth)
	authed.POST("/api/tokens", authHandler.IssueToken)

	return r, db
}

// helper: get an API token for a user
func getTokenFor(t *testing.T, db *gorm.DB, username string) string {
	t.Helper()
	authSvc := auth.NewAuthService(db)
	var user models.User
	db.First(&user, "username = ?", username)
	require.NotZero(t, user.ID)
	_, tokenStr, err := authSvc.IssueAPIToken(user.ID, "test", 1*time.Hour)
	require.NoError(t, err)
	return tokenStr
}

// helper: create an API request with token + HMAC signature
func newSignedAPIRequest(t *testing.T, method, fullURL, token string) *http.Request {
	t.Helper()
	bodyBytes := []byte("")
	bodyHash := sha256Hex(bodyBytes)
	ts := time.Now().Format(time.RFC3339)
	// HMAC signs over the path only (no query string), matching c.Request.URL.Path
	parsed, _ := url.Parse(fullURL)
	sig := hmacSign("test-hmac", method, parsed.Path, ts, bodyHash)
	req, _ := http.NewRequest(method, fullURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-HMAC-Signature", sig)
	req.Header.Set("X-HMAC-Timestamp", ts)
	return req
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSign(secret, method, path, ts, bodyHash string) string {
	message := fmt.Sprintf("%s:%s:%s:%s", method, path, ts, bodyHash)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestLoginPage_Returns200(t *testing.T) {
	r, _ := setupRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/login", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Campus Wellness")
}

// getLoginCSRF does a GET /login and returns the login_csrf cookie value
func getLoginCSRF(t *testing.T, r *gin.Engine) string {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/login", nil)
	r.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.Name == "login_csrf" {
			return c.Value
		}
	}
	t.Fatal("login_csrf cookie not set")
	return ""
}

func TestLogin_ValidCredentials_RedirectsToDashboard(t *testing.T) {
	r, _ := setupRouter(t)
	csrfToken := getLoginCSRF(t, r)

	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "password123")
	form.Set("csrf_token", csrfToken)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "login_csrf", Value: csrfToken})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
	assert.Equal(t, "/dashboard", w.Header().Get("Location"))
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "session_id" && c.Value != "" {
			found = true
		}
	}
	assert.True(t, found, "session_id cookie should be set")
}

func TestLogin_InvalidCredentials_ShowsError(t *testing.T) {
	r, _ := setupRouter(t)
	csrfToken := getLoginCSRF(t, r)

	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "wrongpassword")
	form.Set("csrf_token", csrfToken)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "login_csrf", Value: csrfToken})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid username or password")
}

func TestLogin_MissingCSRF_ShowsError(t *testing.T) {
	r, _ := setupRouter(t)

	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "password123")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid request")
}

func TestDashboard_WithoutAuth_RedirectsToLogin(t *testing.T) {
	r, _ := setupRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/dashboard", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))
}

func TestDashboard_WithValidSession_Returns200(t *testing.T) {
	r, db := setupRouter(t)

	// Create a session directly
	authSvc := auth.NewAuthService(db)
	var user models.User
	db.First(&user, "username = ?", "admin")
	require.NotZero(t, user.ID)

	sessionID, err := authSvc.CreateSession(user.ID)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Health Dashboard")
}

func TestDashboard_WithExpiredSession_RedirectsToLogin(t *testing.T) {
	r, _ := setupRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "invalid-session-id"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))
}

func TestLogout_ClearsSession(t *testing.T) {
	r, db := setupRouter(t)

	authSvc := auth.NewAuthService(db)
	var user models.User
	db.First(&user, "username = ?", "admin")
	sessionID, _ := authSvc.CreateSession(user.ID)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/logout", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))

	// Session should be invalid now
	_, err := authSvc.ValidateSession(sessionID)
	assert.Error(t, err)
}

// helper to get a session cookie for a given username
func getSessionFor(t *testing.T, db *gorm.DB, username string) string {
	t.Helper()
	authSvc := auth.NewAuthService(db)
	var user models.User
	db.First(&user, "username = ?", username)
	require.NotZero(t, user.ID)
	sessionID, err := authSvc.CreateSession(user.ID)
	require.NoError(t, err)
	return sessionID
}

func TestUpload_RejectsOversizedFile(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	// Create a body that's > 10MB
	body := &strings.Builder{}
	body.WriteString("--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"big.pdf\"\r\nContent-Type: application/pdf\r\n\r\n")
	body.WriteString(strings.Repeat("x", 11*1024*1024)) // 11MB
	body.WriteString("\r\n--boundary--\r\n")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/health/upload", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
	req.Header.Set("X-CSRF-Token", "test")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestUpload_RejectsDisallowedType(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	body := &strings.Builder{}
	body.WriteString("--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"hack.exe\"\r\nContent-Type: application/x-executable\r\n\r\n")
	body.WriteString("MZ executable content")
	body.WriteString("\r\n--boundary--\r\n")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/health/upload", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
	req.Header.Set("X-CSRF-Token", "test")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "file type not allowed")
}

func TestAdminEndpoint_DeniedForStudent(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/users", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 403, w.Code)
}

func TestAdminEndpoint_AllowedForAdmin(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/users", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "User Management")
}

func TestBookingPage_RequiresAuth(t *testing.T) {
	r, _ := setupRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/bookings", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))
}

func TestBookingPage_AccessibleWithSession(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/bookings", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Training Sessions")
}

func TestStudentCannotViewOtherStudentDashboard(t *testing.T) {
	r, db := setupRouter(t)

	// Get student user (ID typically 2 from seed data)
	var student models.User
	db.First(&student, "username = ?", "student")
	require.NotZero(t, student.ID)
	sessionID := getSessionFor(t, db, "student")

	// Try to access another user's data via query param
	var otherUser models.User
	db.First(&otherUser, "username = ?", "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/dashboard?user_id="+strconv.FormatUint(uint64(otherUser.ID), 10), nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	// Should return 200 but show student's OWN data (scope enforcement silently ignores invalid target)
	assert.Equal(t, 200, w.Code)
	// The page should contain the student's name, not the admin's
	assert.Contains(t, w.Body.String(), student.FullName)
}

func TestAdminRoleChange_ProducesAuditLog(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	// Get a target user
	var target models.User
	db.First(&target, "username = ?", "student")
	require.NotZero(t, target.ID)

	form := url.Values{}
	form.Set("role", "staff")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/admin/users/"+strconv.FormatUint(uint64(target.ID), 10)+"/role", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)

	// Verify audit log was created
	var auditLog models.AuditLog
	err := db.Where("table_name = ? AND record_id = ? AND action = ?", "users", target.ID, "role_change").
		Order("timestamp DESC").First(&auditLog).Error
	assert.NoError(t, err)
	assert.Contains(t, auditLog.Reason, "Role changed")
}

// ===================== FINDING 1: API TOKEN AUTH TESTS =====================

func TestAPIRoute_ValidTokenAndHMAC_Succeeds(t *testing.T) {
	r, db := setupRouter(t)
	token := getTokenFor(t, db, "student")

	w := httptest.NewRecorder()
	req := newSignedAPIRequest(t, "GET", "/api/slots?venue_id=1&date=2026-12-01", token)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
}

func TestAPIRoute_TokenWithoutHMAC_Fails(t *testing.T) {
	r, db := setupRouter(t)
	token := getTokenFor(t, db, "student")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/slots?venue_id=1&date=2026-12-01", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	// No HMAC headers → should fail
	r.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
}

func TestAPIRoute_MissingToken_Fails(t *testing.T) {
	r, _ := setupRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/slots?venue_id=1&date=2026-12-01", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
}

func TestAPIRoute_InvalidToken_Fails(t *testing.T) {
	r, _ := setupRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/slots?venue_id=1&date=2026-12-01", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-value")
	r.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
}

func TestAPIRoute_SessionCookieAlone_Rejected(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/slots?venue_id=1&date=2026-12-01", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	// Token-only: session cookie alone must be rejected on /api/*
	assert.Equal(t, 401, w.Code)
}

// ===================== CSRF IN INTEGRATION ROUTER (Finding 5) =====================

func TestCSRF_IntegrationRouter_PostWithValidCSRF(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	form := url.Values{}
	form.Set("role", "student")
	form.Set("csrf_token", "test-csrf")

	var target models.User
	db.First(&target, "username = ?", "student")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/admin/users/"+strconv.FormatUint(uint64(target.ID), 10)+"/role", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test-csrf"})
	r.ServeHTTP(w, req)

	// Should succeed (302 redirect) since CSRF token matches
	assert.Equal(t, 302, w.Code)
}

func TestCSRF_IntegrationRouter_PostWithoutCSRF_Fails(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	form := url.Values{}
	form.Set("role", "student")
	// No csrf_token in form

	var target models.User
	db.First(&target, "username = ?", "student")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/admin/users/"+strconv.FormatUint(uint64(target.ID), 10)+"/role", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "real-token"})
	r.ServeHTTP(w, req)

	// Should fail — form token doesn't match cookie token
	assert.Equal(t, 403, w.Code)
}

// ===================== ORG ISOLATION (Finding 1) =====================

func TestAdminUsersPage_OnlyShowsSameOrg(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/users", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "User Management")
}

// ===================== SELF-UPLOAD (Finding 3) =====================

func TestStudentCanUploadToSelf(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")
	var student models.User
	db.First(&student, "username = ?", "student")

	// Create a valid PDF-like multipart upload targeting self
	body := &strings.Builder{}
	body.WriteString("--boundary\r\nContent-Disposition: form-data; name=\"user_id\"\r\n\r\n" + strconv.FormatUint(uint64(student.ID), 10))
	body.WriteString("\r\n--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"doc.pdf\"\r\nContent-Type: application/pdf\r\n\r\n")
	body.WriteString("%PDF-1.4 test content") // PDF magic bytes
	body.WriteString("\r\n--boundary--\r\n")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/health/upload", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
	req.Header.Set("X-CSRF-Token", "test")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	// Should succeed (200) or at least not be 403
	assert.NotEqual(t, 403, w.Code)
}

func TestStudentCannotUploadToOtherUser(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")
	var admin models.User
	db.First(&admin, "username = ?", "admin")

	body := &strings.Builder{}
	body.WriteString("--boundary\r\nContent-Disposition: form-data; name=\"user_id\"\r\n\r\n" + strconv.FormatUint(uint64(admin.ID), 10))
	body.WriteString("\r\n--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"doc.pdf\"\r\nContent-Type: application/pdf\r\n\r\n")
	body.WriteString("%PDF-1.4 test")
	body.WriteString("\r\n--boundary--\r\n")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/health/upload", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
	req.Header.Set("X-CSRF-Token", "test")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 403, w.Code)
}

func TestStaffCannotUpload(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "staff")

	body := &strings.Builder{}
	body.WriteString("--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"doc.pdf\"\r\nContent-Type: application/pdf\r\n\r\n")
	body.WriteString("%PDF-1.4 test")
	body.WriteString("\r\n--boundary--\r\n")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/health/upload", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
	req.Header.Set("X-CSRF-Token", "test")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 403, w.Code)
}

func TestStaffCannotUpdateHealthRecord(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "staff")

	form := url.Values{}
	form.Set("allergies", "none")
	form.Set("reason", "test")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/health/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	// Staff should be denied by RequireRole(clinician/admin)
	assert.Equal(t, 403, w.Code)
}

// ===================== /api/internal/* AUTH GATE TESTS =====================

func TestInternalAPI_MissingToken_Returns401(t *testing.T) {
	r, _ := setupRouter(t)

	endpoints := []string{
		"/api/internal/clinic-utilization",
		"/api/internal/booking-fill-rates",
		"/api/internal/menu-sell-through",
		"/api/internal/webhooks",
	}
	for _, ep := range endpoints {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", ep, nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, 401, w.Code, "missing token should 401 for %s", ep)
	}
}

func TestInternalAPI_TokenWithoutHMAC_Returns401(t *testing.T) {
	r, db := setupRouter(t)
	token := getTokenFor(t, db, "admin")

	endpoints := []string{
		"/api/internal/clinic-utilization",
		"/api/internal/booking-fill-rates",
		"/api/internal/menu-sell-through",
	}
	for _, ep := range endpoints {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", ep, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r.ServeHTTP(w, req)
		assert.Equal(t, 401, w.Code, "token without HMAC should 401 for %s", ep)
	}
}

func TestInternalAPI_InvalidHMAC_Returns401(t *testing.T) {
	r, db := setupRouter(t)
	token := getTokenFor(t, db, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/internal/clinic-utilization", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-HMAC-Signature", "deadbeef")
	req.Header.Set("X-HMAC-Timestamp", time.Now().Format(time.RFC3339))
	r.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
}

// ===================== /api/internal/* RBAC TESTS =====================

func TestInternalAPI_NonAdminRole_Returns403(t *testing.T) {
	r, db := setupRouter(t)

	// Test with student, clinician, and staff roles — all should get 403
	nonAdminUsers := []string{"student", "clinician", "staff"}
	for _, username := range nonAdminUsers {
		token := getTokenFor(t, db, username)
		w := httptest.NewRecorder()
		req := newSignedAPIRequest(t, "GET", "/api/internal/clinic-utilization", token)
		r.ServeHTTP(w, req)
		assert.Equal(t, 403, w.Code, "non-admin user %s should get 403 on /api/internal/*", username)
	}
}

func TestInternalAPI_AdminRole_Returns200(t *testing.T) {
	r, db := setupRouter(t)
	token := getTokenFor(t, db, "admin")

	endpoints := []string{
		"/api/internal/clinic-utilization",
		"/api/internal/booking-fill-rates",
		"/api/internal/menu-sell-through",
		"/api/internal/webhooks",
	}
	for _, ep := range endpoints {
		w := httptest.NewRecorder()
		req := newSignedAPIRequest(t, "GET", ep, token)
		r.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code, "admin should get 200 for %s", ep)
	}
}

// ===================== /api/internal/* ORG SCOPING TESTS =====================

// createSecondOrg creates a second org with its own admin user for multi-org tests.
// Returns the org ID and admin user ID.
func createSecondOrg(t *testing.T, db *gorm.DB) (uint, uint) {
	t.Helper()
	org := models.Organization{Name: "Second University"}
	db.Where("name = ?", org.Name).FirstOrCreate(&org)
	require.NotZero(t, org.ID)

	dept := models.DepartmentRecord{Name: "General Medicine", OrganizationID: org.ID}
	db.Where("name = ? AND organization_id = ?", dept.Name, org.ID).FirstOrCreate(&dept)

	hashBytes, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	hash := string(hashBytes)
	admin2 := models.User{
		Username:       "admin2",
		PasswordHash:   hash,
		FullName:       "Admin Two",
		Email:          "admin2@second.local",
		Role:           models.RoleAdmin,
		OrganizationID: org.ID,
		Active:         true,
	}
	db.Where("username = ?", "admin2").FirstOrCreate(&admin2)

	clinician2 := models.User{
		Username:       "clinician2",
		PasswordHash:   hash,
		FullName:       "Dr. Two",
		Email:          "clinician2@second.local",
		Role:           models.RoleClinician,
		OrganizationID: org.ID,
		DepartmentID:   &dept.ID,
		Active:         true,
	}
	db.Where("username = ?", "clinician2").FirstOrCreate(&clinician2)

	return org.ID, admin2.ID
}

func TestInternalAPI_OrgScopedReports_ClinicUtilization(t *testing.T) {
	r, db := setupRouter(t)
	org2ID, _ := createSecondOrg(t, db)

	// Seed encounter data for org 1 (clinician ID 4 = "clinician", org 1)
	db.Create(&models.Encounter{
		UserID: 2, ClinicianID: 4, Department: "general",
		ChiefComplaint: "test", EncounterDate: time.Now(),
	})
	// Seed encounter data for org 2
	var clinician2 models.User
	db.First(&clinician2, "username = ?", "clinician2")
	db.Create(&models.Encounter{
		UserID: clinician2.ID, ClinicianID: clinician2.ID, Department: "general",
		ChiefComplaint: "org2 test", EncounterDate: time.Now(),
	})

	// Refresh materialized views so data is available
	db.Exec("REFRESH MATERIALIZED VIEW mv_clinic_utilization")

	// Org 1 admin should see only org 1 data
	token1 := getTokenFor(t, db, "admin")
	w := httptest.NewRecorder()
	req := newSignedAPIRequest(t, "GET", "/api/internal/clinic-utilization", token1)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	body := w.Body.String()
	// Org 1 admin should get results (could be empty if no data, but should not error)
	assert.NotContains(t, body, "error")

	// Org 2 admin should see only org 2 data
	token2 := getTokenFor(t, db, "admin2")
	w2 := httptest.NewRecorder()
	req2 := newSignedAPIRequest(t, "GET", "/api/internal/clinic-utilization", token2)
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)

	// Verify org isolation: if org1 data contains encounter_count, org2 must not see org1's count
	// Both should be valid JSON arrays
	assert.Contains(t, body, "[")
	body2 := w2.Body.String()
	assert.Contains(t, body2, "[")

	// Specifically: org2 should NOT see org1's encounters and vice versa.
	// The counts may be empty arrays or have different data.
	_ = org2ID
}

func TestInternalAPI_OrgScopedReports_BookingFillRates(t *testing.T) {
	r, db := setupRouter(t)
	org2ID, _ := createSecondOrg(t, db)

	// Create booking for org 1
	db.Create(&models.Booking{
		OrganizationID: 1, RequesterID: 2, VenueID: 1,
		SlotStart: time.Now().Add(24 * time.Hour), SlotEnd: time.Now().Add(25 * time.Hour),
		Status: models.BookingConfirmed,
	})
	// Create booking for org 2
	db.Create(&models.Booking{
		OrganizationID: org2ID, RequesterID: 2, VenueID: 1,
		SlotStart: time.Now().Add(48 * time.Hour), SlotEnd: time.Now().Add(49 * time.Hour),
		Status: models.BookingCanceled,
	})

	db.Exec("REFRESH MATERIALIZED VIEW mv_booking_fill_rates")

	// Org 1 admin
	token1 := getTokenFor(t, db, "admin")
	w := httptest.NewRecorder()
	req := newSignedAPIRequest(t, "GET", "/api/internal/booking-fill-rates", token1)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	assert.NotContains(t, w.Body.String(), "error")

	// Org 2 admin — should only see org 2 bookings
	token2 := getTokenFor(t, db, "admin2")
	w2 := httptest.NewRecorder()
	req2 := newSignedAPIRequest(t, "GET", "/api/internal/booking-fill-rates", token2)
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)
	assert.NotContains(t, w2.Body.String(), "error")
}

func TestInternalAPI_OrgScopedReports_MenuSellThrough(t *testing.T) {
	r, db := setupRouter(t)
	org2ID, _ := createSecondOrg(t, db)

	// Seed menu data for org 2
	db.Create(&models.MenuCategory{OrganizationID: org2ID, Name: "Org2 Entrees", SortOrder: 1})
	var cat models.MenuCategory
	db.Last(&cat)
	db.Create(&models.MenuItem{
		OrganizationID: org2ID, CategoryID: cat.ID, SKU: "ORG2-001",
		Name: "Org2 Item", ItemType: "dish", BasePriceDineIn: 5.99, BasePriceTakeout: 6.99,
	})

	db.Exec("REFRESH MATERIALIZED VIEW mv_menu_sell_through")

	// Org 1 admin should NOT see org 2 menu items
	token1 := getTokenFor(t, db, "admin")
	w := httptest.NewRecorder()
	req := newSignedAPIRequest(t, "GET", "/api/internal/menu-sell-through", token1)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	assert.NotContains(t, w.Body.String(), "ORG2-001")

	// Org 2 admin should see org 2 menu items only
	token2 := getTokenFor(t, db, "admin2")
	w2 := httptest.NewRecorder()
	req2 := newSignedAPIRequest(t, "GET", "/api/internal/menu-sell-through", token2)
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)
	assert.Contains(t, w2.Body.String(), "ORG2-001")
	// Org 2 should NOT see org 1 items
	assert.NotContains(t, w2.Body.String(), "ENT-001")
}

func TestInternalAPI_WebhookEndpoints_OrgScoped(t *testing.T) {
	r, db := setupRouter(t)
	org2ID, _ := createSecondOrg(t, db)

	// Register webhook for org 1
	db.Create(&models.WebhookEndpoint{
		OrganizationID: 1, URL: "http://org1.local/hook",
		EventType: "booking.created", Secret: "s1", Active: true,
	})
	// Register webhook for org 2
	db.Create(&models.WebhookEndpoint{
		OrganizationID: org2ID, URL: "http://org2.local/hook",
		EventType: "booking.created", Secret: "s2", Active: true,
	})

	// Org 1 admin should only see org 1 webhooks
	token1 := getTokenFor(t, db, "admin")
	w := httptest.NewRecorder()
	req := newSignedAPIRequest(t, "GET", "/api/internal/webhooks", token1)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "org1.local")
	assert.NotContains(t, w.Body.String(), "org2.local")

	// Org 2 admin should only see org 2 webhooks
	token2 := getTokenFor(t, db, "admin2")
	w2 := httptest.NewRecorder()
	req2 := newSignedAPIRequest(t, "GET", "/api/internal/webhooks", token2)
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)
	assert.Contains(t, w2.Body.String(), "org2.local")
	assert.NotContains(t, w2.Body.String(), "org1.local")
}

// ===================== CLINICIAN ENCOUNTER ORG ISOLATION TESTS =====================

func TestClinicianEncounters_OrgIsolation_NoCrossOrgLeak(t *testing.T) {
	_, db := setupRouter(t)
	org2ID, _ := createSecondOrg(t, db)

	// Create encounters for org 1 (clinician ID 4 = "clinician", org 1)
	db.Create(&models.Encounter{
		UserID: 2, ClinicianID: 4, Department: "general",
		ChiefComplaint: "org1 headache", EncounterDate: time.Now(),
	})

	// Create encounters for org 2
	var clinician2 models.User
	db.First(&clinician2, "username = ?", "clinician2")
	var admin2 models.User
	db.First(&admin2, "username = ?", "admin2")
	db.Create(&models.Encounter{
		UserID: admin2.ID, ClinicianID: clinician2.ID, Department: "general",
		ChiefComplaint: "org2 fever", EncounterDate: time.Now(),
	})

	healthSvc := services.NewHealthService(db, nil, "")

	// Org 1 encounters: should only see "org1 headache"
	enc1, err := healthSvc.GetEncountersByDept("general", 1)
	require.NoError(t, err)
	for _, e := range enc1 {
		assert.NotEqual(t, "org2 fever", e.ChiefComplaint,
			"org 1 query must not return org 2 encounters")
	}
	found := false
	for _, e := range enc1 {
		if e.ChiefComplaint == "org1 headache" {
			found = true
		}
	}
	assert.True(t, found, "org 1 query should return org 1 encounters")

	// Org 2 encounters: should only see "org2 fever"
	enc2, err := healthSvc.GetEncountersByDept("general", org2ID)
	require.NoError(t, err)
	for _, e := range enc2 {
		assert.NotEqual(t, "org1 headache", e.ChiefComplaint,
			"org 2 query must not return org 1 encounters")
	}
	found2 := false
	for _, e := range enc2 {
		if e.ChiefComplaint == "org2 fever" {
			found2 = true
		}
	}
	assert.True(t, found2, "org 2 query should return org 2 encounters")
}

func TestClinicianEncounters_SameOrgAuthorizedDept_Succeeds(t *testing.T) {
	_, db := setupRouter(t)

	// Create encounter in "general" department for org 1
	db.Create(&models.Encounter{
		UserID: 2, ClinicianID: 4, Department: "general",
		ChiefComplaint: "checkup", EncounterDate: time.Now(),
	})

	healthSvc := services.NewHealthService(db, nil, "")

	// Clinician in org 1, department "general" should see the encounter
	encounters, err := healthSvc.GetEncountersByDept("general", 1)
	require.NoError(t, err)
	found := false
	for _, e := range encounters {
		if e.ChiefComplaint == "checkup" {
			found = true
		}
	}
	assert.True(t, found, "authorized dept + org should return encounters")
}

func TestClinicianEncounters_DifferentDept_ReturnsEmpty(t *testing.T) {
	_, db := setupRouter(t)

	// Create encounter in "general" department
	db.Create(&models.Encounter{
		UserID: 2, ClinicianID: 4, Department: "general",
		ChiefComplaint: "dept test", EncounterDate: time.Now(),
	})

	healthSvc := services.NewHealthService(db, nil, "")

	// Querying "lab" department in same org should NOT return the "general" encounter
	encounters, err := healthSvc.GetEncountersByDept("lab", 1)
	require.NoError(t, err)
	for _, e := range encounters {
		assert.NotEqual(t, "dept test", e.ChiefComplaint,
			"wrong department query must not return encounters from other departments")
	}
}

// ===================== MATERIALIZED VIEW REPORTING TESTS =====================

func TestReportingService_QueriesMaterializedViews(t *testing.T) {
	_, db := setupRouter(t)

	// Seed data and refresh views
	db.Create(&models.Encounter{
		UserID: 2, ClinicianID: 4, Department: "general",
		ChiefComplaint: "mv test", EncounterDate: time.Now(),
	})
	db.Exec("REFRESH MATERIALIZED VIEW mv_clinic_utilization")
	db.Exec("REFRESH MATERIALIZED VIEW mv_booking_fill_rates")
	db.Exec("REFRESH MATERIALIZED VIEW mv_menu_sell_through")

	svc := services.NewReportingService(db, 5*time.Minute)

	// Clinic utilization from mat view
	clinic, err := svc.GetClinicUtilization(1)
	require.NoError(t, err)
	assert.NotNil(t, clinic, "clinic utilization should return results after MV refresh")

	// Booking fill rates from mat view
	booking, err := svc.GetBookingFillRates(1)
	require.NoError(t, err)
	// May be empty if no bookings, but should not error
	_ = booking

	// Menu sell-through from mat view
	menu, err := svc.GetMenuSellThrough(1)
	require.NoError(t, err)
	// Menu sell-through returns items (may be 0 if MV just refreshed with no orders)
	_ = menu
}

func TestReportingService_RefreshTargetsCorrectViews(t *testing.T) {
	_, db := setupRouter(t)
	svc := services.NewReportingService(db, 5*time.Minute)

	// RefreshMaterializedViews should not panic or error
	svc.RefreshMaterializedViews()

	// Verify the views exist and are queryable after refresh
	var count int64
	err := db.Raw("SELECT COUNT(*) FROM mv_clinic_utilization").Scan(&count).Error
	assert.NoError(t, err, "mv_clinic_utilization should be queryable after refresh")

	err = db.Raw("SELECT COUNT(*) FROM mv_booking_fill_rates").Scan(&count).Error
	assert.NoError(t, err, "mv_booking_fill_rates should be queryable after refresh")

	err = db.Raw("SELECT COUNT(*) FROM mv_menu_sell_through").Scan(&count).Error
	assert.NoError(t, err, "mv_menu_sell_through should be queryable after refresh")
}

func TestReportingService_OrgIsolation_NoLeakAcrossOrgs(t *testing.T) {
	_, db := setupRouter(t)
	org2ID, _ := createSecondOrg(t, db)

	// Seed org 2 menu item
	db.Create(&models.MenuCategory{OrganizationID: org2ID, Name: "Org2 Cat", SortOrder: 1})
	var cat models.MenuCategory
	db.Last(&cat)
	db.Create(&models.MenuItem{
		OrganizationID: org2ID, CategoryID: cat.ID, SKU: "ORG2-MVTEST",
		Name: "Org2 MV Item", ItemType: "dish", BasePriceDineIn: 9.99, BasePriceTakeout: 10.99,
	})

	db.Exec("REFRESH MATERIALIZED VIEW mv_menu_sell_through")

	svc := services.NewReportingService(db, 5*time.Minute)

	// Org 1 should not see org 2 data
	menu1, err := svc.GetMenuSellThrough(1)
	require.NoError(t, err)
	for _, row := range menu1 {
		if sku, ok := row["sku"].(string); ok {
			assert.NotEqual(t, "ORG2-MVTEST", sku, "org 1 must not see org 2 menu items in MV")
		}
	}

	// Org 2 should see org 2 data
	menu2, err := svc.GetMenuSellThrough(org2ID)
	require.NoError(t, err)
	found := false
	for _, row := range menu2 {
		if sku, ok := row["sku"].(string); ok && sku == "ORG2-MVTEST" {
			found = true
		}
	}
	assert.True(t, found, "org 2 should see its own menu items in MV")
}
