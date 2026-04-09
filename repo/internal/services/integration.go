package services

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"campus-portal/internal/models"

	"gorm.io/gorm"
)

// ===================== CSV WATCHER =====================

type CSVWatcher struct {
	DB       *gorm.DB
	WatchDir string
	done     chan struct{}
}

func NewCSVWatcher(db *gorm.DB, watchDir string) *CSVWatcher {
	return &CSVWatcher{DB: db, WatchDir: watchDir, done: make(chan struct{})}
}

func (w *CSVWatcher) Start() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				w.processFiles()
			case <-w.done:
				return
			}
		}
	}()
	log.Printf("CSV Watcher started on directory: %s", w.WatchDir)
}

func (w *CSVWatcher) Stop() {
	close(w.done)
}

func (w *CSVWatcher) processFiles() {
	if err := os.MkdirAll(w.WatchDir, 0755); err != nil {
		return
	}

	files, err := filepath.Glob(filepath.Join(w.WatchDir, "*.csv"))
	if err != nil {
		return
	}

	for _, file := range files {
		if err := w.importCSV(file); err != nil {
			log.Printf("Error importing %s: %v", file, err)
			// Move to error folder
			errDir := filepath.Join(w.WatchDir, "errors")
			os.MkdirAll(errDir, 0755)
			os.Rename(file, filepath.Join(errDir, filepath.Base(file)))
		} else {
			// Move to processed folder
			procDir := filepath.Join(w.WatchDir, "processed")
			os.MkdirAll(procDir, 0755)
			os.Rename(file, filepath.Join(procDir, filepath.Base(file)))
			log.Printf("Successfully imported: %s", filepath.Base(file))
		}
	}
}

func (w *CSVWatcher) importCSV(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return err
	}

	if len(records) < 2 {
		return fmt.Errorf("CSV file has no data rows")
	}

	headers := records[0]
	headerMap := make(map[string]int)
	for i, h := range headers {
		headerMap[strings.ToLower(strings.TrimSpace(h))] = i
	}

	baseName := strings.ToLower(filepath.Base(filePath))

	if strings.Contains(baseName, "enrollment") || strings.Contains(baseName, "student") {
		return w.importEnrollment(records[1:], headerMap)
	} else if strings.Contains(baseName, "org") || strings.Contains(baseName, "department") {
		return w.importOrgStructure(records[1:], headerMap)
	}

	return fmt.Errorf("unrecognized CSV type: %s", baseName)
}

func (w *CSVWatcher) importEnrollment(rows [][]string, headers map[string]int) error {
	for _, row := range rows {
		username := getField(row, headers, "username")
		fullName := getField(row, headers, "full_name")
		email := getField(row, headers, "email")
		role := getField(row, headers, "role")

		if username == "" || fullName == "" {
			continue
		}

		// Validate role against allowed values
		validRoles := map[string]bool{
			"student": true, "faculty": true, "clinician": true, "staff": true, "admin": true,
		}
		if role != "" && !validRoles[role] {
			log.Printf("CSV import: skipping user %s — invalid role %q", username, role)
			continue
		}
		if role == "" {
			role = "student" // Default role
		}

		// Derive org from CSV field or default to first org
		orgName := getField(row, headers, "organization")
		var orgID uint
		if orgName != "" {
			var org models.Organization
			if err := w.DB.Where("name = ?", orgName).First(&org).Error; err == nil {
				orgID = org.ID
			}
		}
		if orgID == 0 {
			var org models.Organization
			w.DB.First(&org)
			orgID = org.ID
		}

		// Eligibility: CSV can include "eligible" field (true/false). Ineligible users are deactivated.
		eligible := getField(row, headers, "eligible")
		isEligible := eligible == "" || strings.EqualFold(eligible, "true") || eligible == "1"

		// Department sync from CSV
		deptName := getField(row, headers, "department")
		var deptID *uint
		if deptName != "" {
			var dept models.DepartmentRecord
			if err := w.DB.Where("name = ? AND organization_id = ?", deptName, orgID).First(&dept).Error; err == nil {
				deptID = &dept.ID
			}
		}

		var user models.User
		err := w.DB.Where("username = ?", username).First(&user).Error
		if err == gorm.ErrRecordNotFound {
			user = models.User{
				Username:       username,
				PasswordHash:   "$2a$10$placeholder", // Needs admin password reset
				FullName:       fullName,
				Email:          email,
				Role:           models.Role(role),
				OrganizationID: orgID,
				DepartmentID:   deptID,
				Active:         isEligible,
			}
			w.DB.Create(&user)
		} else if err == nil {
			user.FullName = fullName
			user.Email = email
			user.Active = isEligible
			if deptID != nil {
				user.DepartmentID = deptID
			}
			w.DB.Save(&user)
		}
	}
	return nil
}

