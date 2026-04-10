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

// IssueToken creates a locally-issued API token for the authenticated user.
func (h *AuthHandler) IssueToken(c *gin.Context) {
	user := GetCurrentUser(c)
	description := c.DefaultPostForm("description", "API token")

	tok, tokenStr, err := h.AuthSvc.IssueAPIToken(user.ID, description, 24*time.Hour)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"token":       tokenStr,
		"hmac_secret": h.Cfg.HMACSecret,
		"expires_at":  tok.ExpiresAt.Format("01/02/2006 03:04 PM"),
		"message":     "Store this token securely — it cannot be retrieved again.",
	})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	sessionID, _ := c.Cookie("session_id")
	if sessionID != "" {
		h.AuthSvc.Logout(sessionID)
	}
	c.SetCookie("session_id", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/login")
}

// getOrgID returns the authenticated user's organization ID from context.
func getOrgID(c *gin.Context) uint {
	orgID, _ := c.Get("orgID")
	return orgID.(uint)
}

// verifyTargetSameOrg checks that a target user belongs to the admin's org. Returns false and sends 403 if not.
func (h *AuthHandler) verifyTargetSameOrg(c *gin.Context, targetUserID uint) bool {
	var target models.User
	if err := h.DB.First(&target, targetUserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return false
	}
	if target.OrganizationID != getOrgID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot administer users outside your organization"})
		return false
	}
	return true
}

// Admin user management — all queries scoped to admin's org
func (h *AuthHandler) UsersPage(c *gin.Context) {
	orgID := getOrgID(c)
	var users []models.User
	h.DB.Where("organization_id = ?", orgID).Find(&users)
	currentUser := GetCurrentUser(c)

	ud := views.AdminUsersData{
		User: &views.UserInfo{FullName: currentUser.FullName, Role: string(currentUser.Role)},
	}
	for _, u := range users {
		ud.Users = append(ud.Users, views.AdminUserRow{
			ID:       u.ID,
			Username: u.Username,
			FullName: u.FullName,
			Email:    u.Email,
			Role:     string(u.Role),
			Active:   u.Active,
		})
	}
	views.Render(c, http.StatusOK, views.AdminUsersPage(ud))
}

func (h *AuthHandler) ToggleUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}
	if !h.verifyTargetSameOrg(c, uint(id)) {
		return
	}
	active := c.PostForm("active") == "true"
	currentUser := GetCurrentUser(c)

	h.AuthSvc.ToggleUserActive(uint(id), active)
	h.AuthSvc.InvalidateUserSessions(uint(id))

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
	if !h.verifyTargetSameOrg(c, uint(id)) {
		return
	}
	role := models.Role(c.PostForm("role"))
	if !isValidRole(role) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
		return
	}
	currentUser := GetCurrentUser(c)

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
	if !h.verifyTargetSameOrg(c, uint(id)) {
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
	currentUser := GetCurrentUser(c)
	views.Render(c, http.StatusOK, views.RegisterPage(views.RegisterData{
		User: &views.UserInfo{FullName: currentUser.FullName, Role: string(currentUser.Role)},
	}))
}

func (h *AuthHandler) Register(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")
	passwordConfirm := c.PostForm("password_confirm")
	fullName := c.PostForm("full_name")
	email := c.PostForm("email")
	role := models.Role(c.PostForm("role"))

	// Password validation
	currentUser := GetCurrentUser(c)
	userInfo := &views.UserInfo{FullName: currentUser.FullName, Role: string(currentUser.Role)}
	if len(password) < 8 {
		views.Render(c, http.StatusOK, views.RegisterPage(views.RegisterData{
			User: userInfo, ErrorMsg: "Password must be at least 8 characters",
		}))
		return
	}
	if password != passwordConfirm {
		views.Render(c, http.StatusOK, views.RegisterPage(views.RegisterData{
			User: userInfo, ErrorMsg: "Passwords do not match",
		}))
		return
	}

	var deptID *uint
	if d, err := strconv.ParseUint(c.PostForm("department_id"), 10, 64); err == nil {
		did := uint(d)
		// Validate department belongs to the admin's organization
		var dept models.DepartmentRecord
		if err := h.DB.First(&dept, did).Error; err != nil || dept.OrganizationID != currentUser.OrganizationID {
			views.Render(c, http.StatusOK, views.RegisterPage(views.RegisterData{
				User: userInfo, ErrorMsg: "Department does not belong to your organization",
			}))
			return
		}
		deptID = &did
	}

	// Derive org from the admin creating the user — not hardcoded
	newUser, err := h.AuthSvc.Register(username, password, fullName, email, role, currentUser.OrganizationID, deptID)
	if err != nil {
		views.Render(c, http.StatusOK, views.RegisterPage(views.RegisterData{
			User: userInfo, ErrorMsg: err.Error(),
		}))
		return
	}

	h.AuditSvc.LogChange("users", newUser.ID, "user_created", currentUser.ID,
		fmt.Sprintf("User %s created with role %s", username, role), newUser)
	c.Redirect(http.StatusFound, "/admin/users")
}

// ResetPassword allows an admin to reset a user's password (e.g., for CSV-imported users).
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}
	if !h.verifyTargetSameOrg(c, uint(id)) {
		return
	}
	newPassword := c.PostForm("new_password")
	if len(newPassword) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 8 characters"})
		return
	}
	if err := h.AuthSvc.ResetPassword(uint(id), newPassword); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	currentUser := GetCurrentUser(c)
	h.AuthSvc.InvalidateUserSessions(uint(id))
	h.AuditSvc.LogChange("users", uint(id), "password_reset", currentUser.ID, "Password reset by admin", nil)
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
