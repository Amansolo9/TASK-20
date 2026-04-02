package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"time"

	"campus-portal/internal/models"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthService struct {
	DB *gorm.DB
}

func NewAuthService(db *gorm.DB) *AuthService {
	return &AuthService{DB: db}
}

func (s *AuthService) Register(username, password, fullName, email string, role models.Role, orgID uint, deptID *uint) (*models.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &models.User{
		Username:       username,
		PasswordHash:   string(hash),
		FullName:       fullName,
		Email:          email,
		Role:           role,
		OrganizationID: orgID,
		DepartmentID:   deptID,
		Active:         true,
	}

	if err := s.DB.Create(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

func (s *AuthService) Login(username, password string) (*models.User, string, error) {
	var user models.User
	if err := s.DB.Where("username = ? AND active = true", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("AUTH_FAILURE: unknown username=%s", username)
			return nil, "", errors.New("invalid credentials")
		}
		return nil, "", err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		log.Printf("AUTH_FAILURE: invalid password for user=%s", username)
		return nil, "", errors.New("invalid credentials")
	}

	// Check for temporary elevated access
	var tempAccess models.TempAccess
	if err := s.DB.Where("user_id = ? AND reverted = false AND expires_at > ?", user.ID, time.Now()).
		Order("created_at DESC").First(&tempAccess).Error; err == nil {
		user.Role = tempAccess.GrantedRole
	}

	sessionID, err := s.CreateSession(user.ID)
	if err != nil {
		return nil, "", err
	}

	return &user, sessionID, nil
}

func (s *AuthService) CreateSession(userID uint) (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	sessionID := hex.EncodeToString(token)

	session := &models.Session{
		ID:        sessionID,
		UserID:    userID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := s.DB.Create(session).Error; err != nil {
		return "", err
	}
	return sessionID, nil
}

func (s *AuthService) ValidateSession(sessionID string) (*models.User, error) {
	var session models.Session
	if err := s.DB.Where("id = ? AND expires_at > ?", sessionID, time.Now()).First(&session).Error; err != nil {
		return nil, errors.New("invalid or expired session")
	}

	var user models.User
	if err := s.DB.First(&user, session.UserID).Error; err != nil {
		return nil, errors.New("user not found")
	}

	if !user.Active {
		return nil, errors.New("account deactivated")
	}

	// Apply temp access if active
	var tempAccess models.TempAccess
	if err := s.DB.Where("user_id = ? AND reverted = false AND expires_at > ?", user.ID, time.Now()).
		Order("created_at DESC").First(&tempAccess).Error; err == nil {
		user.Role = tempAccess.GrantedRole
	}

	return &user, nil
}

func (s *AuthService) Logout(sessionID string) error {
	return s.DB.Delete(&models.Session{}, "id = ?", sessionID).Error
}

// InvalidateUserSessions removes all active sessions for a user,
// forcing them to re-authenticate on next request.
func (s *AuthService) InvalidateUserSessions(userID uint) {
	s.DB.Where("user_id = ?", userID).Delete(&models.Session{})
}

func (s *AuthService) GrantTempAccess(userID uint, grantedRole models.Role, grantedBy uint, duration time.Duration) error {
	var user models.User
	if err := s.DB.First(&user, userID).Error; err != nil {
		return err
	}

	ta := &models.TempAccess{
		UserID:       userID,
		GrantedRole:  grantedRole,
		OriginalRole: user.Role,
		GrantedBy:    grantedBy,
		ExpiresAt:    time.Now().Add(duration),
	}
	return s.DB.Create(ta).Error
}

func (s *AuthService) RevertExpiredAccess() {
	var expired []models.TempAccess
	s.DB.Where("reverted = false AND expires_at <= ?", time.Now()).Find(&expired)

	for _, ta := range expired {
		s.DB.Model(&models.User{}).Where("id = ?", ta.UserID).Update("role", ta.OriginalRole)
		s.DB.Model(&ta).Update("reverted", true)
	}
}

// SetUserSSN encrypts the SSN before storing it.
func (s *AuthService) SetUserSSN(userID uint, ssn string) error {
	if ssn == "" {
		return s.DB.Model(&models.User{}).Where("id = ?", userID).Update("ssn", "").Error
	}
	// Lazy import to avoid circular dependency — use the services package EncryptField
	// SSN is encrypted with AES-256-GCM at rest
	encrypted, err := encryptSSN(ssn)
	if err != nil {
		return err
	}
	return s.DB.Model(&models.User{}).Where("id = ?", userID).Update("ssn", encrypted).Error
}

// encryptSSN wraps the AES-256-GCM encryption for SSN values.
func encryptSSN(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	// Use the same encryption key as field encryption
	// Re-implement here to avoid circular import with services package
	key := os.Getenv("FIELD_ENCRYPTION_KEY")
	if key == "" {
		return "", errors.New("FIELD_ENCRYPTION_KEY not set")
	}
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decoded) != 32 {
		return "", errors.New("invalid encryption key")
	}
	block, err := aes.NewCipher(decoded)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *AuthService) ChangeUserRole(userID uint, newRole models.Role) error {
	return s.DB.Model(&models.User{}).Where("id = ?", userID).Update("role", newRole).Error
}

func (s *AuthService) ToggleUserActive(userID uint, active bool) error {
	return s.DB.Model(&models.User{}).Where("id = ?", userID).Update("active", active).Error
}
