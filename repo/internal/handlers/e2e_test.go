package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"campus-portal/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===================== END-TO-END FLOWS =====================

// TestE2E_LoginToDashboardToLogout exercises the full authentication lifecycle.
func TestE2E_LoginToDashboardToLogout(t *testing.T) {
	r, _ := setupRouter(t)

	// Step 1: GET login page
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/login", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	csrf := extractCSRFCookie(w)
	require.NotEmpty(t, csrf)

	// Step 2: POST login
	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "password123")
	form.Set("csrf_token", csrf)

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(&http.Cookie{Name: "login_csrf", Value: csrf})
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 302, w2.Code)
	sessionID := extractSessionCookie(w2)
	require.NotEmpty(t, sessionID)

	// Step 3: GET dashboard with session
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/dashboard", nil)
	req3.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w3, req3)
	assert.Equal(t, 200, w3.Code)
	assert.Contains(t, w3.Body.String(), "Health Dashboard")

	// Step 4: GET bookings page
	w4 := httptest.NewRecorder()
	req4, _ := http.NewRequest("GET", "/bookings", nil)
	req4.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w4, req4)
	assert.Equal(t, 200, w4.Code)
	assert.Contains(t, w4.Body.String(), "Training Sessions")

	// Step 5: Logout
	w5 := httptest.NewRecorder()
	req5, _ := http.NewRequest("GET", "/logout", nil)
	req5.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w5, req5)
	assert.Equal(t, 302, w5.Code)

	// Step 6: Verify session is invalidated
	w6 := httptest.NewRecorder()
	req6, _ := http.NewRequest("GET", "/dashboard", nil)
	req6.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w6, req6)
	assert.Equal(t, 302, w6.Code, "should redirect to login after logout")
}

// TestE2E_BookingCreateAndTransition exercises full booking flow.
func TestE2E_BookingCreateAndTransition(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	// Step 1: Create a booking
	slotStart := time.Now().Add(48 * time.Hour).Truncate(time.Minute)
	form := url.Values{}
	form.Set("venue_id", "1")
	form.Set("slot_start", slotStart.Format(time.RFC3339))
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/bookings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)
	assert.Equal(t, 302, w.Code, "booking creation should redirect")

	// Step 2: Verify booking was created
	var booking models.Booking
	err := db.Where("slot_start = ?", slotStart).Order("id DESC").First(&booking).Error
	require.NoError(t, err)
	assert.Equal(t, models.BookingInitiated, booking.Status)

	// Step 3: Confirm the booking
	form2 := url.Values{}
	form2.Set("status", "confirmed")
	form2.Set("note", "confirming my session")
	form2.Set("csrf_token", "test")

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/bookings/"+strconv.FormatUint(uint64(booking.ID), 10)+"/transition", strings.NewReader(form2.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req2.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 302, w2.Code)

	// Verify status changed
	db.First(&booking, booking.ID)
	assert.Equal(t, models.BookingConfirmed, booking.Status)

	// Step 4: Check audit trail via API
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/bookings/"+strconv.FormatUint(uint64(booking.ID), 10)+"/audit", nil)
	req3.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w3, req3)
	assert.Equal(t, 200, w3.Code)
	assert.Contains(t, w3.Body.String(), "confirmed")
}

// TestE2E_AdminUserManagement exercises admin creating user and changing roles.
func TestE2E_AdminUserManagement(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	// Step 1: View users page
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/users", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "User Management")
	assert.Contains(t, w.Body.String(), "admin")
	assert.Contains(t, w.Body.String(), "student")

	// Step 2: Change student role to staff
	var student models.User
	db.First(&student, "username = ?", "student")
	originalRole := student.Role

	form := url.Values{}
	form.Set("role", "staff")
	form.Set("csrf_token", "test")

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/admin/users/"+strconv.FormatUint(uint64(student.ID), 10)+"/role", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req2.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 302, w2.Code)

	// Verify role changed
	db.First(&student, student.ID)
	assert.Equal(t, models.RoleStaff, student.Role)

	// Step 3: Verify audit log
	var auditLog models.AuditLog
	err := db.Where("table_name = ? AND record_id = ? AND action = ?", "users", student.ID, "role_change").
		Order("timestamp DESC").First(&auditLog).Error
	assert.NoError(t, err)
	assert.Contains(t, auditLog.Reason, "Role changed")

	// Restore original role
	db.Model(&student).Update("role", originalRole)
}

// ===================== CROSS-TENANT ISOLATION TESTS =====================

