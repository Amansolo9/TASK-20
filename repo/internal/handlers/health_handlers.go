package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"campus-portal/internal/middleware"
	"campus-portal/internal/models"
	"campus-portal/internal/services"

	"github.com/gin-gonic/gin"
)

type HealthHandler struct {
	HealthSvc *services.HealthService
	AuditSvc  *services.AuditService
}

func NewHealthHandler(healthSvc *services.HealthService, auditSvc *services.AuditService) *HealthHandler {
	return &HealthHandler{HealthSvc: healthSvc, AuditSvc: auditSvc}
}

// enforceScopeForHealth uses EnforceDeptScope for department-level users (clinicians/staff)
// and EnforceSelfScope for students/faculty. This ensures cross-department access is blocked.
func (h *HealthHandler) enforceScopeForHealth(c *gin.Context, targetUserID uint) bool {
	user := GetCurrentUser(c)
	if user.Role == models.RoleClinician || user.Role == models.RoleStaff {
		return middleware.EnforceDeptScope(c, h.HealthSvc.DB, targetUserID)
	}
	return middleware.EnforceSelfScope(c, targetUserID)
}

func (h *HealthHandler) DashboardPage(c *gin.Context) {
	user := GetCurrentUser(c)

	// For students/faculty, show their own records
	// For clinicians/admins, show department view
	targetUserID := user.ID
	if qID := c.Query("user_id"); qID != "" {
		if id, err := strconv.ParseUint(qID, 10, 64); err == nil {
			if h.enforceScopeForHealth(c, uint(id)) {
				targetUserID = uint(id)
			}
		}
	}

	record, recErr := h.HealthSvc.GetHealthRecord(targetUserID)
	vitals, vitErr := h.HealthSvc.GetRecentVitals(targetUserID, 10)
	encounters, encErr := h.HealthSvc.GetEncounters(targetUserID)
	attachments, attErr := h.HealthSvc.GetAttachments(targetUserID)

	var serviceErrors []string
	if recErr != nil && recErr.Error() != "record not found" {
		serviceErrors = append(serviceErrors, "Failed to load health record")
	}
	if vitErr != nil {
		serviceErrors = append(serviceErrors, "Failed to load vitals")
	}
	if encErr != nil {
		serviceErrors = append(serviceErrors, "Failed to load encounters")
	}
	if attErr != nil {
		serviceErrors = append(serviceErrors, "Failed to load attachments")
	}

	maskedSSN := services.MaskSSN(user.SSN, user.Role)

	c.HTML(http.StatusOK, "dashboard.html", gin.H{
		"title":         "Health Dashboard",
		"user":          user,
		"record":        record,
		"vitals":        vitals,
		"encounters":    encounters,
		"attachments":   attachments,
		"maskedSSN":     maskedSSN,
		"targetUser":    targetUserID,
		"serviceErrors": serviceErrors,
	})
}

func (h *HealthHandler) UpdateHealthRecord(c *gin.Context) {
	user := GetCurrentUser(c)
	targetUserID := user.ID
	if id, err := strconv.ParseUint(c.PostForm("user_id"), 10, 64); err == nil {
		if !h.enforceScopeForHealth(c, uint(id)) {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied: cannot modify this record"})
			return
		}
		targetUserID = uint(id)
	}

	// Validate text field lengths (max 10KB each)
	const maxTextLen = 10240
	for _, field := range []string{"allergies", "conditions", "medications", "reason"} {
		if len(c.PostForm(field)) > maxTextLen {
			c.JSON(http.StatusBadRequest, gin.H{"error": field + " exceeds maximum length (10KB)"})
			return
		}
	}

	_, err := h.HealthSvc.UpsertHealthRecord(
		targetUserID,
		user.ID,
		c.PostForm("allergies"),
		c.PostForm("conditions"),
		c.PostForm("medications"),
		c.PostForm("blood_type"),
		c.PostForm("reason"),
	)

	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error()})
		return
	}

	c.Redirect(http.StatusFound, "/dashboard")
}

