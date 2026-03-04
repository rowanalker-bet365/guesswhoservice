# Decryption Challenge Design: Two-Stage Encryption System

> **Living design document.** Stage 1 is fully implemented. Stage 2 is specified here for implementation.
> Last updated: March 2026

---

## Overview

The encryption challenge is a progressive difficulty system that activates as teams progress through the game. It operates in two stages, each with distinct mechanics and cipher pools.

In Stage 1, the decryption key is handed to players via the `/decode` endpoint — the challenge is knowing how to use it. In Stage 2, the key must be derived from their own game history using the Witness Key mechanism.

```
Start ──► Stage 1: Cipher Apprentice ──► [48 solves] ──► Stage 2: Cipher Master
           (key provided via /decode)                    (key derived via Witness Key)
```

---

## Cipher Selection

### Design Constraints

1. **Stage 1 (key provided):** Ciphers should be educational and manually implementable.
2. **Stage 2 (key derived):** Ciphers must have a key space large enough that brute-forcing is infeasible. The plaintext is always short (`"true"`, `"false"`, or `"P01"`-`"P64"`), so a small key space allows exhaustive search.

### Why the Previous Cipher Set Was Replaced

The previous set (`base64`, `hex`, `reverse`, `caesar`, `xor`, `vigenere`, `xor-base64`) was not designed with Stage 2 in mind:

| Old Cipher | Problem |
|------------|---------|
| `base64` | Encoding only. No key. Trivially reversible. Incompatible with Stage 2. |
| `hex` | Encoding only. No key. Trivially reversible. Incompatible with Stage 2. |
| `reverse` | No key. Trivially reversible. Incompatible with Stage 2. |
| `xor-base64` | Redundant with `xor` — same cipher, different output encoding. |
| `caesar` | Key space = 25 values. Brute-forceable in milliseconds against known plaintext. |

`caesar` and `vigenere` are retained for **Stage 1 only**. They are excluded from Stage 2 because their key spaces are too small to resist brute-force when the plaintext structure is known.

### Chosen Cipher Set

| # | Cipher | Stage | Key | Key Space | Output Encoding |
|---|--------|-------|-----|-----------|-----------------|
| 1 | `caesar` | Stage 1 only | Shift value (1-25) | 25 | Plaintext |
| 2 | `vigenere` | Stage 1 only | 8-char keyword | ~26^8 | Plaintext |
| 3 | `xor` | Both stages | 8-byte hex key | 2^64 | Hex |
| 4 | `aes-128-gcm` | Both stages | 16-byte key | 2^128 | Hex (nonce prepended) |
| 5 | `aes-256-cbc` | Both stages | 32-byte key | 2^256 | Hex (IV prepended) |

**Stage 1** randomly selects from all 5 ciphers per session.
**Stage 2** randomly selects from `xor`, `aes-128-gcm`, and `aes-256-cbc` only.

> **Implementation note:** The cipher pool in [`domain.NewSession()`](../internal/domain/session.go:54) must be updated. The `EncryptKey` field must be extended from 8 bytes to 32 bytes to accommodate AES-256-CBC. A new `Stage int` field must be added to [`domain.Session`](../internal/domain/session.go:25) and set at session start based on `teamData.Solves`.

---

## Cipher Reference

### `caesar`

Each letter (a-z, A-Z) is shifted forward by the key value. Non-letter characters are unchanged.

- **Decrypt:** shift each letter back by the key amount
- **Key format:** integer 1-25 (returned as string in `/decode` response)
- **Example:** `"true"` with shift 3 → `"wuxh"`

### `vigenere`

Polyalphabetic substitution. Each letter is shifted by the corresponding letter of the keyword (cycling). Non-letter characters are unchanged.

- **Decrypt:** for each letter, subtract the shift of the corresponding keyword letter (a=0, b=1, ..., z=25), mod 26
- **Key format:** lowercase alphabetic string, 8 chars (returned in `/decode` response)

### `xor`

Each byte of the plaintext is XOR'd with the corresponding byte of the key (cycling). Output is hex-encoded.

- **Decrypt:** `hex_decode(ciphertext)`, then XOR each byte with `key[i % len(key)]`
- **Key format:** 16-char hex string (8 bytes)

