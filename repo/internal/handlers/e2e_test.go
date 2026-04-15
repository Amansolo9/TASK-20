package handlers

import (
	"encoding/json"
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
	"gorm.io/gorm"
)

// ===================== END-TO-END FLOWS =====================

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

	// Step 3: GET dashboard
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/dashboard", nil)
	req3.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w3, req3)
	assert.Equal(t, 200, w3.Code)
	assert.Contains(t, w3.Body.String(), "Health Dashboard")

	// Step 4: GET bookings
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

	// Step 6: Session invalidated
	w6 := httptest.NewRecorder()
	req6, _ := http.NewRequest("GET", "/dashboard", nil)
	req6.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w6, req6)
	assert.Equal(t, 302, w6.Code)
}

func TestE2E_BookingCreateAndTransition(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	// Use a unique far-future time to avoid conflicts with leftover test data
	slotStart := time.Date(2028, 6, 15, 10, 0, 0, 0, time.UTC)
	// Clean up any leftover booking at this slot from a prior run
	db.Where("slot_start = ?", slotStart).Delete(&models.Booking{})

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

	var booking models.Booking
	err := db.Where("slot_start = ?", slotStart).Order("id DESC").First(&booking).Error
	require.NoError(t, err)
	assert.Equal(t, models.BookingInitiated, booking.Status)

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

	db.First(&booking, booking.ID)
	assert.Equal(t, models.BookingConfirmed, booking.Status)

	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/bookings/"+strconv.FormatUint(uint64(booking.ID), 10)+"/audit", nil)
	req3.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w3, req3)
	assert.Equal(t, 200, w3.Code)
	assert.Contains(t, w3.Body.String(), "confirmed")
}

// ===================== ROOT REDIRECT =====================

func TestRootRedirect(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)
	assert.Equal(t, 302, w.Code)
	assert.Equal(t, "/dashboard", w.Header().Get("Location"))
}

// ===================== POST /api/tokens =====================

func TestAPITokens_IssueToken(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	form := url.Values{}
	form.Set("description", "test token")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/tokens", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "token")
	assert.Contains(t, body, "hmac_secret")
	assert.Contains(t, body, "expires_at")
}

// ===================== GET /health/download/:id =====================

func TestHealthDownload_NotFound(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health/download/999999", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}

// ===================== GET /health/history =====================

func TestHealthHistory_RequiresAuth(t *testing.T) {
	r, _ := setupRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health/history?table=health_records&record_id=1", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 302, w.Code)
}

func TestHealthHistory_ReturnsJSON(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	// Create a health record so the object-level scope check passes
	var admin models.User
	db.First(&admin, "username = ?", "admin")
	var rec models.HealthRecord
	db.FirstOrCreate(&rec, models.HealthRecord{UserID: admin.ID, BloodType: "O+"})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health/history?table=health_records&record_id="+strconv.FormatUint(uint64(rec.ID), 10), nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
}

// ===================== CLINICIAN ENDPOINTS =====================

func TestClinicianPage_Returns200(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "clinician")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/clinician", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Clinician Dashboard")
}

func TestClinicianPage_DeniedForStudent(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/clinician", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 403, w.Code)
}

func TestClinicianEncounter_Create(t *testing.T) {
	r, db := setupRouter(t)
	// Use admin (org-wide scope) to avoid dept scope issues with dept-less patients
	sessionID := getSessionFor(t, db, "admin")

	var patient models.User
	db.First(&patient, "username = ?", "student")

	form := url.Values{}
	form.Set("patient_id", strconv.FormatUint(uint64(patient.ID), 10))
	form.Set("department", "general")
	form.Set("chief_complaint", "headache")
	form.Set("diagnosis", "tension headache")
	form.Set("treatment", "ibuprofen")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/clinician/encounter", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
}

func TestClinicianVitals_Record(t *testing.T) {
	r, db := setupRouter(t)
	// Use admin (org-wide scope) to avoid dept scope issues with dept-less patients
	sessionID := getSessionFor(t, db, "admin")

	var patient models.User
	db.First(&patient, "username = ?", "student")

	form := url.Values{}
	form.Set("user_id", strconv.FormatUint(uint64(patient.ID), 10))
	form.Set("weight_lb", "165.5")
	form.Set("bp_systolic", "120")
	form.Set("bp_diastolic", "80")
	form.Set("temperature_f", "98.6")
	form.Set("heart_rate", "72")
	form.Set("reason", "routine check-up")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/clinician/vitals", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
}

