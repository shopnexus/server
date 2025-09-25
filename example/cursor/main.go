package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

func main() {
	// Generate a random key
	key := generateKey()
	fmt.Printf("Key (hex): %x\n", key)

	// Original message
	message := "Hello, this is a secret message!"
	fmt.Printf("Original: %s\n", message)

	// Encrypt
	encrypted, err := encrypt([]byte(message), key)
	if err != nil {
		fmt.Printf("Encryption error: %v\n", err)
		return
	}
	fmt.Printf("Encrypted (hex): %x\n", encrypted)

	// Decrypt
	decrypted, err := decrypt(encrypted, key)
	if err != nil {
		fmt.Printf("Decryption error: %v\n", err)
		return
	}
	fmt.Printf("Decrypted: %s\n", string(decrypted))

	// Verify
	if string(decrypted) == message {
		fmt.Println("✓ Encryption/Decryption successful!")
	} else {
		fmt.Println("✗ Something went wrong!")
	}
}

// encrypt encrypts plaintext using AES-GCM
func encrypt(plaintext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// decrypt decrypts ciphertext using AES-GCM
func decrypt(ciphertext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// generateKey generates a random 32-byte key for AES-256
func generateKey() []byte {
	key := make([]byte, 32) // AES-256
	rand.Read(key)
	return key
}
