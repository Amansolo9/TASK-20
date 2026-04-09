package services

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"os"
)

// FieldEncryptor provides AES-256-GCM encryption for sensitive fields at rest.
var fieldEncryptionKey []byte

func InitEncryption() error {
	key := os.Getenv("FIELD_ENCRYPTION_KEY")
	if key == "" {
		ginMode := os.Getenv("GIN_MODE")
		if ginMode == "release" {
			return errors.New("FIELD_ENCRYPTION_KEY is required in production (GIN_MODE=release). Generate with: openssl rand -base64 32")
		}
		fieldEncryptionKey = []byte("DefaultDevKey!ChangeInProd!32b!")
		log.Println("WARNING: FIELD_ENCRYPTION_KEY not set — using built-in dev key. Set this env var for production.")
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return errors.New("FIELD_ENCRYPTION_KEY must be base64-encoded 32 bytes")
	}
	if len(decoded) != 32 {
		return errors.New("FIELD_ENCRYPTION_KEY must decode to exactly 32 bytes")
	}
	fieldEncryptionKey = decoded
	return nil
}

// EncryptField encrypts a plaintext string using AES-256-GCM.
// Returns a base64-encoded ciphertext. Empty input returns empty output.
func EncryptField(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(fieldEncryptionKey)
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

// DecryptField decrypts a base64-encoded AES-256-GCM ciphertext.
// Empty input returns empty output.
func DecryptField(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(fieldEncryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