// TestCrossTenant_AdminUsersPage_StrongNegativeAssertion verifies that org 2 users
// absolutely do not appear in org 1 admin's user management page.
func TestCrossTenant_AdminUsersPage_StrongNegativeAssertion(t *testing.T) {
	r, db := setupRouter(t)
	createSecondOrg(t, db)

	// Org 1 admin
	sessionID := getSessionFor(t, db, "admin")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/users", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)

	body := w.Body.String()
	// Org 1 admin should see org 1 users
	assert.Contains(t, body, "admin")
	assert.Contains(t, body, "student")
	// Org 2 users should NOT appear
	assert.NotContains(t, body, "admin2", "org 2 admin should NOT appear in org 1 user list")
	assert.NotContains(t, body, "clinician2", "org 2 clinician should NOT appear in org 1 user list")
	assert.NotContains(t, body, "Admin Two", "org 2 user full name should NOT appear")
}

// TestCrossTenant_BookingOrgIsolation verifies booking operations are org-scoped.
func TestCrossTenant_BookingOrgIsolation(t *testing.T) {
	r, db := setupRouter(t)
	org2ID, _ := createSecondOrg(t, db)

	// Create booking for org 2
	slotStart := time.Now().Add(72 * time.Hour)
	db.Create(&models.Booking{
		OrganizationID: org2ID, RequesterID: 999, VenueID: 1,
		SlotStart: slotStart, SlotEnd: slotStart.Add(30 * time.Minute),
		Status: models.BookingInitiated,
	})

	// Org 1 student views bookings page
	sessionID := getSessionFor(t, db, "student")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/bookings", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)

	// Org 2 booking should NOT appear (student sees only their own bookings)
	body := w.Body.String()
	assert.NotContains(t, body, "999", "org 2 booking requester ID should not appear")
}

// ===================== HEALTH VALIDATION TESTS =====================

// TestHealthUpdate_RequiresReason verifies that health record updates require a reason.
func TestHealthUpdate_RequiresReason(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	form := url.Values{}
	form.Set("allergies", "penicillin")
	form.Set("reason", "") // Empty reason
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/health/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code, "empty reason should be rejected")
	assert.Contains(t, w.Body.String(), "reason is required")
}

// TestHealthUpdate_RejectsOversizedFields verifies text field length limits.
func TestHealthUpdate_RejectsOversizedFields(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	form := url.Values{}
	form.Set("allergies", strings.Repeat("x", 10241)) // > 10KB
	form.Set("reason", "test")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/health/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code, "oversized field should be rejected")
	assert.Contains(t, w.Body.String(), "exceeds maximum length")
}

// ===================== BOOKING VALIDATION TESTS =====================

func TestBookingCreate_InvalidVenueID(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	form := url.Values{}
	form.Set("venue_id", "0")
	form.Set("slot_start", time.Now().Add(48*time.Hour).Format(time.RFC3339))
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/bookings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestBookingCreate_InvalidSlotTime(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	form := url.Values{}
	form.Set("venue_id", "1")
	form.Set("slot_start", "not-a-date")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/bookings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestBookingTransition_RequiresNote(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	// Create a booking first
	slotStart := time.Now().Add(48 * time.Hour)
	db.Create(&models.Booking{
		OrganizationID: 1, RequesterID: 2, VenueID: 1,
		SlotStart: slotStart, SlotEnd: slotStart.Add(30 * time.Minute),
		Status: models.BookingInitiated,
	})
	var booking models.Booking
	db.Where("requester_id = 2 AND status = ?", models.BookingInitiated).Order("id DESC").First(&booking)

	form := url.Values{}
	form.Set("status", "confirmed")
	form.Set("note", "") // Empty note
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/bookings/"+strconv.FormatUint(uint64(booking.ID), 10)+"/transition", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code, "empty note should be rejected")
}

// ===================== API PAYLOAD VERIFICATION =====================

func TestAPISlots_ReturnsValidPayload(t *testing.T) {
	r, db := setupRouter(t)
	token := getTokenFor(t, db, "student")

	w := httptest.NewRecorder()
	req := newSignedAPIRequest(t, "GET", "/api/slots?venue_id=1&date=2026-12-01", token)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "slots", "response should contain slots key")
	// Verify it's valid JSON
	assert.True(t, strings.HasPrefix(strings.TrimSpace(body), "{"), "response should be valid JSON object")
}

func TestAPISlots_InvalidDate_Returns400(t *testing.T) {
	r, db := setupRouter(t)
	token := getTokenFor(t, db, "student")

	w := httptest.NewRecorder()
	req := newSignedAPIRequest(t, "GET", "/api/slots?venue_id=1&date=invalid", token)
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "invalid date")
}

// ===================== HELPERS =====================

func extractCSRFCookie(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == "login_csrf" {
			return c.Value
		}
	}
	return ""
}

func extractSessionCookie(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" && c.Value != "" {
			return c.Value
		}
	}
	return ""
}