```python
key = bytes.fromhex(key_hex)
data = bytes.fromhex(ciphertext_hex)
plaintext = bytes([b ^ key[i % len(key)] for i, b in enumerate(data)]).decode()
```

### `aes-128-gcm`

AES in Galois/Counter Mode with a 128-bit key. Authenticated encryption — a wrong key produces an authentication error, not garbage plaintext. This makes it ideal for Stage 2: if decryption succeeds without error, the derived key is correct.

- **Ciphertext format (hex-encoded):** `[12-byte nonce][ciphertext + 16-byte auth tag]`
- **Key:** 16 bytes
- **Decrypt:** `nonce = decoded[:12]`, `AES-128-GCM.decrypt(key, nonce, decoded[12:])`
- **Library support:** `crypto/aes` + `crypto/cipher` (Go), `cryptography.hazmat` (Python), `crypto.subtle` (Web Crypto API)

### `aes-256-cbc`

AES in Cipher Block Chaining mode with a 256-bit key. PKCS7 padding applied before encryption.

- **Ciphertext format (hex-encoded):** `[16-byte IV][ciphertext]`
- **Key:** 32 bytes
- **Decrypt:** `iv = decoded[:16]`, `AES-256-CBC.decrypt(key, iv, decoded[16:])`, strip PKCS7 padding
- **Library support:** same as AES-128-GCM above

---

## Stage 1: Cipher Apprentice

### Activation

Active from the start of the game. The **first ask in a session is always exempt** from blocking and encryption.

### Ask Endpoint Mechanics

`POST /sessions/{sessionId}/ask`

Always returns a **pure boolean** response. For enum traits, players must specify the value they are asking about (e.g. "is their hair brown?"). Omitting `value` for an enum trait returns `400`.

**Probability flow (applied in order, after the first ask):**

```
Roll 1: Block? (25% chance)
  YES -> Blocked response. Question NOT recorded. Does not count toward question total.
  NO  -> Record question. Reveal trait on board. Roll 2.

Roll 2: Encrypt? (40% chance)
  YES -> Encrypt boolean answer with session cipher. Return ciphertext + cipherType.
  NO  -> Return plain boolean answer.
```

Net probabilities: **25%** blocked | **30%** encrypted | **45%** plain

### Ask Request

```json
{
  "questionId": "T07",
  "value": "brown"
}
```

`value` is required for `enum`/`numeric` traits, ignored for `boolean` traits.

### Ask Responses ([`domain.TraitAnswer`](../internal/domain/trait.go:33))

**Blocked:**
```json
{
  "questionId": "T07",
  "traitKey": "hair_color",
  "blocked": true,
  "message": "This trait is classified. You are not permitted to know this value."
}
```

**Encrypted:**
```json
{
  "questionId": "T07",
  "traitKey": "hair_color",
  "encrypted": true,
  "ciphertext": "a3f9c2d1...",
  "cipherType": "xor"
}
```

**Plain:**
```json
{
  "questionId": "T07",
  "traitKey": "hair_color",
  "encrypted": false,
  "answer": true
}
```

### Decode Endpoint — Stage 1

`POST /sessions/{sessionId}/decode`

Verifies the question was encrypted in this session (via [`session.EncryptedQuestions`](../internal/domain/session.go:48)), then returns the **full cipher info including the key**.

**Request** ([`handler.DecodeRequest`](../internal/handler/trait_handler.go:60)):
```json
{
  "questionId": "T07",
  "encrypted": "a3f9c2d1..."
}
```

**Response** ([`service.DecryptionHint`](../internal/service/encryption.go:14) via [`handler.DecodeResponse`](../internal/handler/trait_handler.go:66)):
```json
{
  "cipher": "xor",
  "key": "a1b2c3d4e5f6a7b8",
  "encoding": "hex",
  "hint": "The answer has been XOR'd byte-by-byte with the key (cycling). Hex-decode both the ciphertext and key, then XOR each byte."
}
```

The `key` field is always present in Stage 1. Players use the key + cipher type to manually decrypt the ciphertext.

**Error responses:**

| Code | Error | Condition |
|------|-------|-----------|
| `400` | `missing_question_id` | `questionId` field absent |
| `400` | `missing_encrypted_value` | `encrypted` field absent |
| `400` | `not_encrypted` | Question was not encrypted in this session |
| `404` | `session_not_found` | Session ID does not exist or has expired |