func (w *CSVWatcher) importOrgStructure(rows [][]string, headers map[string]int) error {
	for _, row := range rows {
		deptName := getField(row, headers, "department")
		orgName := getField(row, headers, "organization")

		if deptName == "" {
			continue
		}

		var org models.Organization
		if orgName != "" {
			w.DB.Where("name = ?", orgName).FirstOrCreate(&org, models.Organization{Name: orgName})
		} else {
			w.DB.First(&org)
		}

		var dept models.DepartmentRecord
		w.DB.Where("name = ? AND organization_id = ?", deptName, org.ID).
			FirstOrCreate(&dept, models.DepartmentRecord{Name: deptName, OrganizationID: org.ID})
	}
	return nil
}

func getField(row []string, headers map[string]int, field string) string {
	if idx, ok := headers[field]; ok && idx < len(row) {
		return strings.TrimSpace(row[idx])
	}
	return ""
}

// ===================== WEBHOOK SYSTEM =====================

type WebhookService struct {
	DB     *gorm.DB
	client *http.Client
	mu     sync.Mutex
}

func NewWebhookService(db *gorm.DB) *WebhookService {
	return &WebhookService{
		DB:     db,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *WebhookService) RegisterEndpoint(url, eventType, secret string) error {
	return s.RegisterEndpointForOrg(url, eventType, secret, 0)
}

// IsInternalURL checks if a URL points to a private/internal network address only.
func IsInternalURL(rawURL string) bool {
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	// Allow loopback, .local, .internal, .corp, and RFC1918 patterns
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".corp") {
		return true
	}
	// Check RFC1918 prefixes
	for _, prefix := range []string{"10.", "192.168.", "172.16.", "172.17.", "172.18.", "172.19.",
		"172.20.", "172.21.", "172.22.", "172.23.", "172.24.", "172.25.", "172.26.", "172.27.",
		"172.28.", "172.29.", "172.30.", "172.31."} {
		if strings.HasPrefix(host, prefix) {
			return true
		}
	}
	return false
}

func (s *WebhookService) RegisterEndpointForOrg(rawURL, eventType, secret string, orgID uint) error {
	if !IsInternalURL(rawURL) {
		return fmt.Errorf("webhook URL must target an internal network address")
	}
	ep := &models.WebhookEndpoint{
		OrganizationID: orgID,
		URL:            rawURL,
		EventType:      eventType,
		Secret:         secret,
		Active:         true,
	}
	return s.DB.Create(ep).Error
}

// DispatchForOrg sends webhook events scoped to a specific organization.
// orgID is required and must be > 0 to enforce tenant isolation.
func (s *WebhookService) DispatchForOrg(eventType string, payload interface{}, orgID uint) {
	if orgID == 0 {
		log.Printf("WARNING: DispatchForOrg called with orgID=0, skipping broadcast to prevent cross-tenant leak")
		return
	}
	var endpoints []models.WebhookEndpoint
	s.DB.Where("event_type = ? AND active = true AND organization_id = ?", eventType, orgID).Find(&endpoints)

	data, _ := json.Marshal(payload)

	for _, ep := range endpoints {
		go s.deliver(ep, data)
	}
}

func (s *WebhookService) deliver(endpoint models.WebhookEndpoint, payload []byte) {
	// Delivery-time revalidation: ensure target is still internal
	if !IsInternalURL(endpoint.URL) {
		s.logDelivery(endpoint.ID, string(payload), 0, "delivery blocked: non-internal URL", 1)
		return
	}
	// Sign the payload once (payload bytes are immutable)
	mac := hmac.New(sha256.New, []byte(endpoint.Secret))
	mac.Write(payload)
	signature := hex.EncodeToString(mac.Sum(nil))

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Create a FRESH request with a new body reader on every attempt
		req, err := http.NewRequest("POST", endpoint.URL, bytes.NewReader(payload))
		if err != nil {
			s.logDelivery(endpoint.ID, string(payload), 0, err.Error(), attempt)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Signature", signature)
		req.Header.Set("X-Webhook-Event", endpoint.EventType)

		resp, err := s.client.Do(req)
		if err != nil {
			if attempt == maxRetries {
				s.logDelivery(endpoint.ID, string(payload), 0, err.Error(), attempt)
			}
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}
		resp.Body.Close()
		s.logDelivery(endpoint.ID, string(payload), resp.StatusCode, "", attempt)
		return
	}
}