func TestClinicianVitals_MissingReason_Returns400(t *testing.T) {
	r, db := setupRouter(t)
	// Use admin (org-wide scope) to avoid dept scope issues with dept-less patients
	sessionID := getSessionFor(t, db, "admin")

	var patient models.User
	db.First(&patient, "username = ?", "student")

	form := url.Values{}
	form.Set("user_id", strconv.FormatUint(uint64(patient.ID), 10))
	form.Set("weight_lb", "165")
	form.Set("reason", "")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/clinician/vitals", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

// ===================== MENU ENDPOINTS =====================

func TestMenuPage_Returns200(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/menu", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Dining Menu")
}

func TestMenuOrder_NoItems_Returns400(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	form := url.Values{}
	form.Set("order_type", "dine_in")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/menu/order", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestMenuManagePage_Returns200_ForStaff(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "staff")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/menu/manage", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Menu Management")
}

func TestMenuManagePage_DeniedForStudent(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "student")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/menu/manage", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 403, w.Code)
}

func TestMenuManage_CreateCategory(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	form := url.Values{}
	form.Set("name", "Test Category HTTP")
	form.Set("sort_order", "99")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/menu/manage/category", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)

	var cat models.MenuCategory
	err := db.Where("name = ?", "Test Category HTTP").First(&cat).Error
	assert.NoError(t, err)
}

func TestMenuManage_CreateItem(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	var cat models.MenuCategory
	db.First(&cat, "organization_id = ?", 1)

	// Clean up any leftover item from a prior run
	db.Where("sku = ?", "HTTP-TEST-001").Delete(&models.MenuItem{})

	form := url.Values{}
	form.Set("category_id", strconv.FormatUint(uint64(cat.ID), 10))
	form.Set("sku", "HTTP-TEST-001")
	form.Set("name", "HTTP Test Item")
	form.Set("item_type", "dish")
	form.Set("base_price_dine_in", "9.99")
	form.Set("base_price_takeout", "10.99")
	form.Set("member_discount", "5")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/menu/manage/item", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
}

// ensureSeedMenuItem guarantees at least one menu item exists in org 1 for tests.
func ensureSeedMenuItem(t *testing.T, db *gorm.DB) models.MenuItem {
	t.Helper()
	var item models.MenuItem
	if err := db.Where("organization_id = ?", 1).First(&item).Error; err == nil {
		return item
	}
	// Create category + item if none exist
	var cat models.MenuCategory
	db.Where("organization_id = ?", 1).FirstOrCreate(&cat, models.MenuCategory{OrganizationID: 1, Name: "Test Entrees", SortOrder: 1})
	item = models.MenuItem{
		OrganizationID: 1, CategoryID: cat.ID, SKU: "TEST-ENT-001",
		Name: "Test Chicken Wrap", ItemType: "dish", BasePriceDineIn: 8.99, BasePriceTakeout: 9.49,
	}
	db.Create(&item)
	return item
}

func TestMenuManage_ToggleSoldOut(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	item := ensureSeedMenuItem(t, db)
	require.NotZero(t, item.ID)

	form := url.Values{}
	form.Set("sold_out", "true")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/menu/manage/item/"+strconv.FormatUint(uint64(item.ID), 10)+"/sold-out", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
	// Restore
	db.Model(&item).Update("sold_out", false)
}

func TestMenuManage_SetSellWindows(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	item := ensureSeedMenuItem(t, db)

	form := url.Values{}
	form["day_of_week"] = []string{"1"}
	form["open_time"] = []string{"08:00"}
	form["close_time"] = []string{"14:00"}
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/menu/manage/item/"+strconv.FormatUint(uint64(item.ID), 10)+"/sell-windows", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
}

func TestMenuManage_SetSubstitutes(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	item1 := ensureSeedMenuItem(t, db)
	// Create a second item if needed
	var item2 models.MenuItem
	if err := db.Where("organization_id = ? AND id != ?", 1, item1.ID).First(&item2).Error; err != nil {
		item2 = models.MenuItem{
			OrganizationID: 1, CategoryID: item1.CategoryID, SKU: "TEST-ENT-002",
			Name: "Test Veggie Burger", ItemType: "dish", BasePriceDineIn: 7.99, BasePriceTakeout: 8.49,
		}
		db.Create(&item2)
	}
	items := []models.MenuItem{item1, item2}

	form := url.Values{}
	form.Set("substitute_ids", strconv.FormatUint(uint64(items[1].ID), 10))
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/menu/manage/item/"+strconv.FormatUint(uint64(items[0].ID), 10)+"/substitutes", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
}

