package services

import (
	"encoding/base64"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestEncryption(t *testing.T) {
	t.Helper()
	// Set a test encryption key (32 bytes base64-encoded)
	testKey := base64.StdEncoding.EncodeToString([]byte("01234567890abcdef01234567890abcd"))
	os.Setenv("FIELD_ENCRYPTION_KEY", testKey)
	require.NoError(t, InitEncryption())
}

func TestEncryptDecryptField(t *testing.T) {
	setupTestEncryption(t)

	tests := []struct {
		name      string
		plaintext string
	}{
		{"empty string", ""},
		{"simple text", "hello world"},
		{"SSN format", "123-45-6789"},
		{"unicode", "allergic to penicillin - severe reaction"},
		{"long text", "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := EncryptField(tt.plaintext)
			assert.NoError(t, err)

			if tt.plaintext == "" {
				assert.Empty(t, encrypted)
				return
			}

			// Encrypted should be different from plaintext
			assert.NotEqual(t, tt.plaintext, encrypted)

			// Decrypt should return original
			decrypted, err := DecryptField(encrypted)
			assert.NoError(t, err)
			assert.Equal(t, tt.plaintext, decrypted)
		})
	}
}

func TestEncryptField_DifferentCiphertextEachTime(t *testing.T) {
	setupTestEncryption(t)

	enc1, _ := EncryptField("same input")
	enc2, _ := EncryptField("same input")

	// Due to random nonce, each encryption should produce different output
	assert.NotEqual(t, enc1, enc2)

	// But both should decrypt to the same plaintext
	dec1, _ := DecryptField(enc1)
	dec2, _ := DecryptField(enc2)
	assert.Equal(t, dec1, dec2)
	assert.Equal(t, "same input", dec1)
}

func TestDecryptField_InvalidData(t *testing.T) {
	setupTestEncryption(t)

	_, err := DecryptField("not-valid-base64!!!")
	assert.Error(t, err)

	_, err = DecryptField("aGVsbG8=") // valid base64 but not valid ciphertext
	assert.Error(t, err)
}