### M5 Milestone

First successful call to `/decode` awards **M5** ("Encrypted Answer Handled", 1000 pts). Awarded once per team, idempotent. Triggered in [`handler.TraitHandler.Decode()`](../internal/handler/trait_handler.go:133).

### Stage 1 Ends When

The team reaches **48 total character solves**. All subsequent sessions enter Stage 2.

---

## Stage 2: Cipher Master

> **Status: Not yet implemented.** This section is the specification for Stage 2 implementation.

### Activation

When a team's total solve count reaches **48**, all subsequent sessions enter Stage 2. The stage is determined at session start by reading `teamData.Solves` from Redis. A new `Stage int` field on [`domain.Session`](../internal/domain/session.go:25) stores the value (1 or 2) for the lifetime of the session.

### Ask Endpoint Mechanics — Stage 2

- **0% blocking**: Trait blocking is lifted entirely.
- **40% encryption**: Same probability as Stage 1.
- **60% plain boolean**: Normal response.

The cipher pool is restricted to `xor`, `aes-128-gcm`, and `aes-256-cbc`.

### Fake ID Guarantee

The real character ID (e.g., `C34`) is **never** exposed to players in any response. All guess responses and decryption challenges use the session-scoped fake ID (e.g., `P34`). The fake ID is the value players must decrypt and submit to `/decrypt`.

### Guess Response Encryption — Stage 2

There is a **50% chance** that any guess response (correct **or** incorrect) is returned with an encrypted `characterId`. The encryption roll applies regardless of whether the guess was right or wrong.

| Guess outcome | Encrypted plaintext |
|---------------|---------------------|
| Correct | The fake candidate ID (e.g., `"P34"`) |
| Incorrect | The fixed string `"n/a"` |

**Normal guess response (Stage 1 or unencrypted Stage 2):**
```json
{
  "correct": true,
  "characterId": "P34",
  "score": {
    "base": 1000,
    "timeBonus": 300,
    "questionBonus": 200,
    "reliabilityPenalty": 0,
    "total": 1500
  }
}
```

**Encrypted guess response (Stage 2, 50% chance — correct guess):**
```json
{
  "correct": true,
  "characterId": {
    "encrypted": true,
    "ciphertext": "b7e2a1f3...",
    "cipherType": "aes-128-gcm"
  },
  "score": {
    "base": 1000,
    "timeBonus": 300,
    "questionBonus": 200,
    "reliabilityPenalty": 0,
    "total": 1500
  }
}
```

**Encrypted guess response (Stage 2, 50% chance — incorrect guess):**
```json
{
  "correct": false,
  "characterId": {
    "encrypted": true,
    "ciphertext": "c9d1e2f3...",
    "cipherType": "aes-128-gcm"
  },
  "guessesRemaining": 2
}
```

### Session Lifecycle with Pending Decryption

When a guess response is encrypted, the session is **not marked as complete** — even if the guess was correct. The session stays open, occupying the team's active session slot, until the player resolves the pending decryption via `POST /sessions/{sessionId}/decrypt`.

```
Guess submitted → 50% roll triggers encrypted response
  │
  ├─ session.PendingDecryption = true
  ├─ session.PendingGuessCorrect = <true|false>
  ├─ session.PendingDecryptionPlaintext = "P34" or "n/a"
  └─ session.Completed = false  ← NOT marked complete yet
                │
                ▼
  Player derives Witness Key → decrypts → submits /decrypt
                │
  ┌─────────────┴──────────────┐
  │ Guess was CORRECT          │ Guess was INCORRECT
  │ Decryption correct         │ Decryption correct
  │ → +100 bonus applied       │ → -200 penalty applied
  │ → SP1 milestone awarded    │ → Guess count decremented
  │ → Session marked complete  │ → Session continues
  └────────────────────────────┘
```

New session fields required on [`domain.Session`](../internal/domain/session.go:25):

```go
PendingDecryption           bool   `json:"pendingDecryption"`
PendingGuessCorrect         bool   `json:"pendingGuessCorrect"`
PendingDecryptionPlaintext  string `json:"pendingDecryptionPlaintext"`
PendingScore                int    `json:"pendingScore"`
```

