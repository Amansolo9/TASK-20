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

		var user models.User
		err := w.DB.Where("username = ?", username).First(&user).Error
		if err == gorm.ErrRecordNotFound {
			user = models.User{
				Username:       username,
				PasswordHash:   "$2a$10$placeholder", // Needs reset
				FullName:       fullName,
				Email:          email,
				Role:           models.Role(role),
				OrganizationID: 1,
				Active:         true,
			}
			w.DB.Create(&user)
		} else if err == nil {
			user.FullName = fullName
			user.Email = email
			user.Active = true
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
	ep := &models.WebhookEndpoint{
		URL:       url,
		EventType: eventType,
		Secret:    secret,
		Active:    true,
	}
	return s.DB.Create(ep).Error
}

func (s *WebhookService) Dispatch(eventType string, payload interface{}) {
	var endpoints []models.WebhookEndpoint
	s.DB.Where("event_type = ? AND active = true", eventType).Find(&endpoints)

	data, _ := json.Marshal(payload)

	for _, ep := range endpoints {
		go s.deliver(ep, data)
	}
}

func (s *WebhookService) deliver(endpoint models.WebhookEndpoint, payload []byte) {
	// Sign the payload
	mac := hmac.New(sha256.New, []byte(endpoint.Secret))
	mac.Write(payload)
	signature := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest("POST", endpoint.URL, bytes.NewReader(payload))
	if err != nil {
		s.logDelivery(endpoint.ID, string(payload), 0, err.Error(), 1)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signature)
	req.Header.Set("X-Webhook-Event", endpoint.EventType)

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := s.client.Do(req)
		if err != nil {
			if attempt == maxRetries {
				s.logDelivery(endpoint.ID, string(payload), 0, err.Error(), attempt)
			}
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}
		defer resp.Body.Close()
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

func (s *WebhookService) GetEndpoints() ([]models.WebhookEndpoint, error) {
	var endpoints []models.WebhookEndpoint
	err := s.DB.Find(&endpoints).Error
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

func (s *ReportingService) GetClinicUtilization() ([]map[string]interface{}, error) {
	return s.cachedQuery("clinic_utilization", "SELECT * FROM mv_clinic_utilization LIMIT 100")
}

func (s *ReportingService) GetBookingFillRates() ([]map[string]interface{}, error) {
	return s.cachedQuery("booking_fill_rates", "SELECT * FROM mv_booking_fill_rates LIMIT 100")
}

func (s *ReportingService) GetMenuSellThrough() ([]map[string]interface{}, error) {
	return s.cachedQuery("menu_sell_through", "SELECT * FROM mv_menu_sell_through LIMIT 100")
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
