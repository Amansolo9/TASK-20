package services

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"time"

	"campus-portal/internal/models"

	"gorm.io/gorm"
)

type HealthService struct {
	DB        *gorm.DB
	Audit     *AuditService
	UploadDir string
}

func NewHealthService(db *gorm.DB, audit *AuditService, uploadDir string) *HealthService {
	return &HealthService{DB: db, Audit: audit, UploadDir: uploadDir}
}

// Health Records
func (s *HealthService) GetHealthRecord(userID uint) (*models.HealthRecord, error) {
	var record models.HealthRecord
	err := s.DB.Where("user_id = ?", userID).First(&record).Error
	if err != nil {
		return nil, err
	}
	// Decrypt sensitive fields for display
	var decErr error
	record.Allergies, decErr = DecryptField(record.Allergies)
	if decErr != nil && record.Allergies != "" {
		log.Printf("DECRYPT_ERROR: health_record=%d field=allergies: %v", record.ID, decErr)
		record.Allergies = "[encrypted data - decryption failed]"
	}
	record.Conditions, decErr = DecryptField(record.Conditions)
	if decErr != nil && record.Conditions != "" {
		log.Printf("DECRYPT_ERROR: health_record=%d field=conditions: %v", record.ID, decErr)
		record.Conditions = "[encrypted data - decryption failed]"
	}
	record.Medications, decErr = DecryptField(record.Medications)
	if decErr != nil && record.Medications != "" {
		log.Printf("DECRYPT_ERROR: health_record=%d field=medications: %v", record.ID, decErr)
		record.Medications = "[encrypted data - decryption failed]"
	}
	return &record, nil
}

func (s *HealthService) UpsertHealthRecord(userID uint, editorID uint, allergies, conditions, medications, bloodType, reason string) (*models.HealthRecord, error) {
	var record models.HealthRecord
	err := s.DB.Where("user_id = ?", userID).First(&record).Error

	// Encrypt sensitive fields before storage
	encAllergies, _ := EncryptField(allergies)
	encConditions, _ := EncryptField(conditions)
	encMedications, _ := EncryptField(medications)

	if err == gorm.ErrRecordNotFound {
		record = models.HealthRecord{
			UserID:      userID,
			Allergies:   encAllergies,
			Conditions:  encConditions,
			Medications: encMedications,
			BloodType:   bloodType,
		}
		if err := s.DB.Create(&record).Error; err != nil {
			return nil, err
		}
		s.Audit.LogChange("health_records", record.ID, "create", editorID, reason, record)
	} else if err == nil {
		record.Allergies = encAllergies
		record.Conditions = encConditions
		record.Medications = encMedications
		record.BloodType = bloodType
		if err := s.DB.Save(&record).Error; err != nil {
			return nil, err
		}
		s.Audit.LogChange("health_records", record.ID, "update", editorID, reason, record)
	} else {
		return nil, err
	}

	return &record, nil
}

// Vitals
func (s *HealthService) GetRecentVitals(userID uint, limit int) ([]models.Vital, error) {
	var vitals []models.Vital
	err := s.DB.Where("user_id = ?", userID).
		Order("recorded_at DESC").Limit(limit).Find(&vitals).Error
	return vitals, err
}

func (s *HealthService) RecordVitals(vital *models.Vital, reason string) error {
	vital.RecordedAt = time.Now()
	if err := s.DB.Create(vital).Error; err != nil {
		return err
	}
	s.Audit.LogChange("vitals", vital.ID, "create", vital.RecordedBy, reason, vital)
	return nil
}

// Encounters
func (s *HealthService) CreateEncounter(enc *models.Encounter, reason string) error {
	if err := s.DB.Create(enc).Error; err != nil {
		return err
	}
	s.Audit.LogChange("encounters", enc.ID, "create", enc.ClinicianID, reason, enc)
	return nil
}

func (s *HealthService) UpdateEncounter(enc *models.Encounter, editorID uint, reason string) error {
	if err := s.DB.Save(enc).Error; err != nil {
		return err
	}
	s.Audit.LogChange("encounters", enc.ID, "update", editorID, reason, enc)
	return nil
}

func (s *HealthService) GetEncounters(userID uint) ([]models.Encounter, error) {
	var encounters []models.Encounter
	err := s.DB.Where("user_id = ?", userID).Order("encounter_date DESC").Find(&encounters).Error
	return encounters, err
}

func (s *HealthService) GetEncountersByDept(dept models.Department, orgID uint) ([]models.Encounter, error) {
	var encounters []models.Encounter
	// Org-scoped: only return encounters where the clinician belongs to the caller's org
	err := s.DB.Joins("JOIN users u ON u.id = encounters.clinician_id AND u.organization_id = ?", orgID).
		Where("encounters.department = ?", dept).
		Order("encounters.encounter_date DESC").Find(&encounters).Error
	return encounters, err
}

// File Upload
func (s *HealthService) SaveAttachment(file *multipart.FileHeader, src multipart.File, userID, uploaderID uint) (*models.Attachment, error) {
	// Compute SHA-256
	hasher := sha256.New()
	tempBuf, err := io.ReadAll(src)
	if err != nil {
		return nil, err
	}
	hasher.Write(tempBuf)
	hash := fmt.Sprintf("%x", hasher.Sum(nil))

	// Save to disk
	filename := fmt.Sprintf("%d_%d_%s", userID, time.Now().UnixNano(), filepath.Base(file.Filename))
	destPath := filepath.Join(s.UploadDir, filename)

	if err := os.MkdirAll(s.UploadDir, 0755); err != nil {
		return nil, err
	}

	dst, err := os.Create(destPath)
	if err != nil {
		return nil, err
	}
	defer dst.Close()

	if _, err := dst.Write(tempBuf); err != nil {
		return nil, err
	}

	attachment := &models.Attachment{
		UserID:      userID,
		FileName:    file.Filename,
		FilePath:    destPath,
		FileSize:    file.Size,
		ContentType: file.Header.Get("Content-Type"),
		SHA256:      hash,
		UploadedBy:  uploaderID,
	}

	if err := s.DB.Create(attachment).Error; err != nil {
		return nil, err
	}

	return attachment, nil
}

func (s *HealthService) GetAttachments(userID uint) ([]models.Attachment, error) {
	var attachments []models.Attachment
	err := s.DB.Where("user_id = ?", userID).Order("created_at DESC").Find(&attachments).Error
	return attachments, err
}