The encrypted `characterId` uses the same session cipher and key as trait answer encryption. In Stage 2, the key is not provided — players must derive it via the Witness Key mechanism.

To claim the SP1 milestone, the player submits the decrypted `characterId` to `POST /sessions/{sessionId}/decrypt`.

### Decode Endpoint — Stage 2 Behaviour

`POST /sessions/{sessionId}/decode`

In Stage 2, the decode endpoint returns **only the cipher algorithm** — NOT the key.

**Stage 2 response:**
```json
{
  "cipher": "aes-128-gcm",
  "encoding": "hex",
  "hint": "The answer has been encrypted with AES-128-GCM. The first 12 bytes of the hex-decoded ciphertext are the nonce. Derive the decryption key using the Witness Key mechanism documented in the game instructions."
}
```

The `key` field is **absent** in Stage 2 responses.

---

## Witness Key Mechanism

The decryption key for Stage 2 is derived deterministically from the player's own game history — specifically, the unencrypted boolean trait answers they received during the session. Players who played carefully and recorded their answers have all the material needed to derive the key.

### Key Derivation Algorithm

```
1. Collect all questions asked in this session where the answer was NOT encrypted:
   session.EncryptedQuestions[questionId] == false

2. For each such question, form the pair:
   "{traitKey}={answer}"
   where answer is the boolean value as a string ("true" or "false")

3. Sort the pairs alphabetically by traitKey

4. Join with "|" separator:
   key_material = "faculty=true|hair_color=false|year_group=true"

5. Apply HKDF-SHA256:
   witness_key = HKDF(
     hash   = SHA-256,
     length = 32,
     salt   = session_id.encode("utf-8"),
     info   = b"guesswho-witness-v1",
     ikm    = key_material.encode("utf-8")
   )
```

The resulting 32-byte `witness_key` is used as the decryption key:

| Cipher | Key bytes used |
|--------|---------------|
| `xor` | First 8 bytes |
| `aes-128-gcm` | First 16 bytes |
| `aes-256-cbc` | All 32 bytes |

### Example

A player asked 5 questions. Questions 1, 3, and 5 were unencrypted:

| # | traitKey | answer | encrypted |
|---|----------|--------|-----------|
| Q1 | `hair_color` | `false` | false |
| Q2 | `wears_glasses` | — | true |
| Q3 | `faculty` | `true` | false |
| Q4 | `os_primary` | — | true |
| Q5 | `year_group` | `true` | false |

Sorted unencrypted pairs (alphabetical by traitKey):
```
faculty=true
hair_color=false
year_group=true
```

Key material string:
```
faculty=true|hair_color=false|year_group=true
```

Apply HKDF-SHA256 with `salt = session_id`, `info = "guesswho-witness-v1"`, `length = 32`. The resulting 32 bytes are the decryption key.

### Why It Is AI-Resistant

1. **The key material is session-specific game history.** The unencrypted answers depend on which questions were asked, which were randomly encrypted (30% probability), and the target character's actual trait values.
2. **The key space is large.** For `xor` (2^64), `aes-128-gcm` (2^128), and `aes-256-cbc` (2^256), brute-forcing is infeasible.
3. **The derivation requires the player's live game log.** An AI not present during the session cannot reconstruct the unencrypted answers.
4. **The session ID is the HKDF salt.** Even if two players asked identical questions and received identical answers, their keys differ because the session ID is unique.

### Why It Is Human-Solvable

1. **The player already has all the raw material** — their question log with `encrypted: false` responses.
2. **HKDF is a standard algorithm** with library support in every major language:
   - Go: `golang.org/x/crypto/hkdf`
   - Python: `cryptography.hazmat.primitives.kdf.hkdf`
   - Node.js: `crypto.hkdfSync`
3. **The hint in the `/decode` response** names the algorithm and format explicitly.
4. **The first ask is never encrypted**, so every player has at least one unencrypted answer.

### New Endpoint: `POST /sessions/{sessionId}/decrypt`

Players submit their decrypted `characterId` to resolve the pending decryption.

**Request:**
```json
{
  "decryptedAnswer": "P34"
}
```

**Response (correct decryption — underlying guess was correct):**
```json
{
  "correct": true,
  "decryptionBonus": 100,
  "milestoneAwarded": "SP1",
  "sessionEnded": true
}
```