func (s *WebhookService) logDelivery(endpointID uint, payload string, status int, response string, attempts int) {
	s.DB.Create(&models.WebhookDelivery{
		EndpointID: endpointID,
		Payload:    payload,
		Status:     status,
		Response:   response,
		Attempts:   attempts,
	})
}

func (s *WebhookService) GetDeliveries(endpointID uint) ([]models.WebhookDelivery, error) {
	var deliveries []models.WebhookDelivery
	err := s.DB.Where("endpoint_id = ?", endpointID).Order("created_at DESC").Find(&deliveries).Error
	return deliveries, err
}

func (s *WebhookService) GetEndpoints(orgID uint) ([]models.WebhookEndpoint, error) {
	var endpoints []models.WebhookEndpoint
	err := s.DB.Where("organization_id = ?", orgID).Find(&endpoints).Error
	return endpoints, err
}

// ===================== REPORTING =====================

type ReportingService struct {
	DB       *gorm.DB
	cache    map[string]*cacheEntry
	mu       sync.RWMutex
	cacheTTL time.Duration
}

type cacheEntry struct {
	data      interface{}
	expiresAt time.Time
}

func NewReportingService(db *gorm.DB, cacheTTL time.Duration) *ReportingService {
	return &ReportingService{
		DB:       db,
		cache:    make(map[string]*cacheEntry),
		cacheTTL: cacheTTL,
	}
}

func (s *ReportingService) RefreshMaterializedViews() {
	views := []string{"mv_clinic_utilization", "mv_booking_fill_rates", "mv_menu_sell_through"}
	for _, v := range views {
		start := time.Now()
		// Try CONCURRENTLY first (requires unique index); fall back to blocking refresh
		result := s.DB.Exec(fmt.Sprintf("REFRESH MATERIALIZED VIEW CONCURRENTLY %s", v))
		if result.Error != nil {
			s.DB.Exec(fmt.Sprintf("REFRESH MATERIALIZED VIEW %s", v))
		}
		log.Printf("Refreshed %s in %v", v, time.Since(start))
	}
}

// Org-scoped reporting: reads from materialized views with organization_id filter.
// The materialized views are refreshed on a schedule (default every 15 min) and
// provide precomputed aggregates for fast report queries.
func (s *ReportingService) GetClinicUtilization(orgID uint) ([]map[string]interface{}, error) {
	query := `SELECT day, department, encounter_count
		FROM mv_clinic_utilization
		WHERE organization_id = ?
		ORDER BY day DESC LIMIT 100`
	return s.orgQuery(fmt.Sprintf("clinic_utilization_%d", orgID), query, orgID)
}

func (s *ReportingService) GetBookingFillRates(orgID uint) ([]map[string]interface{}, error) {
	query := `SELECT day, venue_id, total_bookings, confirmed, canceled
		FROM mv_booking_fill_rates
		WHERE organization_id = ?
		ORDER BY day DESC LIMIT 100`
	return s.orgQuery(fmt.Sprintf("booking_fill_%d", orgID), query, orgID)
}

func (s *ReportingService) GetMenuSellThrough(orgID uint) ([]map[string]interface{}, error) {
	query := `SELECT sku, name, total_sold, total_revenue
		FROM mv_menu_sell_through
		WHERE organization_id = ?
		ORDER BY total_sold DESC LIMIT 100`
	return s.orgQuery(fmt.Sprintf("menu_sell_%d", orgID), query, orgID)
}

func (s *ReportingService) orgQuery(cacheKey, query string, orgID uint) ([]map[string]interface{}, error) {
	s.mu.RLock()
	if entry, ok := s.cache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		s.mu.RUnlock()
		return entry.data.([]map[string]interface{}), nil
	}
	s.mu.RUnlock()

	var results []map[string]interface{}
	rows, err := s.DB.Raw(query, orgID).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	for rows.Next() {
		row := make(map[string]interface{})
		vals := make([]interface{}, len(cols))
		valPtrs := make([]interface{}, len(cols))
		for i := range vals {
			valPtrs[i] = &vals[i]
		}
		rows.Scan(valPtrs...)
		for i, col := range cols {
			row[col] = vals[i]
		}
		results = append(results, row)
	}

	s.mu.Lock()
	s.cache[cacheKey] = &cacheEntry{data: results, expiresAt: time.Now().Add(s.cacheTTL)}
	s.mu.Unlock()
	return results, nil
}