func (h *HealthHandler) RecordVitals(c *gin.Context) {
	user := GetCurrentUser(c)
	targetUserID, _ := strconv.ParseUint(c.PostForm("user_id"), 10, 64)

	// Enforce data scope — clinicians can only record vitals for patients in their department
	if !h.enforceScopeForHealth(c, uint(targetUserID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied: cannot record vitals for this patient"})
		return
	}
	_ = user

	weightLb, _ := strconv.ParseFloat(c.PostForm("weight_lb"), 64)
	bpSys, _ := strconv.Atoi(c.PostForm("bp_systolic"))
	bpDia, _ := strconv.Atoi(c.PostForm("bp_diastolic"))
	tempF, _ := strconv.ParseFloat(c.PostForm("temperature_f"), 64)
	hr, _ := strconv.Atoi(c.PostForm("heart_rate"))

	// Validate vitals ranges
	var validationErrors []string
	if weightLb != 0 && (weightLb < 1 || weightLb > 1000) {
		validationErrors = append(validationErrors, "Weight must be between 1 and 1000 lb")
	}
	if bpSys != 0 && (bpSys < 40 || bpSys > 300) {
		validationErrors = append(validationErrors, "Systolic BP must be between 40 and 300")
	}
	if bpDia != 0 && (bpDia < 20 || bpDia > 200) {
		validationErrors = append(validationErrors, "Diastolic BP must be between 20 and 200")
	}
	if bpSys != 0 && bpDia != 0 && bpDia >= bpSys {
		validationErrors = append(validationErrors, "Diastolic BP must be less than systolic BP")
	}
	if tempF != 0 && (tempF < 85 || tempF > 115) {
		validationErrors = append(validationErrors, "Temperature must be between 85 and 115 °F")
	}
	if hr != 0 && (hr < 20 || hr > 300) {
		validationErrors = append(validationErrors, "Heart rate must be between 20 and 300 bpm")
	}
	if len(validationErrors) > 0 {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"error": "Validation errors: " + validationErrors[0]})
		return
	}

	vital := &models.Vital{
		UserID:       uint(targetUserID),
		WeightLb:     weightLb,
		BPSystolic:   bpSys,
		BPDiastolic:  bpDia,
		TemperatureF: tempF,
		HeartRate:    hr,
		RecordedBy:   user.ID,
	}

	if err := h.HealthSvc.RecordVitals(vital, c.PostForm("reason")); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error()})
		return
	}

	c.Redirect(http.StatusFound, "/dashboard?user_id="+strconv.FormatUint(targetUserID, 10))
}

func (h *HealthHandler) UploadAttachment(c *gin.Context) {
	user := GetCurrentUser(c)
	targetUserID := user.ID
	if id, err := strconv.ParseUint(c.PostForm("user_id"), 10, 64); err == nil {
		if !h.enforceScopeForHealth(c, uint(id)) {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied: cannot upload to another user's record"})
			return
		}
		targetUserID = uint(id)
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no file provided"})
		return
	}

	// Server-side validation (client-side also validates)
	if file.Size > 10*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file exceeds 10MB limit"})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file"})
		return
	}
	defer src.Close()

	// Read first 512 bytes for magic byte detection
	header := make([]byte, 512)
	n, _ := src.Read(header)
	detectedType := http.DetectContentType(header[:n])

	// Reset reader to beginning
	src.Close()
	src, err = file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file"})
		return
	}
	defer src.Close()

	allowed := map[string]bool{
		"application/pdf": true,
		"image/jpeg":      true,
		"image/png":       true,
		"image/gif":       true,
	}
	// Validate both the declared and detected content types
	declaredType := file.Header.Get("Content-Type")
	if !allowed[declaredType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file type not allowed. Only PDF, JPEG, PNG, GIF accepted"})
		return
	}
	if !allowed[detectedType] && detectedType != "application/octet-stream" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file content does not match allowed types (detected: " + detectedType + ")"})
		return
	}

	attachment, err := h.HealthSvc.SaveAttachment(file, src, targetUserID, user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "File uploaded successfully",
		"attachment": attachment,
	})
}