**Response (correct decryption — underlying guess was incorrect):**
```json
{
  "correct": false,
  "guessesRemaining": 2,
  "sessionEnded": false
}
```

**Response (wrong decryption attempt):**
```json
{
  "error": "wrong_decryption",
  "message": "The decrypted answer does not match. Check your key derivation and try again."
}
```

**Scoring on correct decryption:**

| Scenario | Effect |
|----------|--------|
| Correct guess + correct decryption | Full score applied + **+100 decryption bonus**. Session ends. SP1 awarded. |
| Incorrect guess + correct decryption | −200 wrong-guess penalty applied. Guess count decremented. Session continues. No bonus. |

Players may retry wrong decryption attempts without penalty. The endpoint remains available for the lifetime of the session (24-hour Redis TTL).

---

## Milestone Summary

| Milestone | Name | Trigger | Stage | Points |
|-----------|------|---------|-------|--------|
| M5 | Encrypted Answer Handled | First successful call to `/decode` that returns the key | Stage 1 | 1000 |
| SP1 | Cipher Master | Successfully decrypting an encrypted guess response using the Witness Key | Stage 2 | 4000 |

SP1 is a **Special Milestone** — a new classification above Stretch milestones, reflecting the significantly higher difficulty. It is awarded once per team, idempotent.

---

## Implementation Status

| Feature | Status |
|---------|--------|
| Pure boolean ask endpoint | ✅ Implemented |
| 25% trait blocking (Stage 1) | ✅ Implemented |
| 40% encrypted trait response | ✅ Implemented |
| `EncryptedQuestions` tracking on session | ✅ Implemented |
| Decode endpoint — returns key (Stage 1) | ✅ Implemented |
| M5 milestone on first decode | ✅ Implemented |
| New cipher set (`caesar`, `vigenere`, `xor`, `aes-128-gcm`, `aes-256-cbc`) | 🔲 Not yet implemented |
| Remove old ciphers (`base64`, `hex`, `reverse`, `xor-base64`) | 🔲 Not yet implemented |
| `Stage` field on `domain.Session` | 🔲 Not yet implemented |
| Stage 2 activation at 48 solves | 🔲 Not yet implemented |
| Stage 2 cipher pool restriction | 🔲 Not yet implemented |
| 0% blocking in Stage 2 | 🔲 Not yet implemented |
| 50% encrypted guess response (Stage 2) | 🔲 Not yet implemented |
| Decode endpoint — algorithm only (Stage 2) | 🔲 Not yet implemented |
| `DeriveWitnessKey` function | 🔲 Not yet implemented |
| `POST /sessions/{sessionId}/decrypt` endpoint | 🔲 Not yet implemented |
| SP1 milestone definition and award | 🔲 Not yet implemented |

---

## Stage 2 Implementation Guide

This section describes the code changes required to implement Stage 2.

### 1. Update `domain/milestone.go`

Add the SP1 milestone constant and score:

```go
const (
    MilestoneM1  Milestone = "M1"
    MilestoneM2  Milestone = "M2"
    MilestoneM3  Milestone = "M3"
    MilestoneM4  Milestone = "M4"
    MilestoneM5  Milestone = "M5"
    MilestoneS1  Milestone = "S1"
    MilestoneS2  Milestone = "S2"
    MilestoneS3  Milestone = "S3"
    MilestoneSP1 Milestone = "SP1" // NEW: Cipher Master
)

const (
    CoreMilestoneScore    = 1000
    StretchMilestoneScore = 2000
    SpecialMilestoneScore = 4000 // NEW
)
```

Add to `AllMilestones`:
```go
{MilestoneSP1, "Cipher Master", "Derived the Witness Key and decrypted an encrypted guess response", SpecialMilestoneScore},
```

### 2. Update `domain/session.go`

Add `Stage` field and extend `EncryptKey` to 32 bytes:

```go
type Session struct {
    // ... existing fields ...
    Stage int `json:"stage"` // 1 or 2
}
```

Update `NewSession()` to accept a `stage int` parameter and select the cipher pool accordingly:

