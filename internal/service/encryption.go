package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/hkdf"
)

// DecryptionHint contains the information a client needs to decrypt an encrypted answer.
type DecryptionHint struct {
	Cipher   string `json:"cipher"`
	Key      string `json:"key,omitempty"`
	Encoding string `json:"encoding"`
	Hint     string `json:"hint"`
}

// EncryptionService handles encryption of trait answers and password hashing.
type EncryptionService interface {
	Encrypt(plaintext, cipherType, keyHex string) (string, error)
	Decrypt(ciphertext, cipherType, keyHex string) (string, error)
	GetCipherInfo(cipherType, keyHex string) (DecryptionHint, error)
	// DeriveWitnessKey derives a 32-byte key (returned as 64-char hex) from the
	// session's unencrypted trait answers using HKDF-SHA256.
	// traitAnswers: map of traitKey to answer for all UNENCRYPTED questions asked so far.
	// sessionID: used as the HKDF salt.
	DeriveWitnessKey(sessionID string, traitAnswers map[string]string) (string, error)
	HashPassword(password string) (string, error)
	CheckPasswordHash(password, hash string) bool
}

type encryptionService struct{}

// NewEncryptionService returns a new EncryptionService.
func NewEncryptionService() EncryptionService {
	return &encryptionService{}
}

// Encrypt encrypts plaintext using the specified cipher and hex-encoded key.
func (s *encryptionService) Encrypt(plaintext, cipherType, keyHex string) (string, error) {
	switch cipherType {
	case "caesar":
		shift, err := caesarShift(keyHex)
		if err != nil {
			return "", err
		}
		return encryptCaesar(plaintext, shift), nil
	case "vigenere":
		keyword, err := deriveKeyword(keyHex)
		if err != nil {
			return "", err
		}
		return encryptVigenere(plaintext, keyword), nil
	case "xor":
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			return "", fmt.Errorf("invalid key: %w", err)
		}
		if len(key) == 0 {
			return "", errors.New("xor key must not be empty")
		}
		return encryptXOR(plaintext, key), nil
	case "aes-128-gcm":
		return encryptAES128GCM(plaintext, keyHex)
	case "aes-256-cbc":
		return encryptAES256CBC(plaintext, keyHex)
	default:
		return "", fmt.Errorf("unsupported cipher: %s", cipherType)
	}
}

// Decrypt decrypts ciphertext using the specified cipher and hex-encoded key.
func (s *encryptionService) Decrypt(ciphertext, cipherType, keyHex string) (string, error) {
	switch cipherType {
	case "caesar":
		shift, err := caesarShift(keyHex)
		if err != nil {
			return "", err
		}
		return encryptCaesar(ciphertext, 26-shift), nil
	case "vigenere":
		keyword, err := deriveKeyword(keyHex)
		if err != nil {
			return "", err
		}
		return decryptVigenere(ciphertext, keyword), nil
	case "xor":
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			return "", fmt.Errorf("invalid key: %w", err)
		}
		if len(key) == 0 {
			return "", errors.New("xor key must not be empty")
		}
		return decryptXOR(ciphertext, key)
	case "aes-128-gcm":
		return decryptAES128GCM(ciphertext, keyHex)
	case "aes-256-cbc":
		return decryptAES256CBC(ciphertext, keyHex)
	default:
		return "", fmt.Errorf("unsupported cipher: %s", cipherType)
	}
}

