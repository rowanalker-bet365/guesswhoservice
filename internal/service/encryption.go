package service

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
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
	Encrypt(plaintext string, cipherType string, keyHex string) (string, error)
	Decrypt(ciphertext string, cipherType string, keyHex string) (string, error)
	GetCipherInfo(cipherType string, keyHex string) DecryptionHint
	HashPassword(password string) (string, error)
	CheckPasswordHash(password, hash string) bool
}

type encryptionService struct{}

func NewEncryptionService() EncryptionService {
	return &encryptionService{}
}

func (s *encryptionService) Encrypt(plaintext string, cipherType string, keyHex string) (string, error) {
	switch cipherType {
	case "base64":
		return encryptBase64(plaintext), nil
	case "hex":
		return encryptHex(plaintext), nil
	case "reverse":
		return encryptReverse(plaintext), nil
	case "caesar":
		shift := caesarShift(keyHex)
		return encryptCaesar(plaintext, shift), nil
	case "xor":
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			return "", fmt.Errorf("invalid key: %w", err)
		}
		return encryptXOR(plaintext, key), nil
	case "vigenere":
		keyword := deriveKeyword(keyHex)
		return encryptVigenere(plaintext, keyword), nil
	case "xor-base64":
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			return "", fmt.Errorf("invalid key: %w", err)
		}
		return encryptXORBase64(plaintext, key), nil
	default:
		return "", fmt.Errorf("unsupported cipher: %s", cipherType)
	}
}

func (s *encryptionService) Decrypt(ciphertext string, cipherType string, keyHex string) (string, error) {
	switch cipherType {
	case "base64":
		return decryptBase64(ciphertext)
	case "hex":
		return decryptHex(ciphertext)
	case "reverse":
		return encryptReverse(ciphertext), nil
	case "caesar":
		shift := caesarShift(keyHex)
		return encryptCaesar(ciphertext, 26-shift), nil
	case "xor":
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			return "", fmt.Errorf("invalid key: %w", err)
		}
		return decryptXOR(ciphertext, key)
	case "vigenere":
		keyword := deriveKeyword(keyHex)
		return decryptVigenere(ciphertext, keyword), nil
	case "xor-base64":
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			return "", fmt.Errorf("invalid key: %w", err)
		}
		return decryptXORBase64(ciphertext, key)
	default:
		return "", fmt.Errorf("unsupported cipher: %s", cipherType)
	}
}

func (s *encryptionService) GetCipherInfo(cipherType string, keyHex string) DecryptionHint {
	switch cipherType {
	case "base64":
		return DecryptionHint{
			Cipher:   "base64",
			Encoding: "base64",
			Hint:     "The answer has been base64 encoded. Decode it using standard base64 decoding (e.g., base64.b64decode in Python, atob in JavaScript, base64.StdEncoding.DecodeString in Go).",
		}
	case "hex":
		return DecryptionHint{
			Cipher:   "hex",
			Encoding: "hex",
			Hint:     "The answer has been converted to its hexadecimal byte representation. Decode each pair of hex characters back to bytes to recover the original string (e.g., bytes.fromhex in Python, hex.DecodeString in Go).",
		}
	case "reverse":
		return DecryptionHint{
			Cipher:   "reverse",
			Encoding: "text",
			Hint:     "The answer has been reversed character by character. Reverse the string to recover the original value.",
		}
	case "caesar":
		shift := caesarShift(keyHex)
		return DecryptionHint{
			Cipher:   "caesar",
			Key:      strconv.Itoa(shift),
			Encoding: "text",
			Hint:     fmt.Sprintf("Each letter (a-z, A-Z) has been shifted forward by %d positions in the alphabet, wrapping around from z to a. Shift each letter back by %d positions to decrypt. Non-letter characters are unchanged.", shift, shift),
		}
	case "xor":
		return DecryptionHint{
			Cipher:   "xor",
			Key:      keyHex,
			Encoding: "hex",
			Hint:     "The answer has been XOR'd byte-by-byte with the key (cycling the key). The result is hex-encoded. To decrypt: hex-decode the value, hex-decode the key, then XOR each byte of the value with the corresponding byte of the key (cycling).",
		}
	case "vigenere":
		keyword := deriveKeyword(keyHex)
		return DecryptionHint{
			Cipher:   "vigenere",
			Key:      keyword,
			Encoding: "text",
			Hint:     fmt.Sprintf("Each letter has been shifted using the Vigenère cipher with keyword '%s'. For each letter in the encrypted text, subtract the shift of the corresponding keyword letter (a=0, b=1, ..., z=25), cycling through the keyword. Non-letter characters are unchanged. To decrypt letter E with key letter K: plaintext = (E - K + 26) mod 26.", keyword),
		}
	case "xor-base64":
		return DecryptionHint{
			Cipher:   "xor-base64",
			Key:      keyHex,
			Encoding: "base64",
			Hint:     "The answer was first XOR'd byte-by-byte with the key (cycling), then the XOR'd bytes were base64-encoded. To decrypt: base64-decode the value to get raw bytes, hex-decode the key, then XOR each byte with the corresponding key byte (cycling) to recover the plaintext.",
		}
	default:
		return DecryptionHint{
			Cipher:   cipherType,
			Encoding: "unknown",
			Hint:     "Unknown cipher type.",
		}
	}
}

