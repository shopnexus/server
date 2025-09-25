package biz

import (
	"encoding/base64"
	"fmt"

	"shopnexus-remastered/internal/utils/aes"
)

// EncryptCursor encrypts a string ID with AES-GCM and returns a base64 cursor.
func EncryptCursor(id, secret string) (string, error) {
	ciphertext, err := aes.Encrypt([]byte(secret), []byte(id))
	if err != nil {
		return "", fmt.Errorf("failed to encrypt cursor: %w", err)
	}

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptCursor decrypts a base64 cursor back to the original string.
func DecryptCursor(cursor, secret string) (string, error) {
	cipherText, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return "", fmt.Errorf("failed to decode cursor: %w", err)
	}

	plaintext, err := aes.Decrypt([]byte(secret), cipherText)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt cursor: %w", err)
	}

	return string(plaintext), nil
}