func TestMenuManage_AddChoice(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	item := ensureSeedMenuItem(t, db)

	form := url.Values{}
	form.Set("choice_type", "prep")
	form.Set("name", "Grilled")
	form.Set("extra_price", "1.50")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/menu/manage/item/"+strconv.FormatUint(uint64(item.ID), 10)+"/choices", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
}

func TestMenuManage_CreateBlackout(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	form := url.Values{}
	form.Set("date", "2027-12-25")
	form.Set("description", "Christmas Day")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/menu/manage/blackout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)

	var bo models.HolidayBlackout
	err := db.Where("description = ?", "Christmas Day").First(&bo).Error
	assert.NoError(t, err)
}

func TestMenuManage_DeleteBlackout(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	// Create a blackout to delete
	bo := models.HolidayBlackout{OrganizationID: 1, Date: time.Date(2027, 12, 31, 0, 0, 0, 0, time.UTC), Description: "NYE test"}
	db.Create(&bo)

	form := url.Values{}
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/menu/manage/blackout/"+strconv.FormatUint(uint64(bo.ID), 10)+"/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
}

func TestMenuManage_CreatePromotion(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	item := ensureSeedMenuItem(t, db)

	form := url.Values{}
	form.Set("menu_item_id", strconv.FormatUint(uint64(item.ID), 10))
	form.Set("discount_pct", "15")
	form.Set("starts_at", time.Now().Add(24*time.Hour).Format("2006-01-02T15:04"))
	form.Set("ends_at", time.Now().Add(48*time.Hour).Format("2006-01-02T15:04"))
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/menu/manage/promotion", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
}

// ===================== ADMIN ENDPOINTS =====================

func TestAdminRegisterPage_Returns200(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/register", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Register New User")
}

func TestAdminRegister_CreatesUser(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	// Hard delete (bypass soft-delete) leftover from prior runs
	db.Unscoped().Where("username = ?", "http_test_user").Delete(&models.User{})

	form := url.Values{}
	form.Set("username", "http_test_user")
	form.Set("password", "password123")
	form.Set("password_confirm", "password123")
	form.Set("full_name", "HTTP Test User")
	form.Set("email", "httptest@campus.local")
	form.Set("role", "student")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/admin/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)

	var user models.User
	err := db.Where("username = ?", "http_test_user").First(&user).Error
	assert.NoError(t, err)
	assert.Equal(t, "HTTP Test User", user.FullName)
}

func TestAdminToggleUser(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	var student models.User
	db.First(&student, "username = ?", "student")
	originalActive := student.Active

	form := url.Values{}
	form.Set("active", "false")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/admin/users/"+strconv.FormatUint(uint64(student.ID), 10)+"/toggle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)

	db.First(&student, student.ID)
	assert.False(t, student.Active)

	// Restore
	db.Model(&student).Update("active", originalActive)
}

func TestAdminTempAccess(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	var student models.User
	db.First(&student, "username = ?", "student")
	originalRole := student.Role

	form := url.Values{}
	form.Set("role", "clinician")
	form.Set("duration_hours", "2")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/admin/users/"+strconv.FormatUint(uint64(student.ID), 10)+"/temp-access", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)

	// Restore
	db.Model(&student).Update("role", originalRole)
}

func TestAdminResetPassword(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	var student models.User
	db.First(&student, "username = ?", "student")

	form := url.Values{}
	form.Set("new_password", "newpassword123")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/admin/users/"+strconv.FormatUint(uint64(student.ID), 10)+"/reset-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
}

func TestAdminPerformancePage_Returns200(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/performance", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Performance Dashboard")
}

func TestAdminRefreshViews(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	form := url.Values{}
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/admin/refresh-views", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "refreshed")
}

func TestAdminWebhooksPage_Returns200(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/webhooks", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "Webhook Management")
}

func TestAdminRegisterWebhook(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	form := url.Values{}
	form.Set("url", "http://localhost:9999/hook")
	form.Set("event_type", "booking.created")
	form.Set("secret", "test-secret")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/admin/webhooks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 302, w.Code)
}

func TestAdminBookingsPage_Returns200(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/bookings", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "All Training Sessions")
}

func TestAdminSubmitReport(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	form := url.Values{}
	form.Set("report_type", "clinic_utilization")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/admin/reports", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 202, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "job_id")
	assert.Contains(t, body, "pending")
}

func TestAdminGetReportStatus(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	// Create a report job first
	var admin models.User
	db.First(&admin, "username = ?", "admin")
	job := models.ReportJob{OrganizationID: admin.OrganizationID, ReportType: "clinic_utilization", Status: "completed", RequestedBy: admin.ID}
	db.Create(&job)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/reports/"+strconv.FormatUint(uint64(job.ID), 10), nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "completed")
}