// --- base64 ---

func encryptBase64(plaintext string) string {
	return base64.StdEncoding.EncodeToString([]byte(plaintext))
}

// --- hex ---

func encryptHex(plaintext string) string {
	return hex.EncodeToString([]byte(plaintext))
}

// --- reverse ---

func encryptReverse(plaintext string) string {
	runes := []rune(plaintext)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// --- caesar ---

// caesarShift derives a shift value (1-25) from the hex key.
func caesarShift(keyHex string) int {
	if len(keyHex) == 0 {
		return 3 // default shift
	}
	// Use first byte of the key to determine shift
	b, err := hex.DecodeString(keyHex[:2])
	if err != nil || len(b) == 0 {
		return 3
	}
	return int(b[0])%25 + 1 // 1-25
}

func encryptCaesar(plaintext string, shift int) string {
	var b strings.Builder
	for _, ch := range plaintext {
		switch {
		case ch >= 'a' && ch <= 'z':
			b.WriteRune('a' + (ch-'a'+rune(shift))%26)
		case ch >= 'A' && ch <= 'Z':
			b.WriteRune('A' + (ch-'A'+rune(shift))%26)
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
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

// --- vigenere ---

// deriveKeyword converts the hex key into a lowercase alphabetic keyword.
func deriveKeyword(keyHex string) string {
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil || len(keyBytes) == 0 {
		return "secret"
	}
	keyword := make([]byte, len(keyBytes))
	for i, b := range keyBytes {
		keyword[i] = 'a' + b%26
	}
	return string(keyword)
}

func encryptVigenere(plaintext string, keyword string) string {
	if len(keyword) == 0 {
		return plaintext
	}
	keyRunes := []rune(strings.ToLower(keyword))
	var b strings.Builder
	ki := 0
	for _, ch := range plaintext {
		shift := keyRunes[ki%len(keyRunes)] - 'a'
		switch {
		case ch >= 'a' && ch <= 'z':
			b.WriteRune('a' + (ch-'a'+shift)%26)
			ki++
		case ch >= 'A' && ch <= 'Z':
			b.WriteRune('A' + (ch-'A'+shift)%26)
			ki++
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}

// --- xor-base64 ---

func encryptXORBase64(plaintext string, key []byte) string {
	data := []byte(plaintext)
	xored := make([]byte, len(data))
	for i, b := range data {
		xored[i] = b ^ key[i%len(key)]
	}
	return base64.StdEncoding.EncodeToString(xored)
}

// --- decrypt helpers ---

func decryptBase64(ciphertext string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("invalid base64: %w", err)
	}
	return string(decoded), nil
}

func decryptHex(ciphertext string) (string, error) {
	decoded, err := hex.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("invalid hex: %w", err)
	}
	return string(decoded), nil
}

func decryptXOR(ciphertext string, key []byte) (string, error) {
	data, err := hex.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("invalid hex: %w", err)
	}
	result := make([]byte, len(data))
	for i, b := range data {
		result[i] = b ^ key[i%len(key)]
	}
	return string(result), nil
}

func decryptVigenere(ciphertext string, keyword string) string {
	if len(keyword) == 0 {
		return ciphertext
	}
	keyRunes := []rune(strings.ToLower(keyword))
	var b strings.Builder
	ki := 0
	for _, ch := range ciphertext {
		shift := keyRunes[ki%len(keyRunes)] - 'a'
		switch {
		case ch >= 'a' && ch <= 'z':
			b.WriteRune('a' + (ch-'a'-shift+26)%26)
			ki++
		case ch >= 'A' && ch <= 'Z':
			b.WriteRune('A' + (ch-'A'-shift+26)%26)
			ki++
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func decryptXORBase64(ciphertext string, key []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("invalid base64: %w", err)
	}
	result := make([]byte, len(data))
	for i, b := range data {
		result[i] = b ^ key[i%len(key)]
	}
	return string(result), nil
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