```go
// Stage 1: all 5 ciphers
stage1Ciphers := []string{"caesar", "vigenere", "xor", "aes-128-gcm", "aes-256-cbc"}
// Stage 2: secure ciphers only
stage2Ciphers := []string{"xor", "aes-128-gcm", "aes-256-cbc"}

// Generate 32-byte key (up from 8 bytes)
keyBytes := make([]byte, 32)
cryptorand.Read(keyBytes)
encryptKey := hex.EncodeToString(keyBytes) // 64-char hex string
```

### 3. Update `service/encryption.go`

Add AES-128-GCM and AES-256-CBC to `Encrypt`, `Decrypt`, and `GetCipherInfo`. Remove `base64`, `hex`, `reverse`, and `xor-base64` cases.

**AES-128-GCM encrypt (Go):**
```go
case "aes-128-gcm":
    key, _ := hex.DecodeString(keyHex[:32]) // first 16 bytes
    block, _ := aes.NewCipher(key)
    gcm, _ := cipher.NewGCM(block)
    nonce := make([]byte, gcm.NonceSize()) // 12 bytes
    cryptorand.Read(nonce)
    ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
    return hex.EncodeToString(ciphertext), nil
```

**AES-128-GCM decrypt (Go):**
```go
case "aes-128-gcm":
    key, _ := hex.DecodeString(keyHex[:32])
    data, _ := hex.DecodeString(ciphertext)
    block, _ := aes.NewCipher(key)
    gcm, _ := cipher.NewGCM(block)
    nonce, ciphertextBytes := data[:gcm.NonceSize()], data[gcm.NonceSize():]
    plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
    return string(plaintext), err
```

**AES-256-CBC encrypt (Go):**
```go
case "aes-256-cbc":
    key, _ := hex.DecodeString(keyHex) // all 32 bytes
    block, _ := aes.NewCipher(key)
    plainPadded := pkcs7Pad([]byte(plaintext), aes.BlockSize)
    iv := make([]byte, aes.BlockSize)
    cryptorand.Read(iv)
    mode := cipher.NewCBCEncrypter(block, iv)
    encrypted := make([]byte, len(plainPadded))
    mode.CryptBlocks(encrypted, plainPadded)
    return hex.EncodeToString(append(iv, encrypted...)), nil
```

Required imports: `crypto/aes`, `crypto/cipher`.

Update `GetCipherInfo` to return appropriate hints for the new ciphers, and to omit the `Key` field when called in Stage 2 context (pass `includeKey bool` parameter, or handle at the handler layer).

### 4. Update `service/session_service.go`

**In `StartSession`:** Determine stage from `teamData.Solves` and pass to `NewSession`:
```go
stage := 1
if teamData.Solves >= 48 {
    stage = 2
}
session := domain.NewSession(sessionID, teamID, 3, perSessionSeed, chaosProfile, stage)
```

**In `AskQuestion`:** Skip the block roll when `session.Stage == 2`.

**In `SubmitGuess`:** When `session.Stage == 2`, roll 50% for guess response encryption on **every** guess (correct or incorrect). If the roll triggers:
- Set `session.PendingDecryption = true`
- Set `session.PendingGuessCorrect = correct`
- Set `session.PendingDecryptionPlaintext` to the fake candidate ID if correct, or `"n/a"` if incorrect
- Set `session.PendingScore` to the computed score (for correct guesses)
- Do **not** mark the session complete — return the encrypted `characterId` in the response

```go
if session.Stage == 2 {
    rollBytes := make([]byte, 1)
    cryptorand.Read(rollBytes)
    if int(rollBytes[0])%100 < 50 {
        plaintext := "n/a"
        if correct {
            plaintext = session.FakeCharacterID // e.g. "P34"
        }
        ciphertext, _ := h.encryptionService.Encrypt(session.EncryptCipher, session.EncryptKey, plaintext)
        session.PendingDecryption = true
        session.PendingGuessCorrect = correct
        session.PendingDecryptionPlaintext = plaintext
        session.PendingScore = computedScore
        // Return encrypted characterId; do NOT mark session complete
    }
}
```

### 5. Add `DeriveWitnessKey` to `service/encryption.go`