// ===================== REST API ENDPOINTS (token+HMAC) =====================

func TestAPI_MatchPartners(t *testing.T) {
	r, db := setupRouter(t)

	// Ensure the student user has a trainer profile (may have been cleaned by other tests)
	var student models.User
	db.First(&student, "username = ?", "student")
	db.Where("user_id = ?", student.ID).FirstOrCreate(&models.TrainerProfile{
		UserID: student.ID, SkillLevel: 3, WeightClass: 145, PrimaryStyle: "boxing",
	})

	token := getTokenFor(t, db, "student")

	w := httptest.NewRecorder()
	req := newSignedAPIRequest(t, "GET", "/api/match-partners?skill_range=5&weight_range=50", token)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "matches")
}

func TestAPI_CheckConflicts(t *testing.T) {
	r, db := setupRouter(t)
	token := getTokenFor(t, db, "student")

	slotStart := time.Now().Add(72 * time.Hour).Format(time.RFC3339)
	w := httptest.NewRecorder()
	req := newSignedAPIRequest(t, "GET", "/api/check-conflicts?venue_id=1&slot_start="+url.QueryEscape(slotStart), token)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "conflicts")
}

func TestAPI_Price(t *testing.T) {
	r, db := setupRouter(t)
	token := getTokenFor(t, db, "student")

	item := ensureSeedMenuItem(t, db)

	w := httptest.NewRecorder()
	req := newSignedAPIRequest(t, "GET", "/api/price?item_id="+strconv.FormatUint(uint64(item.ID), 10)+"&order_type=dine_in", token)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotNil(t, resp["price"])
}

// ===================== INTERNAL API: webhooks/receive =====================

func TestInternalAPI_WebhookReceive_ValidEvent(t *testing.T) {
	r, db := setupRouter(t)
	token := getTokenFor(t, db, "admin")

	payload := `{"event_type":"booking.created","booking_id":1}`
	bodyBytes := []byte(payload)
	bodyHash := sha256Hex(bodyBytes)
	ts := time.Now().Format(time.RFC3339)
	sig := hmacSign("test-hmac", "POST", "/api/internal/webhooks/receive", ts, bodyHash)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/internal/webhooks/receive", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-HMAC-Signature", sig)
	req.Header.Set("X-HMAC-Timestamp", ts)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "received")
}

func TestInternalAPI_WebhookReceive_UnknownEvent(t *testing.T) {
	r, db := setupRouter(t)
	token := getTokenFor(t, db, "admin")

	payload := `{"event_type":"unknown.event"}`
	bodyBytes := []byte(payload)
	bodyHash := sha256Hex(bodyBytes)
	ts := time.Now().Format(time.RFC3339)
	sig := hmacSign("test-hmac", "POST", "/api/internal/webhooks/receive", ts, bodyHash)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/internal/webhooks/receive", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-HMAC-Signature", sig)
	req.Header.Set("X-HMAC-Timestamp", ts)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "unknown event type")
}

// ===================== CROSS-TENANT & VALIDATION =====================

func TestCrossTenant_AdminUsersPage_StrongNegativeAssertion(t *testing.T) {
	r, db := setupRouter(t)
	createSecondOrg(t, db)

	sessionID := getSessionFor(t, db, "admin")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/users", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)

	body := w.Body.String()
	assert.Contains(t, body, "admin")
	assert.Contains(t, body, "student")
	assert.NotContains(t, body, "admin2")
	assert.NotContains(t, body, "clinician2")
	assert.NotContains(t, body, "Admin Two")
}

func TestHealthUpdate_RequiresReason(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	form := url.Values{}
	form.Set("allergies", "penicillin")
	form.Set("reason", "")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/health/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "reason is required")
}

func TestHealthUpdate_RejectsOversizedFields(t *testing.T) {
	r, db := setupRouter(t)
	sessionID := getSessionFor(t, db, "admin")

	form := url.Values{}
	form.Set("allergies", strings.Repeat("x", 10241))
	form.Set("reason", "test")
	form.Set("csrf_token", "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/health/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "exceeds maximum length")
}

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

func TestAPISlots_ReturnsValidPayload(t *testing.T) {
	r, db := setupRouter(t)
	token := getTokenFor(t, db, "student")

	w := httptest.NewRecorder()
	req := newSignedAPIRequest(t, "GET", "/api/slots?venue_id=1&date=2026-12-01", token)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "slots")
	assert.True(t, strings.HasPrefix(strings.TrimSpace(body), "{"))
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