// GetCipherInfo returns the decryption hint for the given cipher and key.
func (s *encryptionService) GetCipherInfo(cipherType, keyHex string) (DecryptionHint, error) {
	switch cipherType {
	case "caesar":
		shift, err := caesarShift(keyHex)
		if err != nil {
			return DecryptionHint{}, fmt.Errorf("caesar key error: %w", err)
		}
		return DecryptionHint{
			Cipher:   "caesar",
			Key:      fmt.Sprintf("%d", shift),
			Encoding: "plaintext",
			Hint:     "Caesar cipher; shift each letter back by the key value",
		}, nil
	case "vigenere":
		keyword, err := deriveKeyword(keyHex)
		if err != nil {
			return DecryptionHint{}, fmt.Errorf("vigenere key error: %w", err)
		}
		return DecryptionHint{
			Cipher:   "vigenere",
			Key:      keyword,
			Encoding: "plaintext",
			Hint:     "Vigenere cipher; use the key to reverse the substitution",
		}, nil
	case "xor":
		return DecryptionHint{
			Cipher:   "xor",
			Key:      keyHex,
			Encoding: "hex",
			Hint:     "XOR cipher; hex-decode the ciphertext, XOR with key bytes (cycling)",
		}, nil
	case "aes-128-gcm":
		if len(keyHex) < 32 {
			return DecryptionHint{}, errors.New("aes-128-gcm key must be at least 16 bytes (32 hex chars)")
		}
		return DecryptionHint{
			Cipher:   "aes-128-gcm",
			Key:      keyHex[:32],
			Encoding: "hex",
			Hint:     "AES-128-GCM; first 12 bytes of hex output are the nonce",
		}, nil
	case "aes-256-cbc":
		return DecryptionHint{
			Cipher:   "aes-256-cbc",
			Key:      keyHex,
			Encoding: "hex",
			Hint:     "AES-256-CBC with PKCS#7 padding; first 16 bytes of hex output are the IV",
		}, nil
	default:
		return DecryptionHint{}, fmt.Errorf("unsupported cipher: %s", cipherType)
	}
}

// DeriveWitnessKey derives a 32-byte key (returned as 64-char hex) from the
// session's unencrypted trait answers using HKDF-SHA256.
func (s *encryptionService) DeriveWitnessKey(sessionID string, traitAnswers map[string]string) (string, error) {
	pairs := make([]string, 0, len(traitAnswers))
	for k, v := range traitAnswers {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	ikm := strings.Join(pairs, "\n")

	r := hkdf.New(sha256.New, []byte(ikm), []byte(sessionID), []byte("guesswho-witness-v1"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return "", fmt.Errorf("hkdf read: %w", err)
	}
	return hex.EncodeToString(key), nil
}

// --- caesar ---

// caesarShift derives a shift value (1-25) from the hex key.
func caesarShift(keyHex string) (int, error) {
	if len(keyHex) < 2 {
		return 0, errors.New("caesar key must be at least 1 byte (2 hex chars)")
	}
	b, err := hex.DecodeString(keyHex[:2])
	if err != nil || len(b) == 0 {
		return 0, fmt.Errorf("invalid caesar key: %w", err)
	}
	return int(b[0])%25 + 1, nil // range 1-25
}

func encryptCaesar(plaintext string, shift int) string {
	var sb strings.Builder
	for _, ch := range plaintext {
		switch {
		case ch >= 'a' && ch <= 'z':
			sb.WriteRune('a' + (ch-'a'+rune(shift))%26)
		case ch >= 'A' && ch <= 'Z':
			sb.WriteRune('A' + (ch-'A'+rune(shift))%26)
		default:
			sb.WriteRune(ch)
		}
	}
	return sb.String()
}

// --- vigenere ---

// deriveKeyword converts the hex key into a lowercase alphabetic keyword.
func deriveKeyword(keyHex string) (string, error) {
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", fmt.Errorf("invalid vigenere key: %w", err)
	}
	if len(keyBytes) == 0 {
		return "", errors.New("vigenere key must not be empty")
	}
	keyword := make([]byte, len(keyBytes))
	for i, b := range keyBytes {
		keyword[i] = 'a' + b%26
	}
	return string(keyword), nil
}

func encryptVigenere(plaintext, keyword string) string {
	if len(keyword) == 0 {
		return plaintext
	}
	keyRunes := []rune(strings.ToLower(keyword))
	var sb strings.Builder
	ki := 0
	for _, ch := range plaintext {
		shift := keyRunes[ki%len(keyRunes)] - 'a'
		switch {
		case ch >= 'a' && ch <= 'z':
			sb.WriteRune('a' + (ch-'a'+shift)%26)
			ki++
		case ch >= 'A' && ch <= 'Z':
			sb.WriteRune('A' + (ch-'A'+shift)%26)
			ki++
		default:
			sb.WriteRune(ch)
		}
	}
	return sb.String()
}

func decryptVigenere(ciphertext, keyword string) string {
	if len(keyword) == 0 {
		return ciphertext
	}
	keyRunes := []rune(strings.ToLower(keyword))
	var sb strings.Builder
	ki := 0
	for _, ch := range ciphertext {
		shift := keyRunes[ki%len(keyRunes)] - 'a'
		switch {
		case ch >= 'a' && ch <= 'z':
			sb.WriteRune('a' + (ch-'a'-shift+26)%26)
			ki++
		case ch >= 'A' && ch <= 'Z':
			sb.WriteRune('A' + (ch-'A'-shift+26)%26)
			ki++
		default:
			sb.WriteRune(ch)
		}
	}
	return sb.String()
}

// --- xor ---

func encryptXOR(plaintext string, key []byte) string {
	data := []byte(plaintext)
	result := make([]byte, len(data))
	for i, b := range data {
		result[i] = b ^ key[i%len(key)]
	}
	return hex.EncodeToString(result)
}

func decryptXOR(ciphertext string, key []byte) (string, error) {
	data, err := hex.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("invalid hex ciphertext: %w", err)
	}
	result := make([]byte, len(data))
	for i, b := range data {
		result[i] = b ^ key[i%len(key)]
	}
	return string(result), nil
}

