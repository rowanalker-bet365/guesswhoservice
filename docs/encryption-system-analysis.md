# Encryption System Analysis

**Service:** guesswhoservice
**Date:** 2026-02-27
**Branch:** Deploy-miletsones

---

## Table of Contents

1. [Overview](#1-overview)
2. [Which Traits Are Encrypted](#2-which-traits-are-encrypted)
3. [Encryption Flow](#3-encryption-flow)
4. [Encrypted Response Format](#4-encrypted-response-format)
5. [Decode Endpoint](#5-decode-endpoint)
6. [Decryption Flow](#6-decryption-flow)
7. [Milestone Integration](#7-milestone-integration)
8. [Security Assessment](#8-security-assessment)
9. [Sequence Diagram](#9-sequence-diagram)
10. [Current Pain Points and Observations](#10-current-pain-points-and-observations)

---

## 1. Overview

The encryption system controls how answers to certain trait questions are delivered to clients. Instead of returning the plaintext answer directly (e.g., `"Python"`), the server wraps it in an encoded payload and returns a string that begins with `b64:`. The client must then make a second HTTP call to `POST /sessions/{sessionId}/decode` to recover the original value.

**The problem it solves in the game context:**

The game is a "Guess Who" competition where teams automate solutions to identify a hidden character. If every answer were returned in plaintext, a client could trivially read trait values directly from the JSON. The encrypted tier forces clients to implement an additional step — calling the decode endpoint — before they can use the answer in their elimination logic. This adds a layer of integration complexity that serves as a milestone gate: milestone **M5 ("Encrypted Answer Handled")** is only awarded once a team successfully calls the decode endpoint.

The system is intentionally described as simplified (see the comment in `internal/service/encryption.go` line 11: `"Simplified per Solution spec: returns "b64:<base64>" and decodes from it."`). The original design document (`docs/SOLUTION_DOCUMENT.md`) specified AES-256, but the implementation uses base64 encoding with a structured prefix instead.

---

## 2. Which Traits Are Encrypted

Traits are defined in `internal/service/trait_catalog.go`. The `IsEncrypted` flag on `domain.TraitDefinition` (`internal/domain/trait.go`, line 30) controls whether the answer goes through the encryption path.

### Pure Encrypted Traits (Tier: `encrypted`, not flaky)

| ID  | Trait Key          | Question              | Type | Values                          | Cost |
|-----|--------------------|-----------------------|------|---------------------------------|------|
| T29 | `phone_os`         | Phone OS?             | enum | iOS, Android, other             | 2    |
| T36 | `preferred_editor` | Preferred editor?     | enum | VSCode, IntelliJ, Vim, Other    | 2    |
| T37 | `os_primary`       | Primary OS?           | enum | Windows, macOS, Linux           | 2    |
| T43 | `favorite_language`| Favorite language?    | enum | Python, JS, Java, C#, Other     | 2    |
| T48 | `keyboard_layout`  | Keyboard layout?      | enum | QWERTY, AZERTY, Other           | 2    |
| T56 | `coffee_order`     | Coffee order?         | enum | latte, americano, tea, none     | 2    |

### Both Encrypted AND Flaky (Tier: `flaky`, `IsEncrypted: true`, `IsFlaky: true`)

| ID  | Trait Key           | Question            | Type | Values           | Cost |
|-----|---------------------|---------------------|------|------------------|------|
| T61 | `training_provider` | Training provider?  | enum | A, B, C, D       | 4    |
| T64 | `risk_band`         | Risk band?          | enum | low, medium, high| 4    |

**Total encrypted traits: 8** (6 encrypted-only, 2 encrypted+flaky)

The remaining 56 traits are either `basic` or `flaky` with `IsEncrypted: false` and return their answers in plaintext.

**Source references:**

- `internal/service/trait_catalog.go`, lines 96, 105–106, 114, 119, 129, 136, 139
- `internal/domain/trait.go`, lines 30–31

---

## 3. Encryption Flow

This describes what happens server-side when a client asks an encrypted question.

**Entry point:** `POST /sessions/{sessionId}/ask` → `handler.SessionHandler.AskQuestion` → `service.sessionService.AskQuestion`

### Step-by-step

**Step 1 — Trait lookup**

```go
// internal/service/session_service.go, line 205
traitDef, exists := s.traitCatalog.GetTraitByID(questionID)
```

The trait catalog is consulted by question ID (e.g., `"T43"`). The returned `TraitDefinition` carries the `IsEncrypted` boolean.

**Step 2 — Get the raw answer**

```go
// internal/service/session_service.go, line 226
answer, exists := session.TargetCandidate.GetTrait(traitDef.TraitKey)
```

The target candidate's trait value is retrieved from a `map[string]interface{}` (defined in `internal/domain/candidate.go`, line 8). The value is the raw Go type: a `string` for enum traits, a `bool` for boolean traits.

**Step 3 — Branch on IsEncrypted**

```go
// internal/service/session_service.go, lines 256–266
if traitDef.IsEncrypted {
    plaintext := fmt.Sprintf("%v", answer)
    encrypted, err := s.encryptionService.Encrypt(plaintext)
    if err != nil {
        return nil, fmt.Errorf("failed to encrypt answer: %w", err)
    }
    traitAnswer.Encrypted = encrypted
} else {
    traitAnswer.Answer = answer
}
```

The raw value is stringified with `fmt.Sprintf("%v", answer)` before being passed to `Encrypt`. This converts any Go type to its default string representation (e.g., `true` → `"true"`, `"Python"` → `"Python"`).

**Step 4 — Encrypt function**

```go
// internal/service/encryption.go, lines 26–31
func (s *encryptionService) Encrypt(plaintext string) (string, error) {
    payload := fmt.Sprintf("PAYLOAD::%s", plaintext)
    encoded := base64.StdEncoding.EncodeToString([]byte(payload))
    return fmt.Sprintf("b64:%s", encoded), nil
}
```

The process is:
1. Prepend the literal string `PAYLOAD::` to the plaintext → e.g., `"PAYLOAD::Python"`
2. Base64-encode the resulting bytes → e.g., `"UEFZTE9BRDo6UHl0aG9u"`
3. Prepend the literal string `b64:` → e.g., `"b64:UEFZTE9BRDo6UHl0aG9u"`

**Concrete example for `favorite_language = "Python"`:**

```
plaintext  →  "Python"
step 1     →  "PAYLOAD::Python"
step 2     →  "UEFZTE9BRDo6UHl0aG9u"   (standard base64)
step 3     →  "b64:UEFZTE9BRDo6UHl0aG9u"
```

---

## 4. Encrypted Response Format

The `TraitAnswer` struct in `internal/domain/trait.go` (lines 41–50) uses `omitempty` on both `Answer` and `Encrypted`, so exactly one of them will appear in the JSON response.

### Normal (plaintext) answer

```json
{
  "questionId": "T01",
  "traitKey": "hair_color",
  "answer": "brown"
}
```

For a boolean trait:

```json
{
  "questionId": "T04",
  "traitKey": "wears_glasses",
  "answer": true
}
```

### Encrypted answer

```json
{
  "questionId": "T43",
  "traitKey": "favorite_language",
  "encrypted": "b64:UEFZTE9BRDo6UHl0aG9u"
}
```

The `answer` field is absent. The `encrypted` field holds the full `b64:<base64>` string.

Note: The `TraitAnswer` struct also has `Cipher` and `KeyHintID` fields (`internal/domain/trait.go`, lines 46–47):

```go
Cipher    string `json:"cipher,omitempty"`
KeyHintID string `json:"keyHintId,omitempty"`
```

These fields are never populated by any code in the current implementation. They appear to be stubs for a future scheme where the cipher type or a key hint would be communicated to the client. They will be omitted from all responses as long as they remain empty.

### Degraded (chaos) answer

When a flaky trait is requested during a chaos window and the chaos service decides to inject a failure, a degraded response is returned instead:

```json
{
  "questionId": "T61",
  "traitKey": "training_provider",
  "status": "degraded",
  "retryAfterMs": 5000
}
```

For traits that are both flaky and encrypted (T61, T64), the degraded path short-circuits before encryption is attempted — no `encrypted` field appears.

---

## 5. Decode Endpoint

**Route:** `POST /sessions/{sessionId}/decode`
**Handler:** `handler.TraitHandler.Decode` in `internal/handler/trait_handler.go`
**Registration:** `guesswhoserviceapi.go`, line 158

```go
publicMux.HandleFunc("POST /sessions/{sessionId}/decode", traitHandler.Decode)
```

This route is on the public mux — no JWT or API key is required to call it. The session ID in the path is structural (matches the pattern of other session routes) but the handler does not validate or read the session ID. The endpoint accepts any `encrypted` value regardless of which session it came from.

### Request format

```json
{
  "encrypted": "b64:UEFZTE9BRDo6UHl0aG9u"
}
```

The `encrypted` field is defined as:

```go
// internal/handler/trait_handler.go, line 56
type DecodeRequest struct {
    Encrypted string `json:"encrypted"`
}
```

### Response format

On success (HTTP 200):

```json
{
  "decrypted": "Python"
}
```

Defined as:

```go
// internal/handler/trait_handler.go, lines 61–63
type DecodeResponse struct {
    Decrypted string `json:"decrypted"`
}
```

On failure (HTTP 400):

```
Failed to decode: failed to decode: illegal base64 data at input byte X
```

The error is returned as plain text, not JSON.

### Milestone side effect

After a successful decode, the handler awards **M5**:

```go
// internal/handler/trait_handler.go, lines 85–87
if teamID, ok := r.Context().Value(middleware.TeamIDKey).(string); ok && teamID != "" {
    h.milestoneService.AwardIfAbsent(r.Context(), teamID, domain.MilestoneM5)
}
```

The `teamID` is read from the request context, which is populated by the `JWTAuth` middleware. Since the decode endpoint is on the public mux (no JWT middleware applied), the context will not carry a `teamID` unless the client explicitly provides a JWT `Authorization` header. Without a JWT, `teamID` will be the zero value and `AwardIfAbsent` will not be called — **M5 will not be awarded for unauthenticated decode calls**.

---

## 6. Decryption Flow

**Entry point:** `service.EncryptionService.Decrypt` in `internal/service/encryption.go`

### Step-by-step

```go
// internal/service/encryption.go, lines 33–52
func (s *encryptionService) Decrypt(encrypted string) (string, error) {
    if len(encrypted) > 4 && encrypted[:4] == "b64:" {
        encrypted = encrypted[4:]
    }

    decoded, err := base64.StdEncoding.DecodeString(encrypted)
    if err != nil {
        return "", fmt.Errorf("failed to decode: %w", err)
    }

    payload := string(decoded)

    if len(payload) > 9 && payload[:9] == "PAYLOAD::" {
        return payload[9:], nil
    }

    return payload, nil
}
```

**Step 1 — Strip `b64:` prefix**

If the string starts with `b64:` (length check: `> 4`), the first 4 characters are stripped. If the prefix is absent, the string is treated as raw base64 — the function does not return an error, it simply proceeds.

**Step 2 — Base64 decode**

Standard base64 decoding is applied. If the input is not valid base64, an error is returned wrapping the standard library error.

**Step 3 — Strip `PAYLOAD::` prefix**

If the decoded bytes, when interpreted as a string, begin with `PAYLOAD::` (length check: `> 9`), the first 9 characters are stripped and the remainder is returned. If the prefix is absent, the raw decoded string is returned without error.

**Concrete example:**

```
input      →  "b64:UEFZTE9BRDo6UHl0aG9u"
step 1     →  "UEFZTE9BRDo6UHl0aG9u"
step 2     →  "PAYLOAD::Python"
step 3     →  "Python"
output     →  "Python"
```

---

## 7. Milestone Integration

Milestones are defined in `internal/domain/milestone.go`. The encryption system is directly involved in one core milestone.

| Milestone | Name                     | Trigger                                                                                       | Score |
|-----------|--------------------------|-----------------------------------------------------------------------------------------------|-------|
| M5        | Encrypted Answer Handled | Awarded when a team successfully calls `POST /sessions/{sessionId}/decode` with a valid JWT   | 1000  |

The award happens in `internal/handler/trait_handler.go` at lines 85–87 (see Section 5 above).

Other milestones tangentially related to encrypted traits:

| Milestone | Name          | Trigger                                                                                                        | Score |
|-----------|---------------|----------------------------------------------------------------------------------------------------------------|-------|
| S3        | Resilience    | Awarded when a flaky trait (e.g., T61, T64) returns a successful answer during an active chaos window          | 2000  |

S3 is triggered by flaky traits specifically, and T61 and T64 happen to be both flaky and encrypted. A successful (non-degraded) answer from T61 or T64 during a chaos window awards S3 — but the encryption on those traits is handled after the chaos check, so a degraded response from T61/T64 does not trigger S3.

**S3 logic** (`internal/service/session_service.go`, lines 245–247):

```go
if traitDef.IsFlaky && s.chaosService.IsInChaosWindow(session) {
    s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneS3)
}
```

---

## 8. Security Assessment

### Is this real encryption?

No. The system uses **base64 encoding with a structured prefix**, not cryptographic encryption. It provides no confidentiality guarantee.

Base64 is a binary-to-text encoding scheme. It is fully reversible by anyone with the output, requires no key, and is a standard tool available in every programming language's standard library. Any client that receives `b64:UEFZTE9BRDo6UHl0aG9u` can decode it directly:

```bash
echo "UEFZTE9BRDo6UHl0aG9u" | base64 --decode
# Output: PAYLOAD::Python
```

The `PAYLOAD::` prefix is a fixed literal that adds no security — it is a structural sentinel the server uses during decoding to know whether to strip a prefix.

### Could it be trivially reversed?

Yes. The steps to reverse the encoding without calling the decode endpoint are:

1. Take the `encrypted` value from the API response.
2. Strip the leading `b64:` characters.
3. Run standard base64 decode on the remainder.
4. Strip the leading `PAYLOAD::` characters (9 bytes).
5. The result is the plaintext trait value.

A client that knows this structure can skip the `/decode` call entirely. The only cost of doing so is missing milestone M5 (which requires calling the endpoint).

### What the system does achieve

- **Integration gate:** Forces clients to implement a second HTTP call, which is a deliberate complexity requirement for the hackathon.
- **Milestone gate:** M5 is only awarded via the decode endpoint, so teams that shortcut the decoding locally will not earn M5.
- **Discoverability challenge:** Teams that do not read the documentation may not immediately understand that `b64:` values are base64 and must figure out the decode endpoint exists.

### Comparison with the original specification

The original `docs/SOLUTION_DOCUMENT.md` (Section 11, "Encryption/Decryption Implementation") specified:

> "Use symmetric encryption (AES-256 recommended)"
> "Use authenticated encryption (AES-GCM) to prevent tampering"
> "Include random IV/nonce with each encryption"

The implementation does not use AES, GCM, or any nonce. The comment at `internal/service/encryption.go` line 11 acknowledges this explicitly:

```go
// Simplified per Solution spec: returns "b64:<base64>" and decodes from it.
```

This is an intentional simplification, likely made to keep the hackathon server implementation lean and to ensure the decode step is deterministic (no session-specific keys required).

---

## 9. Sequence Diagram

The following diagram shows the complete flow from asking an encrypted question through to using the decrypted answer.

```
Client                          Server (sessionService)         EncryptionService
  |                                      |                              |
  |  POST /sessions/{id}/ask             |                              |
  |  { "questionId": "T43" }             |                              |
  |------------------------------------->|                              |
  |                                      |                              |
  |                         GetTraitByID("T43")                        |
  |                         traitDef.IsEncrypted == true               |
  |                                      |                              |
  |                   GetTrait("favorite_language")                     |
  |                         answer = "Python"                          |
  |                                      |                              |
  |                         plaintext = fmt.Sprintf("%v", answer)      |
  |                                      |  Encrypt("Python")           |
  |                                      |----------------------------->|
  |                                      |                              |
  |                                      |   payload = "PAYLOAD::Python"|
  |                                      |   encoded = base64(payload)  |
  |                                      |   return "b64:<encoded>"     |
  |                                      |<-----------------------------|
  |                                      |                              |
  |  HTTP 200                            |                              |
  |  {                                   |                              |
  |    "questionId": "T43",              |                              |
  |    "traitKey": "favorite_language",  |                              |
  |    "encrypted": "b64:UEFZ..."        |                              |
  |  }                                   |                              |
  |<-------------------------------------|                              |
  |                                      |                              |
  |  (Client stores encrypted value)     |                              |
  |                                      |                              |
  |  POST /sessions/{id}/decode          |                              |
  |  { "encrypted": "b64:UEFZ..." }      |                              |
  |------------------------------------->|                              |
  |                                 (TraitHandler.Decode)              |
  |                                      |  Decrypt("b64:UEFZ...")      |
  |                                      |----------------------------->|
  |                                      |                              |
  |                                      |   strip "b64:" prefix        |
  |                                      |   base64.Decode(...)         |
  |                                      |   strip "PAYLOAD::" prefix   |
  |                                      |   return "Python"            |
  |                                      |<-----------------------------|
  |                                      |                              |
  |                                 AwardIfAbsent(teamID, M5)          |
  |                                      |                              |
  |  HTTP 200                            |                              |
  |  { "decrypted": "Python" }           |                              |
  |<-------------------------------------|                              |
  |                                      |                              |
  |  (Client uses "Python" in           |                              |
  |   candidate elimination logic)       |                              |
```

---

## 10. Current Pain Points and Observations

### 1. The decode endpoint does not validate the session ID

`POST /sessions/{sessionId}/decode` accepts any `sessionId` in the path but never reads or validates it (`internal/handler/trait_handler.go` — the handler does not call `r.PathValue("sessionId")` or look up the session). A client can pass any string as the session ID, including a fake one, and the decode will succeed as long as the `encrypted` body is valid base64. This means:

- Encrypted values from session A can be decoded using session B's URL.
- Expired session IDs work just as well as active ones.
- There is no per-session key rotation (which is expected given the design, but worth noting).

### 2. M5 is silently not awarded for unauthenticated decode calls

The decode route is on the public mux with no JWT middleware. The milestone award inside `Decode` reads the team ID from the JWT context, which will be empty for clients that do not send an `Authorization: Bearer <token>` header. M5 will not be awarded in this case, with no error or warning returned to the client. This is a silent failure that could confuse teams that call `/decode` without a JWT.

### 3. Boolean trait values lose type fidelity after encoding

When an encrypted boolean trait is asked, the answer goes through:

```go
plaintext := fmt.Sprintf("%v", answer)  // answer is interface{} holding bool
```

This converts `true` (bool) to `"true"` (string). After decoding, the client receives `"true"` as a string in the `decrypted` field, not a JSON boolean. If the client needs to compare this against the board's trait values (which are stored as booleans in `map[string]interface{}`), it must handle the type conversion. In practice, the only encrypted boolean traits are T61 (`training_provider`) and... actually all 8 encrypted traits are `enum` (string values), so this is not a current problem. However, it would become one if any future boolean trait were marked encrypted.

### 4. The `Cipher` and `KeyHintID` fields are dead stubs

`domain.TraitAnswer` (`internal/domain/trait.go`, lines 46–47) defines:

```go
Cipher    string `json:"cipher,omitempty"`
KeyHintID string `json:"keyHintId,omitempty"`
```

No code ever sets these fields. They are artefacts of a more sophisticated encryption design that was not implemented. They add noise to the struct definition and could mislead readers into thinking cipher selection or key hints are a live feature.

### 5. Base64 encoding uses standard (padded) encoding

`base64.StdEncoding` uses the standard alphabet (`A-Z`, `a-z`, `0-9`, `+`, `/`) with `=` padding. This means the encoded string may contain `+` and `/` characters. If a client ever needs to embed the `encrypted` value in a URL query parameter (rather than a JSON body), those characters require percent-encoding. The decode endpoint only accepts the value via POST body, so this is not currently an issue — but it is worth noting if the design ever changes.

### 6. No protection against replay of encrypted values

Since encryption is deterministic (same plaintext always produces the same ciphertext — there is no nonce or salt), a client could observe that `"b64:UEFZTE9BRDo6UHl0aG9u"` always decodes to `"Python"` and cache the mapping. Over time, a client could build a complete rainbow table of all possible encrypted values, since the value space is small (enum traits have 3–6 possible values). This would allow a sophisticated team to skip the decode call entirely after the first session.

### 7. The `Encrypt` function cannot return an error in practice

The signature of `Encrypt` returns `(string, error)`, but the implementation:

```go
func (s *encryptionService) Encrypt(plaintext string) (string, error) {
    payload := fmt.Sprintf("PAYLOAD::%s", plaintext)
    encoded := base64.StdEncoding.EncodeToString([]byte(payload))
    return fmt.Sprintf("b64:%s", encoded), nil
}
```

always returns `nil` for the error. `base64.StdEncoding.EncodeToString` does not return an error. The error return value is vestigial from the interface definition and adds unnecessary error-handling boilerplate at the call site (`internal/service/session_service.go`, lines 258–261).

---

*End of analysis. All line number references are accurate as of commit `92fd103`.*