func (s *ReportingService) cachedQuery(key, query string) ([]map[string]interface{}, error) {
	s.mu.RLock()
	if entry, ok := s.cache[key]; ok && time.Now().Before(entry.expiresAt) {
		s.mu.RUnlock()
		return entry.data.([]map[string]interface{}), nil
	}
	s.mu.RUnlock()

	var results []map[string]interface{}
	rows, err := s.DB.Raw(query).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	for rows.Next() {
		row := make(map[string]interface{})
		vals := make([]interface{}, len(cols))
		valPtrs := make([]interface{}, len(cols))
		for i := range vals {
			valPtrs[i] = &vals[i]
		}
		rows.Scan(valPtrs...)
		for i, col := range cols {
			row[col] = vals[i]
		}
		results = append(results, row)
	}

	s.mu.Lock()
	s.cache[key] = &cacheEntry{data: results, expiresAt: time.Now().Add(s.cacheTTL)}
	s.mu.Unlock()

	return results, nil
}

func (s *ReportingService) GetSlowQueries(limit int) ([]models.SlowQueryLog, error) {
	var logs []models.SlowQueryLog
	err := s.DB.Order("created_at DESC").Limit(limit).Find(&logs).Error
	return logs, err
}

// CleanupSlowQueryLogs removes slow query log entries older than the given retention period.
func (s *ReportingService) CleanupSlowQueryLogs(retentionDays int) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result := s.DB.Where("created_at < ?", cutoff).Delete(&models.SlowQueryLog{})
	if result.RowsAffected > 0 {
		log.Printf("Cleaned up %d slow query log entries older than %d days", result.RowsAffected, retentionDays)
	}
}

// ===================== ASYNC REPORTING =====================

// SubmitReportJob creates a pending report job and executes it asynchronously.
func (s *ReportingService) SubmitReportJob(orgID uint, reportType string, requestedBy uint) (*models.ReportJob, error) {
	job := &models.ReportJob{
		OrganizationID: orgID,
		ReportType:     reportType,
		Status:         "pending",
		RequestedBy:    requestedBy,
	}
	if err := s.DB.Create(job).Error; err != nil {
		return nil, err
	}
	go s.executeReportJob(job.ID)
	return job, nil
}

func (s *ReportingService) executeReportJob(jobID uint) {
	var job models.ReportJob
	if err := s.DB.First(&job, jobID).Error; err != nil {
		return
	}
	s.DB.Model(&job).Update("status", "running")

	var data []map[string]interface{}
	var err error
	switch job.ReportType {
	case "clinic_utilization":
		data, err = s.GetClinicUtilization(job.OrganizationID)
	case "booking_fill_rates":
		data, err = s.GetBookingFillRates(job.OrganizationID)
	case "menu_sell_through":
		data, err = s.GetMenuSellThrough(job.OrganizationID)
	default:
		err = fmt.Errorf("unknown report type: %s", job.ReportType)
	}

	now := time.Now()
	if err != nil {
		s.DB.Model(&job).Updates(map[string]interface{}{"status": "failed", "result": err.Error(), "completed_at": now})
		return
	}
	jsonData, _ := json.Marshal(data)
	s.DB.Model(&job).Updates(map[string]interface{}{"status": "completed", "result": string(jsonData), "completed_at": now})
}

// GetReportJob returns a report job by ID, org-scoped.
func (s *ReportingService) GetReportJob(jobID uint, orgID uint) (*models.ReportJob, error) {
	var job models.ReportJob
	err := s.DB.Where("id = ? AND organization_id = ?", jobID, orgID).First(&job).Error
	return &job, err
}

// ===================== PII MASKING =====================

func MaskSSN(ssn string, role models.Role) string {
	if ssn == "" {
		return ""
	}
	if role == models.RoleAdmin {
		return ssn
	}
	// Show only last 4 digits
	clean := strings.ReplaceAll(strings.ReplaceAll(ssn, "-", ""), " ", "")
	if len(clean) >= 4 {
		return "***-**-" + clean[len(clean)-4:]
	}
	return "***-**-****"
}

func MaskEmail(email string, role models.Role) string {
	if role == models.RoleAdmin || role == models.RoleClinician {
		return email
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "***@***"
	}
	name := parts[0]
	if len(name) > 2 {
		name = name[:2] + "***"
	}
	return name + "@" + parts[1]
}
