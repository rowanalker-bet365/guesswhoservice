package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/bcrypt"
)

// DecryptionHint contains the information a client needs to decrypt an encrypted answer.
type DecryptionHint struct {
	Cipher   string `json:"cipher"`
	Key      string `json:"key"`
	Encoding string `json:"encoding"`
	Hint     string `json:"hint"`
}

// EncryptionService handles encryption of trait answers and password hashing.
type EncryptionService interface {
	// Encrypt encrypts plaintext using the given cipher and hex-encoded key.
	// Returns the ciphertext as a hex-encoded string.
	Encrypt(plaintext string, cipherType string, keyHex string) (string, error)

	// GetCipherInfo returns decryption metadata so the client can decrypt on their own.
	GetCipherInfo(cipherType string, keyHex string) DecryptionHint

	// HashPassword and CheckPasswordHash remain for auth.
	HashPassword(password string) (string, error)
	CheckPasswordHash(password, hash string) bool
}

type encryptionService struct{}

// NewEncryptionService creates a new encryption service
func NewEncryptionService() EncryptionService {
	return &encryptionService{}
}

func (s *encryptionService) Encrypt(plaintext string, cipherType string, keyHex string) (string, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", fmt.Errorf("invalid key: %w", err)
	}

	switch cipherType {
	case "AES-256-GCM":
		return encryptAESGCM([]byte(plaintext), key)
	case "AES-256-CBC":
		return encryptAESCBC([]byte(plaintext), key)
	case "XOR":
		return encryptXOR([]byte(plaintext), key)
	default:
		return "", fmt.Errorf("unsupported cipher: %s", cipherType)
	}
}

func (s *encryptionService) GetCipherInfo(cipherType string, keyHex string) DecryptionHint {
	switch cipherType {
	case "AES-256-GCM":
		return DecryptionHint{
			Cipher:   "AES-256-GCM",
			Key:      keyHex,
			Encoding: "hex",
			Hint:     "The encrypted value is hex-encoded. Decode the hex to get raw bytes. The first 12 bytes are the nonce/IV. The remaining bytes are the ciphertext with a 16-byte authentication tag appended. Use AES-256-GCM with the provided key (hex-decode to 32 bytes) and the extracted nonce to decrypt.",
		}
	case "AES-256-CBC":
		return DecryptionHint{
			Cipher:   "AES-256-CBC",
			Key:      keyHex,
			Encoding: "hex",
			Hint:     "The encrypted value is hex-encoded. Decode the hex to get raw bytes. The first 16 bytes are the IV. The remaining bytes are the ciphertext encrypted with AES-256-CBC and PKCS7 padding. Use the provided key (hex-decode to 32 bytes) and the extracted IV to decrypt, then remove PKCS7 padding.",
		}
	case "XOR":
		return DecryptionHint{
			Cipher:   "XOR",
			Key:      keyHex,
			Encoding: "hex",
			Hint:     "The encrypted value is hex-encoded. Decode the hex to get raw bytes. XOR each byte with the corresponding byte of the key (cycling the key if plaintext is longer). The result is the plaintext string.",
		}
	default:
		return DecryptionHint{
			Cipher:   cipherType,
			Key:      keyHex,
			Encoding: "hex",
			Hint:     "Unknown cipher type.",
		}
	}
}

// --- AES-256-GCM ---

func encryptAESGCM(plaintext, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Seal appends the ciphertext+tag to the nonce
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return hex.EncodeToString(ciphertext), nil
}

// --- AES-256-CBC ---

func encryptAESCBC(plaintext, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// PKCS7 padding
	blockSize := block.BlockSize()
	padding := blockSize - len(plaintext)%blockSize
	padded := make([]byte, len(plaintext)+padding)
	copy(padded, plaintext)
	for i := len(plaintext); i < len(padded); i++ {
		padded[i] = byte(padding)
	}

	// Generate random IV
	iv := make([]byte, blockSize) // 16 bytes
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", fmt.Errorf("failed to generate IV: %w", err)
	}

	mode := cipher.NewCBCEncrypter(block, iv)
	ciphertext := make([]byte, len(padded))
	mode.CryptBlocks(ciphertext, padded)

	// Prepend IV to ciphertext
	result := append(iv, ciphertext...)
	return hex.EncodeToString(result), nil
}

// --- XOR ---

func encryptXOR(plaintext, key []byte) (string, error) {
	if len(key) == 0 {
		return "", fmt.Errorf("XOR key cannot be empty")
	}

	ciphertext := make([]byte, len(plaintext))
	for i, b := range plaintext {
		ciphertext[i] = b ^ key[i%len(key)]
	}
	return hex.EncodeToString(ciphertext), nil
}

// --- Password hashing (unchanged) ---

func (s *encryptionService) HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func (s *encryptionService) CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