```go
type WitnessKeyPair struct {
    TraitKey string
    Answer   string // "true" or "false"
}

func DeriveWitnessKey(sessionID string, pairs []WitnessKeyPair) ([]byte, error) {
    sort.Slice(pairs, func(i, j int) bool {
        return pairs[i].TraitKey < pairs[j].TraitKey
    })
    parts := make([]string, len(pairs))
    for i, p := range pairs {
        parts[i] = p.TraitKey + "=" + p.Answer
    }
    keyMaterial := strings.Join(parts, "|")

    salt := []byte(sessionID)
    info := []byte("guesswho-witness-v1")
    r := hkdf.New(sha256.New, []byte(keyMaterial), salt, info)
    key := make([]byte, 32)
    if _, err := io.ReadFull(r, key); err != nil {
        return nil, err
    }
    return key, nil
}
```

Required imports: `golang.org/x/crypto/hkdf`, `crypto/sha256`, `io`, `sort`, `strings`.

The caller slices the 32-byte key to the length required by the session cipher:
- `xor`: `key[:8]`
- `aes-128-gcm`: `key[:16]`
- `aes-256-cbc`: `key` (all 32 bytes)

### 6. Update `handler/trait_handler.go` — Decode Stage Awareness

The `Decode` handler must check `session.Stage` and omit the `Key` field from the response when in Stage 2:

```go
hint := h.encryptionService.GetCipherInfo(session.EncryptCipher, session.EncryptKey)

response := DecodeResponse{
    Cipher:   hint.Cipher,
    Encoding: hint.Encoding,
    Hint:     hint.Hint,
}
if session.Stage == 1 {
    response.Key = hint.Key // only include key in Stage 1
}
```

### 7. Add `handler/session_handler.go` — `SubmitDecryption`

New handler for `POST /sessions/{sessionId}/decrypt`:

```go
type DecryptRequest struct {
    DecryptedAnswer string `json:"decryptedAnswer"`
}

func (h *SessionHandler) SubmitDecryption(w http.ResponseWriter, r *http.Request) {
    sessionID := r.PathValue("sessionId")
    // 1. Load session; return 404 if not found
    // 2. Verify session.Stage == 2; return 400 if not
    // 3. Verify session.PendingDecryption == true; return 400 "no_pending_decryption" if not
    // 4. Decode request body → req.DecryptedAnswer
    // 5. Compare req.DecryptedAnswer against session.PendingDecryptionPlaintext
    //    - Wrong: return 400 wrong_decryption (no penalty; player may retry)
    //    - Correct:
    //      a. Clear session.PendingDecryption = false
    //      b. If session.PendingGuessCorrect:
    //           - Apply session.PendingScore + 100 decryption bonus to team score
    //           - Award SP1 milestone (idempotent)
    //           - Mark session.Completed = true
    //           - Return { correct: true, decryptionBonus: 100, milestoneAwarded: "SP1", sessionEnded: true }
    //      c. If !session.PendingGuessCorrect:
    //           - Apply −200 wrong-guess penalty to team score
    //           - Decrement session.GuessesRemaining
    //           - If GuessesRemaining == 0: mark session.Completed = true
    //           - Return { correct: false, guessesRemaining: N, sessionEnded: <bool> }
    // 6. Persist updated session to Redis
}
```

### 8. Register New Route in `guesswhoserviceapi.go`

```go
mux.HandleFunc("POST /sessions/{sessionId}/decrypt", sessionHandler.SubmitDecryption)
```

---

## Open Questions

1. **Retry limit on `/decrypt`.** Currently unlimited retries are allowed. The player cannot brute-force the plaintext space without the key, and wrong attempts are harmless. Recommendation: no limit for initial release.

2. **Minimum unencrypted answers.** If a player is very unlucky and all their answers are encrypted, they have no key material. Consider guaranteeing at least N unencrypted answers per session (e.g., the first 3 asks are never encrypted). The first-ask exemption already provides 1.

3. **Stage 2 cipher pool for trait asks vs. guess response.** Currently both use the same session cipher. An alternative is to use a different cipher for the guess response to increase variety. Recommendation: keep the same cipher per session for simplicity.

4. **`GetCipherInfo` key leakage.** The existing `GetCipherInfo()` function returns the raw `EncryptKey`. The Stage 2 decode handler must not call this with `includeKey=true`. This is documented in `security-assessment-encryption.md` and must be enforced at the handler layer.