// --- aes-128-gcm ---

func encryptAES128GCM(plaintext, keyHex string) (string, error) {
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", fmt.Errorf("invalid key: %w", err)
	}
	if len(keyBytes) < 16 {
		return "", errors.New("aes-128-gcm key must be at least 16 bytes")
	}
	key := keyBytes[:16]

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm: %w", err)
	}

	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce generation: %w", err)
	}

	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	result := append(nonce, sealed...)
	return hex.EncodeToString(result), nil
}

func decryptAES128GCM(ciphertext, keyHex string) (string, error) {
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", fmt.Errorf("invalid key: %w", err)
	}
	if len(keyBytes) < 16 {
		return "", errors.New("aes-128-gcm key must be at least 16 bytes")
	}
	key := keyBytes[:16]

	data, err := hex.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("invalid hex ciphertext: %w", err)
	}
	if len(data) < 12 {
		return "", errors.New("ciphertext too short: missing nonce")
	}

	nonce := data[:12]
	ct := data[12:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("gcm open: %w", err)
	}
	return string(plaintext), nil
}

// --- aes-256-cbc ---

func encryptAES256CBC(plaintext, keyHex string) (string, error) {
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", fmt.Errorf("invalid key: %w", err)
	}
	if len(keyBytes) != 32 {
		return "", errors.New("aes-256-cbc key must be exactly 32 bytes")
	}

	padded := pkcs7Pad([]byte(plaintext), aes.BlockSize)

	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", fmt.Errorf("iv generation: %w", err)
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}

	ct := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ct, padded)

	result := append(iv, ct...)
	return hex.EncodeToString(result), nil
}

func decryptAES256CBC(ciphertext, keyHex string) (string, error) {
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", fmt.Errorf("invalid key: %w", err)
	}
	if len(keyBytes) != 32 {
		return "", errors.New("aes-256-cbc key must be exactly 32 bytes")
	}

	data, err := hex.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("invalid hex ciphertext: %w", err)
	}
	if len(data) < aes.BlockSize {
		return "", errors.New("ciphertext too short: missing IV")
	}

	iv := data[:aes.BlockSize]
	ct := data[aes.BlockSize:]

	if len(ct)%aes.BlockSize != 0 {
		return "", errors.New("ciphertext length is not a multiple of block size")
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(ct, ct)

	plaintext, err := pkcs7Unpad(ct)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// --- pkcs7 helpers ---

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padded := make([]byte, len(data)+padding)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	return padded
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("pkcs7: empty data")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > aes.BlockSize {
		return nil, errors.New("pkcs7: invalid padding size")
	}
	for i := len(data) - padding; i < len(data); i++ {
		if data[i] != byte(padding) {
			return nil, errors.New("pkcs7: invalid padding bytes")
		}
	}
	return data[:len(data)-padding], nil
}

// --- Password hashing ---

func (s *encryptionService) HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func (s *encryptionService) CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
