package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"campus-portal/internal/auth"
	"campus-portal/internal/config"
	"campus-portal/internal/models"
	"campus-portal/internal/services"
	"campus-portal/internal/views"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AuthHandler struct {
	AuthSvc  *auth.AuthService
	AuditSvc *services.AuditService
	DB       *gorm.DB
	Cfg      *config.Config
}

func NewAuthHandler(authSvc *auth.AuthService, auditSvc *services.AuditService, db *gorm.DB, cfg *config.Config) *AuthHandler {
	return &AuthHandler{AuthSvc: authSvc, AuditSvc: auditSvc, DB: db, Cfg: cfg}
}

func (h *AuthHandler) LoginPage(c *gin.Context) {
	// Generate a CSRF token for the login form
	token := ensureLoginCSRF(c)
	views.Render(c, http.StatusOK, views.LoginPage("", token))
}

func (h *AuthHandler) Login(c *gin.Context) {
	// Validate CSRF token on login POST
	cookieToken, _ := c.Cookie("login_csrf")
	formToken := c.PostForm("csrf_token")
	if cookieToken == "" || formToken == "" || cookieToken != formToken {
		views.Render(c, http.StatusOK, views.LoginPage("Invalid request. Please try again.", ensureLoginCSRF(c)))
		return
	}

	username := c.PostForm("username")
	password := c.PostForm("password")

	user, sessionID, err := h.AuthSvc.Login(username, password)
	if err != nil {
		views.Render(c, http.StatusOK, views.LoginPage("Invalid username or password", ensureLoginCSRF(c)))
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("session_id", sessionID, 86400, "/", "", h.Cfg.SecureCookies, true)
	_ = user
	c.Redirect(http.StatusFound, "/dashboard")
}

func (h *AuthHandler) Logout(c *gin.Context) {
	sessionID, _ := c.Cookie("session_id")
	if sessionID != "" {
		h.AuthSvc.Logout(sessionID)
	}
	c.SetCookie("session_id", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/login")
}

// Admin user management
func (h *AuthHandler) UsersPage(c *gin.Context) {
	var users []models.User
	h.DB.Find(&users)
	c.HTML(http.StatusOK, "admin_users.html", gin.H{
		"title": "User Management",
		"users": users,
		"user":  GetCurrentUser(c),
	})
}

func (h *AuthHandler) ToggleUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}
	active := c.PostForm("active") == "true"
	currentUser := GetCurrentUser(c)

	h.AuthSvc.ToggleUserActive(uint(id), active)
	h.AuthSvc.InvalidateUserSessions(uint(id)) // Force re-auth immediately

	action := "deactivated"
	if active {
		action = "activated"
	}
	h.AuditSvc.LogChange("users", uint(id), action, currentUser.ID, fmt.Sprintf("Account %s by admin", action), gin.H{"active": active})
	c.Redirect(http.StatusFound, "/admin/users")
}

func (h *AuthHandler) ChangeRole(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}
	role := models.Role(c.PostForm("role"))
	if !isValidRole(role) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
		return
	}
	currentUser := GetCurrentUser(c)

	// Get old role for audit
	var targetUser models.User
	h.DB.First(&targetUser, uint(id))

	h.AuthSvc.ChangeUserRole(uint(id), role)
	h.AuthSvc.InvalidateUserSessions(uint(id)) // Force re-auth with new role
	h.AuditSvc.LogChange("users", uint(id), "role_change", currentUser.ID,
		fmt.Sprintf("Role changed from %s to %s", targetUser.Role, role),
		gin.H{"old_role": targetUser.Role, "new_role": role})
	c.Redirect(http.StatusFound, "/admin/users")
}

func (h *AuthHandler) GrantTempAccess(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}
	role := models.Role(c.PostForm("role"))
	if !isValidRole(role) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
		return
	}
	currentUser := GetCurrentUser(c)

	durationHours, _ := strconv.Atoi(c.PostForm("duration_hours"))
	if durationHours <= 0 {
		durationHours = 8
	}

	h.AuthSvc.GrantTempAccess(uint(id), role, currentUser.ID, time.Duration(durationHours)*time.Hour)
	h.AuditSvc.LogChange("users", uint(id), "temp_access_granted", currentUser.ID,
		fmt.Sprintf("Temporary %s access granted for %d hours", role, durationHours),
		gin.H{"granted_role": role, "duration_hours": durationHours})
	c.Redirect(http.StatusFound, "/admin/users")
}

func (h *AuthHandler) RegisterPage(c *gin.Context) {
	c.HTML(http.StatusOK, "register.html", gin.H{
		"title": "Register New User",
		"user":  GetCurrentUser(c),
	})
}

func (h *AuthHandler) Register(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")
	passwordConfirm := c.PostForm("password_confirm")
	fullName := c.PostForm("full_name")
	email := c.PostForm("email")
	role := models.Role(c.PostForm("role"))

	// Password validation
	if len(password) < 8 {
		c.HTML(http.StatusOK, "register.html", gin.H{
			"title": "Register New User", "error": "Password must be at least 8 characters", "user": GetCurrentUser(c),
		})
		return
	}
	if password != passwordConfirm {
		c.HTML(http.StatusOK, "register.html", gin.H{
			"title": "Register New User", "error": "Passwords do not match", "user": GetCurrentUser(c),
		})
		return
	}

	var deptID *uint
	if d, err := strconv.ParseUint(c.PostForm("department_id"), 10, 64); err == nil {
		did := uint(d)
		deptID = &did
	}

	newUser, err := h.AuthSvc.Register(username, password, fullName, email, role, 1, deptID)
	if err != nil {
		c.HTML(http.StatusOK, "register.html", gin.H{
			"title": "Register New User",
			"error": err.Error(),
			"user":  GetCurrentUser(c),
		})
		return
	}

	currentUser := GetCurrentUser(c)
	h.AuditSvc.LogChange("users", newUser.ID, "user_created", currentUser.ID,
		fmt.Sprintf("User %s created with role %s", username, role), newUser)
	c.Redirect(http.StatusFound, "/admin/users")
}

func ensureLoginCSRF(c *gin.Context) string {
	if token, _ := c.Cookie("login_csrf"); token != "" {
		return token
	}
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("login_csrf", token, 600, "/", "", false, true)
	return token
}

func isValidRole(role models.Role) bool {
	switch role {
	case models.RoleStudent, models.RoleFaculty, models.RoleClinician, models.RoleStaff, models.RoleAdmin:
		return true
	}
	return false
}

func GetCurrentUser(c *gin.Context) *models.User {
	u, exists := c.Get("user")
	if !exists {
		return nil
	}
	return u.(*models.User)
}