func (h *HealthHandler) DownloadAttachment(c *gin.Context) {
	user := GetCurrentUser(c)
	attachmentID, _ := strconv.ParseUint(c.Param("id"), 10, 64)

	var attachment models.Attachment
	if err := h.HealthSvc.DB.First(&attachment, uint(attachmentID)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "attachment not found"})
		return
	}

	// Verify the user has access to this attachment
	if !h.enforceScopeForHealth(c, attachment.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}
	_ = user

	// Sanitize filename to prevent header injection
	safeName := strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r == '\n' || r == '\r' || r == '/' {
			return '_'
		}
		return r
	}, attachment.FileName)
	c.Header("Content-Disposition", "attachment; filename=\""+safeName+"\"")
	c.Header("Content-Type", attachment.ContentType)
	c.File(attachment.FilePath)
}

// Clinician views
func (h *HealthHandler) ClinicianPage(c *gin.Context) {
	user := GetCurrentUser(c)
	dept := models.Department(c.DefaultQuery("dept", "general"))

	encounters, _ := h.HealthSvc.GetEncountersByDept(dept)

	c.HTML(http.StatusOK, "clinician.html", gin.H{
		"title":      "Clinician Dashboard",
		"user":       user,
		"encounters": encounters,
		"activeDept": dept,
	})
}

func (h *HealthHandler) CreateEncounter(c *gin.Context) {
	user := GetCurrentUser(c)
	patientID, _ := strconv.ParseUint(c.PostForm("patient_id"), 10, 64)

	// Enforce data scope
	if !h.enforceScopeForHealth(c, uint(patientID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied: cannot create encounter for this patient"})
		return
	}

	enc := &models.Encounter{
		UserID:         uint(patientID),
		ClinicianID:    user.ID,
		Department:     models.Department(c.PostForm("department")),
		ChiefComplaint: c.PostForm("chief_complaint"),
		Notes:          c.PostForm("notes"),
		Diagnosis:      c.PostForm("diagnosis"),
		Treatment:      c.PostForm("treatment"),
		EncounterDate:  time.Now(),
	}

	if err := h.HealthSvc.CreateEncounter(enc, "New encounter recorded"); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error()})
		return
	}

	c.Redirect(http.StatusFound, "/clinician?dept="+string(enc.Department))
}

func (h *HealthHandler) RecordHistory(c *gin.Context) {
	user := GetCurrentUser(c)
	table := c.Query("table")
	recordID, _ := strconv.ParseUint(c.Query("record_id"), 10, 64)

	// Allowlist of tables non-admin users can query
	allowedTables := map[string]bool{
		"health_records": true,
		"vitals":         true,
		"encounters":     true,
	}

	// Only admins and clinicians can view audit history; others restricted to own records
	switch user.Role {
	case models.RoleStudent, models.RoleFaculty:
		if !allowedTables[table] {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			return
		}
		// Verify the record belongs to this user
		switch table {
		case "health_records":
			var rec models.HealthRecord
			if err := h.HealthSvc.DB.First(&rec, recordID).Error; err != nil || rec.UserID != user.ID {
				c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
				return
			}
		case "encounters":
			var enc models.Encounter
			if err := h.HealthSvc.DB.First(&enc, recordID).Error; err != nil || enc.UserID != user.ID {
				c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
				return
			}
		case "vitals":
			var v models.Vital
			if err := h.HealthSvc.DB.First(&v, recordID).Error; err != nil || v.UserID != user.ID {
				c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
				return
			}
		}
	case models.RoleClinician, models.RoleStaff:
		if !allowedTables[table] && user.Role != models.RoleStaff {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			return
		}
	// Admin can see everything
	}

	logs, err := h.AuditSvc.GetHistory(table, uint(recordID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, logs)
}
