package services

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"campus-portal/internal/models"

	"gorm.io/gorm"
)

// SSOUser represents a user record from the on-prem SSO source.
type SSOUser struct {
	Username     string   `json:"username"`
	FullName     string   `json:"full_name"`
	Email        string   `json:"email"`
	Role         string   `json:"role"`
	Department   string   `json:"department"`
	Groups       []string `json:"groups"`
	Active       bool     `json:"active"`
	Organization string   `json:"organization"`
}

// SSOSource defines the interface for fetching users from an internal SSO directory.
type SSOSource interface {
	FetchUsers() ([]SSOUser, error)
}

// FileSSOSource reads SSO data from a JSON file on a shared network path.
type FileSSOSource struct {
	FilePath string
}

func (s *FileSSOSource) FetchUsers() ([]SSOUser, error) {
	data, err := os.ReadFile(s.FilePath)
	if err != nil {
		return nil, fmt.Errorf("sso source read error: %w", err)
	}
	var users []SSOUser
	if err := json.Unmarshal(data, &users); err != nil {
		return nil, fmt.Errorf("sso source parse error: %w", err)
	}
	return users, nil
}

// SSOSyncService synchronizes user and group data from an on-prem SSO source
// into the campus portal database. This is separate from CSV enrollment import.
type SSOSyncService struct {
	DB       *gorm.DB
	Source   SSOSource
	AuditSvc *AuditService
	done     chan struct{}
}

func NewSSOSyncService(db *gorm.DB, source SSOSource, auditSvc *AuditService) *SSOSyncService {
	return &SSOSyncService{
		DB:       db,
		Source:   source,
		AuditSvc: auditSvc,
		done:     make(chan struct{}),
	}
}

// Start begins periodic SSO synchronization at the given interval.
func (s *SSOSyncService) Start(interval time.Duration) {
	go func() {
		// Run once immediately on startup
		s.RunOnce()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.RunOnce()
			case <-s.done:
				return
			}
		}
	}()
	log.Printf("SSO Sync service started (interval: %v)", interval)
}

func (s *SSOSyncService) Stop() {
	close(s.done)
}

// RunOnce performs a single synchronization cycle.
func (s *SSOSyncService) RunOnce() {
	users, err := s.Source.FetchUsers()
	if err != nil {
		log.Printf("SSO sync: failed to fetch users: %v", err)
		return
	}

	var synced, created, updated, deactivated int
	for _, ssoUser := range users {
		if ssoUser.Username == "" || ssoUser.FullName == "" {
			continue
		}

		// Validate role
		validRoles := map[string]bool{
			"student": true, "faculty": true, "clinician": true, "staff": true, "admin": true,
		}
		if ssoUser.Role != "" && !validRoles[ssoUser.Role] {
			log.Printf("SSO sync: skipping user %s — invalid role %q", ssoUser.Username, ssoUser.Role)
			continue
		}
		if ssoUser.Role == "" {
			ssoUser.Role = "student"
		}

		// Resolve organization
		var orgID uint
		if ssoUser.Organization != "" {
			var org models.Organization
			if err := s.DB.Where("name = ?", ssoUser.Organization).First(&org).Error; err == nil {
				orgID = org.ID
			}
		}
		if orgID == 0 {
			var org models.Organization
			s.DB.First(&org)
			orgID = org.ID
		}

		// Resolve department
		var deptID *uint
		if ssoUser.Department != "" {
			var dept models.DepartmentRecord
			if err := s.DB.Where("name = ? AND organization_id = ?", ssoUser.Department, orgID).First(&dept).Error; err == nil {
				deptID = &dept.ID
			} else {
				// Auto-create department from SSO source
				dept = models.DepartmentRecord{Name: ssoUser.Department, OrganizationID: orgID}
				if err := s.DB.Create(&dept).Error; err == nil {
					deptID = &dept.ID
					s.AuditSvc.LogChange("departments", dept.ID, "sso_sync_created", 0,
						fmt.Sprintf("Department %s auto-created from SSO sync", ssoUser.Department), dept)
				}
			}
		}

		var existing models.User
		err := s.DB.Where("username = ?", ssoUser.Username).First(&existing).Error

		if err == gorm.ErrRecordNotFound {
			// Create new user from SSO
			newUser := models.User{
				Username:       ssoUser.Username,
				PasswordHash:   "$2a$10$placeholder", // Requires admin password reset or SSO login
				FullName:       ssoUser.FullName,
				Email:          ssoUser.Email,
				Role:           models.Role(ssoUser.Role),
				OrganizationID: orgID,
				DepartmentID:   deptID,
				Active:         ssoUser.Active,
			}
			if err := s.DB.Create(&newUser).Error; err != nil {
				log.Printf("SSO sync: failed to create user %s: %v", ssoUser.Username, err)
				continue
			}
			s.AuditSvc.LogChange("users", newUser.ID, "sso_sync_created", 0,
				fmt.Sprintf("User %s provisioned from SSO sync", ssoUser.Username), newUser)
			created++
		} else if err == nil {
			// Update existing user
			changes := []string{}
			if existing.FullName != ssoUser.FullName {
				changes = append(changes, fmt.Sprintf("name: %s -> %s", existing.FullName, ssoUser.FullName))
				existing.FullName = ssoUser.FullName
			}
			if ssoUser.Email != "" && existing.Email != ssoUser.Email {
				changes = append(changes, fmt.Sprintf("email: %s -> %s", existing.Email, ssoUser.Email))
				existing.Email = ssoUser.Email
			}
			if deptID != nil && (existing.DepartmentID == nil || *existing.DepartmentID != *deptID) {
				changes = append(changes, fmt.Sprintf("department_id -> %d", *deptID))
				existing.DepartmentID = deptID
			}
			if models.Role(ssoUser.Role) != existing.Role {
				changes = append(changes, fmt.Sprintf("role: %s -> %s", existing.Role, ssoUser.Role))
				existing.Role = models.Role(ssoUser.Role)
			}
			if existing.Active != ssoUser.Active {
				if ssoUser.Active {
					changes = append(changes, "reactivated")
				} else {
					changes = append(changes, "deactivated")
					deactivated++
				}
				existing.Active = ssoUser.Active
			}

			if len(changes) > 0 {
				s.DB.Save(&existing)
				s.AuditSvc.LogChange("users", existing.ID, "sso_sync_updated", 0,
					fmt.Sprintf("SSO sync: %s", strings.Join(changes, "; ")), existing)
				updated++
			}
		}
		synced++
	}

	log.Printf("SSO sync complete: %d processed, %d created, %d updated, %d deactivated",
		synced, created, updated, deactivated)
}
