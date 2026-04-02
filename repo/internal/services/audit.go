package services

import (
	"encoding/json"
	"fmt"
	"time"

	"campus-portal/internal/models"

	"gorm.io/gorm"
)

type AuditService struct {
	DB *gorm.DB
}

func NewAuditService(db *gorm.DB) *AuditService {
	return &AuditService{DB: db}
}

func (s *AuditService) LogChange(tableName string, recordID uint, action string, editorID uint, reason string, snapshot interface{}) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}

	fingerprint := models.ComputeFingerprint(fmt.Sprintf("%s:%d:%s:%d:%s", tableName, recordID, action, editorID, string(data)))

	entry := &models.AuditLog{
		TableName:   tableName,
		RecordID:    recordID,
		Action:      action,
		EditorID:    editorID,
		Reason:      reason,
		Fingerprint: fingerprint,
		Snapshot:    string(data),
		Timestamp:   time.Now(),
	}

	return s.DB.Create(entry).Error
}

func (s *AuditService) GetHistory(tableName string, recordID uint) ([]models.AuditLog, error) {
	var logs []models.AuditLog
	err := s.DB.Where("table_name = ? AND record_id = ?", tableName, recordID).
		Order("timestamp DESC").Find(&logs).Error
	return logs, err
}

// GORM hook helper for auto-auditing
type Auditable interface {
	GetTableName() string
	GetID() uint
}
