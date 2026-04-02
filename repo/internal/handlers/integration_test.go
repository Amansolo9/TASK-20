package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

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
	}

	authSvc := auth.NewAuthService(db)
	auditSvc := services.NewAuditService(db)
	healthSvc := services.NewHealthService(db, auditSvc, cfg.UploadDir)

	bookingSvc := services.NewBookingService(db, auditSvc, nil)

	authHandler := NewAuthHandler(authSvc, auditSvc, db, cfg)
	healthHandler := NewHealthHandler(healthSvc, auditSvc)
	bookingHandler := NewBookingHandler(bookingSvc, db)

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

	// Authenticated routes
	authed := r.Group("/")
	authed.Use(middleware.AuthRequired(authSvc))
	authed.Use(middleware.DataScope())
	{
		authed.GET("/dashboard", healthHandler.DashboardPage)
		authed.GET("/logout", authHandler.Logout)
		authed.POST("/health/update", healthHandler.UpdateHealthRecord)
		authed.POST("/health/upload", healthHandler.UploadAttachment)
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

	return r, db
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
