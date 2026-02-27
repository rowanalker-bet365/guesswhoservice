# Implementation Traces: Chaos Engineering, Flaky Responses, and Encrypted Response Handling

## Table of Contents

1. [Chaos Engineering Implementation](#1-chaos-engineering-implementation)
2. [Flaky Response Implementation](#2-flaky-response-implementation)
3. [Encrypted Response Handling](#3-encrypted-response-handling)

---

## 1. Chaos Engineering Implementation

### 1.1 Entry Point / Trigger

Chaos engineering is triggered on a **per-question basis** during the [`sessionService.AskQuestion()`](../internal/service/session_service.go:180) method. The trigger point is:

```go
if s.chaosService.ShouldFail(session, traitDef) {
```

This is invoked every time a client sends `POST /sessions/{sessionId}/ask` with a `questionId`. The request flows through:

1. **HTTP layer**: [`SessionHandler.AskQuestion()`](../internal/handler/session_handler.go:114) receives the HTTP request, extracts `sessionId` from the path and `questionId` from the JSON body.
2. **Service layer**: Calls [`sessionService.AskQuestion()`](../internal/service/session_service.go:180) which loads the session from Redis, retrieves the trait definition, then checks [`chaosService.ShouldFail()`](../internal/service/chaos.go:33).

### 1.2 Configuration

Chaos is controlled by **three environment variables** loaded in [`config.Load()`](../config/config.go:25):

| Environment Variable | Field in [`Config`](../config/config.go:9) | Default | Purpose |
|---|---|---|---|
| `CHAOS_ENABLED` | [`ChaosEnabled`](../config/config.go:12) | `false` | Master on/off switch for chaos |
| `CHAOS_INTERVAL_SECONDS` | [`ChaosIntervalSeconds`](../config/config.go:13) | `240` (4 min) | Length of one full cycle (chaos window + safe period) |
| `CHAOS_WINDOW_SECONDS` | [`ChaosWindowSeconds`](../config/config.go:14) | `90` (1.5 min) | Duration of the chaos window within each interval |

These are passed through the application in [`guesswhoserviceapi.go`](../guesswhoserviceapi.go:97):

```go
chaosService := service.NewChaosService(cfg.ChaosEnabled)
```

And into the session service via [`SessionServiceConfig`](../internal/service/session_service.go:40):

```go
service.SessionServiceConfig{
    ChaosEnabled:  cfg.ChaosEnabled,
    ChaosInterval: cfg.ChaosIntervalSeconds,
    ChaosWindow:   cfg.ChaosWindowSeconds,
}
```

The chaos profile is embedded **per-session** at creation time in [`StartSession()`](../internal/service/session_service.go:80) at lines 87-91:

```go
chaosProfile := domain.ChaosProfile{
    Mode:            domain.ChaosModeScheduled,
    WindowSeconds:   s.chaosWindow,
    IntervalSeconds: s.chaosInterval,
}
```

This [`ChaosProfile`](../internal/domain/session.go:16) struct is stored within each [`Session`](../internal/domain/session.go:23) object and persisted to Redis.

### 1.3 Core Processing Logic

The chaos decision flows through [`ChaosService.ShouldFail()`](../internal/service/chaos.go:33) with this exact sequence:

**Step 1 -- Global enable check** (line 34-36):

```go
if !s.enabled {
    return false
}
```

If `CHAOS_ENABLED=false`, chaos never fires.

**Step 2 -- Trait flakiness check** (line 39-41):

```go
if !traitDef.IsFlaky {
    return false
}
```

Only traits with [`IsFlaky: true`](../internal/domain/trait.go:31) can trigger chaos failures.

**Step 3 -- Chaos window check** (line 44-46):

```go
if !s.isInChaosWindow(session) {
    return false
}
```

The [`isInChaosWindow()`](../internal/service/chaos.go:66) method implements **scheduled chaos windows**:

1. Checks that [`session.ChaosProfile.Mode`](../internal/domain/session.go:17) equals [`ChaosModeScheduled`](../internal/domain/session.go:11).
2. Calculates elapsed seconds since [`session.CreatedAt`](../internal/domain/session.go:33).
3. Uses modular arithmetic to determine position within the current interval cycle:
   ```go
   positionInInterval := float64(int(elapsed) % int(intervalSeconds))
   ```
4. Returns `true` if `positionInInterval < windowSeconds`.

**Example with defaults (interval=240s, window=90s)**:

- 0--89s: **IN** chaos window
- 90--239s: **OUT** of chaos window
- 240--329s: **IN** chaos window (next cycle)
- 330--479s: **OUT** of chaos window
- ...and so on cyclically

**Step 4 -- Probabilistic failure** (line 50-51):

```go
failureRate := 0.6 // 60% failure rate during chaos window
return s.rng.Float64() < failureRate
```

Even within a chaos window, there is only a **60% probability** of actual failure. The [`rng`](../internal/service/chaos.go:22) field is a `*rand.Rand` seeded with `time.Now().UnixNano()`.

### 1.4 Output / Effect on Response

When [`ShouldFail()`](../internal/service/chaos.go:33) returns `true`, the flow in [`AskQuestion()`](../internal/service/session_service.go:199) produces a **degraded response**:

```go
session.IncrementFailure("failed")
s.dbStore.WriteSession(ctx, session)

return &domain.TraitAnswer{
    QuestionID: questionID,
    TraitKey:   traitDef.TraitKey,
    Status:     "degraded",
    RetryAfter: s.chaosService.GetRetryDelay(),
}, nil
```

The [`TraitAnswer`](../internal/domain/trait.go:41) returned has:

- **`Status`**: `"degraded"` (JSON field `"status"`)
- **`RetryAfter`**: A random value between 500-2000 milliseconds (JSON field `"retryAfterMs"`) computed by [`GetRetryDelay()`](../internal/service/chaos.go:54): `500 + s.rng.Intn(1500)`
- **`Answer`**: omitted (zero value, `omitempty`)
- **`Encrypted`**: omitted (empty string, `omitempty`)

The HTTP response is serialized as JSON with **HTTP 200 status**. Chaos does NOT return an HTTP error code. It returns a successful HTTP response with a degraded payload.

### 1.5 Error Handling / Edge Cases

- **Session failure counter**: [`session.IncrementFailure("failed")`](../internal/domain/session.go:69) increments [`Session.FailedRequests`](../internal/domain/session.go:35). This counter is persisted to Redis.
- **Scoring penalty**: Failed requests feed into the scoring formula. In [`calculateReliabilityPenalty()`](../internal/service/scoring.go:48), each failed request costs **5 points**: `5 * session.FailedRequests`.
- **Question NOT recorded**: When chaos fires, [`session.RecordQuestion()`](../internal/domain/session.go:64) is **NOT called**. The question does not count toward [`QuestionsAsked`](../internal/domain/session.go:34).
- **Milestones NOT awarded**: M2 (First Successful Question) and M3 (Elimination Working) checks happen after the chaos check, so degraded responses do not trigger them.
- **No retry logic on the server**: The server simply suggests a retry delay via `retryAfterMs`. The client must implement retry logic.

### 1.6 Interactions with Other Components

- **Middleware stack**: Chaos operates entirely within the service layer. The middleware stack ([`corsMiddleware`](../guesswhoserviceapi.go:27), [`logging.HTTPMiddleware`](../internal/logging/middleware.go:20), [`timeoutMiddleware`](../guesswhoserviceapi.go:40)) has no awareness of chaos.
- **Rate limiter**: The `/sessions/{sessionId}/ask` endpoint has rate limiting via [`rateLimiter.Limit(60, 5)`](../guesswhoserviceapi.go:156). The rate limiter fires **before** chaos, so a rate-limited request never reaches the chaos check.
- **Milestone S3 (Resilience)**: When chaos is enabled and a flaky trait question **succeeds** (i.e., [`ShouldFail`](../internal/service/chaos.go:33) returned `false` but the session IS in a chaos window), the milestone S3 is awarded at [`session_service.go:233-235`](../internal/service/session_service.go:233):

  ```go
  if traitDef.IsFlaky && s.chaosService.IsInChaosWindow(session) {
      s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneS3)
  }
  ```

- **StartSession response**: The [`ChaosProfile`](../internal/domain/session.go:16) is exposed to the client in the [`StartSessionResponse`](../internal/handler/session_handler.go:27), so clients can see the chaos configuration.

### 1.7 Chaos Engineering Flow Diagram

```
Client: POST /sessions/{id}/ask {questionId}
  SessionHandler.AskQuestion()
    sessionService.AskQuestion(ctx, sessionID, questionID)
      dbStore.ReadSession(ctx, sessionID) -> session
      traitCatalog.GetTraitByID(questionID) -> traitDef
      chaosService.ShouldFail(session, traitDef)
        Check: s.enabled? (CHAOS_ENABLED)
        Check: traitDef.IsFlaky?
        Check: isInChaosWindow(session)?
          session.ChaosProfile.Mode == "scheduled"?
          elapsed since session.CreatedAt
          positionInInterval = elapsed % intervalSeconds
          positionInInterval < windowSeconds?
        Check: random < 0.6?
        Returns true/false

      IF ShouldFail returns true:
        session.IncrementFailure("failed")
        dbStore.WriteSession(ctx, session)
        Return TraitAnswer{Status:"degraded", RetryAfter:500-2000}

      IF ShouldFail returns false:
        session.TargetCandidate.GetTrait(traitDef.TraitKey) -> answer
        session.RecordQuestion(questionID)
        Check encryption/normal response path
        Return TraitAnswer with answer or encrypted value
```

---

## 2. Flaky Response Implementation

### 2.1 Overview

**Flaky responses and chaos engineering are the SAME mechanism.** There is no separate "flakiness" system. Flakiness is a **trait-level property** that makes certain traits eligible for chaos-induced failures. The entry point is identical: [`sessionService.AskQuestion()`](../internal/service/session_service.go:180) calling [`chaosService.ShouldFail()`](../internal/service/chaos.go:33).

### 2.2 What Makes a Trait Flaky

Flakiness is defined statically per trait in [`initializeTraits()`](../internal/service/trait_catalog.go:60) via two fields on [`TraitDefinition`](../internal/domain/trait.go:22):

- [`IsFlaky bool`](../internal/domain/trait.go:31): marks the trait as subject to chaos failures (tagged `json:"-"`, never exposed to clients)
- [`Tier TraitTier`](../internal/domain/trait.go:28): flaky traits use [`TierFlaky`](../internal/domain/trait.go:18) = `"flaky"`

### 2.3 The 6 Flaky Traits

| QuestionID | TraitKey | Type | Cost | Also Encrypted? | Location |
|---|---|---|---|---|---|
| T49 | `two_factor` | boolean | 3 | No | [`trait_catalog.go:120`](../internal/service/trait_catalog.go:120) |
| T50 | `password_manager` | boolean | 3 | No | [`trait_catalog.go:121`](../internal/service/trait_catalog.go:121) |
| T61 | `training_provider` | enum | 4 | **Yes** | [`trait_catalog.go:136`](../internal/service/trait_catalog.go:136) |
| T62 | `id_verified` | boolean | 4 | No | [`trait_catalog.go:137`](../internal/service/trait_catalog.go:137) |
| T63 | `eligibility` | boolean | 4 | No | [`trait_catalog.go:138`](../internal/service/trait_catalog.go:138) |
| T64 | `risk_band` | enum | 4 | **Yes** | [`trait_catalog.go:139`](../internal/service/trait_catalog.go:139) |

T61 and T64 are **both flaky AND encrypted**. They combine both challenge mechanisms.

### 2.4 Flaky Response Manifest

When a flaky trait triggers a chaos failure, the client receives:

```json
{
    "questionId": "T49",
    "traitKey": "two_factor",
    "status": "degraded",
    "retryAfterMs": 1234
}
```

Key characteristics:

- **HTTP status code**: `200 OK` (NOT an error code)
- **`status` field**: `"degraded"` (only present on chaos-affected responses)
- **`retryAfterMs` field**: Random integer 500--2000, suggests when to retry
- **`answer` field**: **absent** (omitted via `omitempty`)
- **`encrypted` field**: **absent** (omitted via `omitempty`)

The degraded response is constructed at [`session_service.go:205-210`](../internal/service/session_service.go:205):

```go
return &domain.TraitAnswer{
    QuestionID: questionID,
    TraitKey:   traitDef.TraitKey,
    Status:     "degraded",
    RetryAfter: s.chaosService.GetRetryDelay(),
}, nil
```

The [`TraitAnswer`](../internal/domain/trait.go:41) struct uses `omitempty` JSON tags on [`Answer`](../internal/domain/trait.go:44), [`Encrypted`](../internal/domain/trait.go:45), [`Cipher`](../internal/domain/trait.go:46), [`KeyHintID`](../internal/domain/trait.go:47), [`Status`](../internal/domain/trait.go:48), and [`RetryAfter`](../internal/domain/trait.go:49), so only populated fields appear in the JSON output.

### 2.5 How Flakiness Differs from Chaos

Flakiness is a **subset** of the chaos system, not a parallel mechanism:

| Aspect | Chaos (System) | Flakiness (Trait Property) |
|---|---|---|
| Scope | Global system feature | Per-trait boolean flag |
| Config | `CHAOS_ENABLED`, intervals, windows | [`IsFlaky`](../internal/domain/trait.go:31) field in trait catalog |
| Activation | Chaos window + 60% probability | Only when chaos system is active |
| Affected traits | Only flaky traits | Defines which traits chaos can affect |
| Relationship | Parent system | Filter/gate within the parent system |

The full decision chain in [`ShouldFail()`](../internal/service/chaos.go:33) requires all four conditions to be true:

1. [`s.enabled`](../internal/service/chaos.go:34) must be `true` (global chaos switch)
2. [`traitDef.IsFlaky`](../internal/service/chaos.go:39) must be `true` (trait-level gate)
3. [`isInChaosWindow(session)`](../internal/service/chaos.go:44) must return `true` (time-based window)
4. [`s.rng.Float64() < 0.6`](../internal/service/chaos.go:51) must be `true` (probabilistic check)

If ANY of these conditions is false, the trait responds normally. Flakiness (condition 2) is just one gate among four.

### 2.6 No Server-Side Retry Logic

There is no server-side retry or recovery mechanism. The `retryAfterMs` value in the response is purely advisory. The client is expected to:

1. Detect `status: "degraded"` in the response
2. Wait the suggested `retryAfterMs` duration
3. Re-send the same question
4. If the chaos window has passed or the 60% random check passes, get the real answer

### 2.7 Scoring Impact

Each failed (degraded) request:

- Increments [`Session.FailedRequests`](../internal/domain/session.go:35) via [`IncrementFailure("failed")`](../internal/domain/session.go:69)
- Costs **5 points** in the reliability penalty via [`calculateReliabilityPenalty()`](../internal/service/scoring.go:48): `5 * session.FailedRequests`
- Does NOT count as a question asked (does not affect question efficiency score)

The full reliability penalty formula in [`calculateReliabilityPenalty()`](../internal/service/scoring.go:48) is:

```go
penalty := (5 * session.FailedRequests) +
    (2 * session.Timeouts) +
    (10 * session.Unhandled5xx)
```

### 2.8 Milestone Interaction

- **M2 (First Successful Question)**: NOT awarded on degraded responses. M2 is checked at [`session_service.go:224`](../internal/service/session_service.go:224) which is only reached when [`ShouldFail()`](../internal/service/chaos.go:33) returns `false`.
- **M3 (Elimination Working)**: NOT awarded on degraded responses. M3 is checked at [`session_service.go:227`](../internal/service/session_service.go:227) which requires 3+ questions recorded via [`RecordQuestion()`](../internal/domain/session.go:64), and degraded responses skip that call.
- **S3 (Resilience)**: Awarded when a flaky trait succeeds DURING a chaos window. This is checked at [`session_service.go:233`](../internal/service/session_service.go:233). The [`IsInChaosWindow()`](../internal/service/chaos.go:62) public method delegates to the same [`isInChaosWindow()`](../internal/service/chaos.go:66) helper used by [`ShouldFail()`](../internal/service/chaos.go:33). S3 specifically rewards clients that successfully handle flaky traits despite active chaos.

---

## 3. Encrypted Response Handling

### 3.1 Entry Point / Trigger

Encrypted response handling is triggered during [`sessionService.AskQuestion()`](../internal/service/session_service.go:180) when a trait has [`IsEncrypted: true`](../internal/domain/trait.go:30). The decision point is at [`session_service.go:244`](../internal/service/session_service.go:244):

```go
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

This check only runs AFTER the chaos check passes (i.e., [`ShouldFail()`](../internal/service/chaos.go:33) returned `false`), meaning encrypted responses are never returned during chaos-degraded responses.

### 3.2 Which Traits Are Encrypted

There are **8 encrypted traits** defined in the trait catalog at [`initializeTraits()`](../internal/service/trait_catalog.go:60). They use [`TierEncrypted`](../internal/domain/trait.go:17) = `"encrypted"` and have [`IsEncrypted: true`](../internal/domain/trait.go:30):

**Pure encrypted traits (not flaky):**

| QuestionID | TraitKey | Type | Cost | Location |
|---|---|---|---|---|
| T29 | `phone_os` | enum | 2 | [`trait_catalog.go:96`](../internal/service/trait_catalog.go:96) |
| T36 | `preferred_editor` | enum | 2 | [`trait_catalog.go:105`](../internal/service/trait_catalog.go:105) |
| T37 | `os_primary` | enum | 2 | [`trait_catalog.go:106`](../internal/service/trait_catalog.go:106) |
| T43 | `favorite_language` | enum | 2 | [`trait_catalog.go:114`](../internal/service/trait_catalog.go:114) |
| T48 | `keyboard_layout` | enum | 2 | [`trait_catalog.go:119`](../internal/service/trait_catalog.go:119) |
| T56 | `coffee_order` | enum | 2 | [`trait_catalog.go:129`](../internal/service/trait_catalog.go:129) |

**Both encrypted AND flaky:**

| QuestionID | TraitKey | Type | Cost | Location |
|---|---|---|---|---|
| T61 | `training_provider` | enum | 4 | [`trait_catalog.go:136`](../internal/service/trait_catalog.go:136) |
| T64 | `risk_band` | enum | 4 | [`trait_catalog.go:139`](../internal/service/trait_catalog.go:139) |

Note: All encrypted traits happen to be of type `enum`. The [`IsEncrypted`](../internal/domain/trait.go:30) field is tagged `json:"-"` so it is never exposed to clients.

### 3.3 Encryption Mechanism

The [`EncryptionService`](../internal/service/encryption.go:11) interface is defined with four methods:

```go
type EncryptionService interface {
    Encrypt(plaintext string) (string, error)
    Decrypt(encrypted string) (string, error)
    HashPassword(password string) (string, error)
    CheckPasswordHash(password, hash string) bool
}
```

The implementation at [`encryptionService`](../internal/service/encryption.go:19) uses a simplified base64 encoding scheme.

**Encryption flow** in [`Encrypt()`](../internal/service/encryption.go:26):

1. Prepend `"PAYLOAD::"` prefix to the plaintext: `payload := fmt.Sprintf("PAYLOAD::%s", plaintext)`
2. Base64 encode the payload: `encoded := base64.StdEncoding.EncodeToString([]byte(payload))`
3. Prepend `"b64:"` prefix: `return fmt.Sprintf("b64:%s", encoded), nil`

Example: For plaintext `"Python"`, the encrypted output is `"b64:UEFZTE9BRDo6UHl0aG9u"`.

**Decryption flow** in [`Decrypt()`](../internal/service/encryption.go:33):

1. Strip the `"b64:"` prefix (line 35-37)
2. Base64 decode the remaining string (line 39-42)
3. Strip the `"PAYLOAD::"` prefix from the decoded result (line 47-49)
4. Return the original plaintext

### 3.4 Encrypted Response Format

When a trait is encrypted, the [`TraitAnswer`](../internal/domain/trait.go:41) response has:

- **`encrypted` field**: Contains the `"b64:..."` encoded string
- **`answer` field**: **absent** (omitted via `omitempty`)

Example encrypted response:

```json
{
    "questionId": "T43",
    "traitKey": "favorite_language",
    "encrypted": "b64:UEFZTE9BRDo6UHl0aG9u"
}
```

Compare with a normal (non-encrypted) response:

```json
{
    "questionId": "T01",
    "traitKey": "hair_color",
    "answer": "brown"
}
```

The key difference is that `answer` and `encrypted` are **mutually exclusive** in the response. This is controlled by the if/else at [`session_service.go:244-254`](../internal/service/session_service.go:244).

### 3.5 Decoding Endpoint

The client decodes encrypted answers via `POST /sessions/{sessionId}/decode`, handled by [`TraitHandler.Decode()`](../internal/handler/trait_handler.go:66).

**Request format** ([`DecodeRequest`](../internal/handler/trait_handler.go:56)):

```json
{
    "encrypted": "b64:UEFZTE9BRDo6UHl0aG9u"
}
```

**Response format** ([`DecodeResponse`](../internal/handler/trait_handler.go:61)):

```json
{
    "decrypted": "Python"
}
```

**Processing flow:**

1. Parse JSON body into [`DecodeRequest`](../internal/handler/trait_handler.go:56) (line 68)
2. Call [`encryptionService.Decrypt(req.Encrypted)`](../internal/service/encryption.go:33) (line 74)
3. On success, award milestone M5 if not already awarded (line 85-87):
   ```go
   if teamID, ok := r.Context().Value(middleware.TeamIDKey).(string); ok && teamID != "" {
       h.milestoneService.AwardIfAbsent(r.Context(), teamID, domain.MilestoneM5)
   }
   ```
4. Return [`DecodeResponse`](../internal/handler/trait_handler.go:61) with the decrypted plaintext (line 89-91)

Note: The team ID is extracted from JWT context via [`middleware.TeamIDKey`](../internal/middleware/auth_middleware.go). If no JWT is present (unauthenticated request), M5 is silently skipped but decoding still succeeds.

### 3.6 Milestone M5 (Encrypted Answer Handled)

[`MilestoneM5`](../internal/domain/milestone.go:11) is defined as `"M5"` with the description "Encrypted Answer Handled". It is a core milestone worth [`CoreMilestoneScore`](../internal/domain/milestone.go:19) = 1000 points.

M5 is awarded **once per team** the first time they successfully call the decode endpoint. The idempotency check is handled by [`milestoneService.AwardIfAbsent()`](../internal/service/milestone_service.go:33) which:

1. Reads the team's current milestones from Redis via [`store.ReadTeamData()`](../internal/service/milestone_service.go:42)
2. Checks if the milestone string is already in the list (line 54-57)
3. If absent, appends it and persists via [`store.SetMilestones()`](../internal/service/milestone_service.go:62)
4. Awards the score bonus via [`store.IncrementTeamScore()`](../internal/service/milestone_service.go:69)

### 3.7 Interaction with Flaky Traits

For traits that are **both encrypted AND flaky** (T61 `training_provider` and T64 `risk_band`), the behavior depends on the chaos state:

**Scenario A -- Chaos fires (degraded response):**

The response is the standard degraded format with NO encrypted content:

```json
{
    "questionId": "T61",
    "traitKey": "training_provider",
    "status": "degraded",
    "retryAfterMs": 1500
}
```

The encryption path at [`session_service.go:244`](../internal/service/session_service.go:244) is never reached because the function returns early at [`session_service.go:205`](../internal/service/session_service.go:205) with the degraded [`TraitAnswer`](../internal/domain/trait.go:41).

**Scenario B -- Chaos does not fire (successful response):**

The response contains the encrypted value:

```json
{
    "questionId": "T61",
    "traitKey": "training_provider",
    "encrypted": "b64:UEFZTE9BRDo6Qg=="
}
```

The client must then call `POST /sessions/{sessionId}/decode` with this encrypted value to get the plaintext answer.

### 3.8 Encrypted Response Flow Diagram

```
Client: POST /sessions/{id}/ask {questionId: "T43"}
  SessionHandler.AskQuestion()
    sessionService.AskQuestion(ctx, sessionID, "T43")
      dbStore.ReadSession(ctx, sessionID) -> session
      traitCatalog.GetTraitByID("T43") -> traitDef (IsEncrypted=true, IsFlaky=false)

      chaosService.ShouldFail(session, traitDef)
        traitDef.IsFlaky == false -> return false (skip chaos)

      session.TargetCandidate.GetTrait("favorite_language") -> "Python"
      session.RecordQuestion("T43")
      dbStore.WriteSession(ctx, session)

      traitDef.IsEncrypted == true:
        plaintext = "Python"
        encryptionService.Encrypt("Python")
          payload = "PAYLOAD::Python"
          encoded = base64("PAYLOAD::Python")
          return "b64:<encoded>"
        traitAnswer.Encrypted = "b64:<encoded>"

      Return TraitAnswer{QuestionID:"T43", TraitKey:"favorite_language", Encrypted:"b64:..."}

Client receives: {"questionId":"T43","traitKey":"favorite_language","encrypted":"b64:UEFZTE9BRDo6UHl0aG9u"}

Client: POST /sessions/{id}/decode {"encrypted":"b64:UEFZTE9BRDo6UHl0aG9u"}
  TraitHandler.Decode()
    encryptionService.Decrypt("b64:UEFZTE9BRDo6UHl0aG9u")
      strip "b64:" -> "UEFZTE9BRDo6UHl0aG9u"
      base64 decode -> "PAYLOAD::Python"
      strip "PAYLOAD::" -> "Python"
    Award M5 milestone (if team has JWT and M5 not already awarded)
    Return DecodeResponse{Decrypted:"Python"}

Client receives: {"decrypted":"Python"}
```

### 3.9 Combined Flow for Flaky + Encrypted Traits

```
Client: POST /sessions/{id}/ask {questionId: "T61"}
  sessionService.AskQuestion(ctx, sessionID, "T61")
    traitDef = {IsEncrypted:true, IsFlaky:true, TraitKey:"training_provider"}

    chaosService.ShouldFail(session, traitDef)
      s.enabled? -> true (assuming chaos is on)
      traitDef.IsFlaky? -> true
      isInChaosWindow(session)? -> depends on timing
      random < 0.6? -> depends on RNG

      CASE 1: ShouldFail returns true
        -> Return degraded response (no encryption, no answer)
        -> Client retries after retryAfterMs

      CASE 2: ShouldFail returns false
        -> session.TargetCandidate.GetTrait("training_provider") -> answer
        -> session.RecordQuestion("T61")
        -> traitDef.IsEncrypted == true
          -> encryptionService.Encrypt(answer) -> "b64:..."
          -> traitAnswer.Encrypted = "b64:..."
        -> Return encrypted TraitAnswer
        -> Client calls POST /decode to get plaintext

        -> If session is in chaos window:
          -> S3 (Resilience) milestone awarded
```

### 3.10 Error Handling in Encryption

- **Encrypt failure**: If [`encryptionService.Encrypt()`](../internal/service/encryption.go:26) returns an error, [`AskQuestion()`](../internal/service/session_service.go:247) returns an error to the caller: `fmt.Errorf("failed to encrypt answer: %w", err)`. This surfaces as an HTTP 400 Bad Request via [`SessionHandler.AskQuestion()`](../internal/handler/session_handler.go:126).
- **Decrypt failure**: If [`encryptionService.Decrypt()`](../internal/service/encryption.go:33) returns an error (e.g., invalid base64), the [`Decode()`](../internal/handler/trait_handler.go:74) handler returns HTTP 400 with `"Failed to decode: <error>"`.
- **Missing PAYLOAD:: prefix**: If the decoded string does not start with `"PAYLOAD::"`, the [`Decrypt()`](../internal/service/encryption.go:47) method returns the raw decoded string without error. This provides graceful handling for edge cases.

### 3.11 Security Considerations

The encryption used is **intentionally simple** (base64 with a known prefix). This is by design for the Guess Who challenge:

- The encoding scheme is discoverable: clients can reverse-engineer it by observing the `"b64:"` prefix.
- The `"PAYLOAD::"` prefix inside the base64 acts as a validation marker but not a security measure.
- The [`HashPassword()`](../internal/service/encryption.go:54) and [`CheckPasswordHash()`](../internal/service/encryption.go:59) methods use `bcrypt` with cost 14 for actual authentication security, but these are unrelated to trait encryption.
- The decode endpoint exists specifically to help clients who want to use the server-side decoding rather than implementing their own.

### 3.12 Questions Endpoint and Trait Visibility

The [`GetQuestions()`](../internal/handler/trait_handler.go:30) handler at `GET /sessions/{sessionId}/questions` exposes trait metadata including the [`Tier`](../internal/domain/trait.go:28) field (which will be `"encrypted"` or `"flaky"` for relevant traits) and the [`Cost`](../internal/domain/trait.go:29) field. However, the [`IsEncrypted`](../internal/domain/trait.go:30) and [`IsFlaky`](../internal/domain/trait.go:31) boolean fields are tagged `json:"-"` and are **never exposed** to clients. Clients must infer encryption behavior from the `tier` field value or by observing the response format (presence of `encrypted` vs `answer` field).

The questions response format is built at [`trait_handler.go:35-42`](../internal/handler/trait_handler.go:35):

```go
question := map[string]interface{}{
    "questionId": trait.QuestionID,
    "traitKey":   trait.TraitKey,
    "type":       trait.Type,
    "cost":       trait.Cost,
    "tier":       trait.Tier,
}
```

This gives clients enough information to identify encrypted traits (tier = `"encrypted"`) and flaky traits (tier = `"flaky"`) without exposing the internal boolean flags.

### 3.13 Key Management

**There are no encryption keys.** The base64 encoding scheme is keyless. The [`encryptionService`](internal/service/encryption.go:19) struct has no fields — it's an empty struct. [`NewEncryptionService()`](internal/service/encryption.go:22) takes no parameters.

The service also provides [`HashPassword()`](internal/service/encryption.go:54) and [`CheckPasswordHash()`](internal/service/encryption.go:59) using **bcrypt** (cost factor 14), but these are for authentication — NOT for trait encryption.

### 3.14 Detailed Function Call Chain

Complete trace for an encrypted trait question (e.g., T43 `favorite_language`):

1. [`SessionHandler.AskQuestion()`](internal/handler/session_handler.go:114) parses `questionId` from body
2. Calls [`sessionService.AskQuestion(ctx, sessionID, req.QuestionID)`](internal/handler/session_handler.go:124)
3. [`sessionService.AskQuestion()`](internal/service/session_service.go:180) loads session from Redis ([line 183](internal/service/session_service.go:183))
4. Retrieves trait definition via [`s.traitCatalog.GetTraitByID(questionID)`](internal/service/session_service.go:193)
5. Checks chaos: [`s.chaosService.ShouldFail(session, traitDef)`](internal/service/session_service.go:199) — for encrypted-only traits (non-flaky), this always returns `false`
6. Gets answer from target: [`session.TargetCandidate.GetTrait(traitDef.TraitKey)`](internal/service/session_service.go:214)
7. Records question in session ([line 220](internal/service/session_service.go:220))
8. Builds [`TraitAnswer`](internal/domain/trait.go:41) struct ([line 238](internal/service/session_service.go:238))
9. **Encryption branch at [line 244](internal/service/session_service.go:244)**: `if traitDef.IsEncrypted`
   - Converts answer to string: `fmt.Sprintf("%v", answer)` ([line 245](internal/service/session_service.go:245))
   - Calls [`s.encryptionService.Encrypt(plaintext)`](internal/service/session_service.go:246)
   - Sets [`traitAnswer.Encrypted = encrypted`](internal/service/session_service.go:251)
10. Returns `traitAnswer` — handler serializes to JSON at [line 134](internal/handler/session_handler.go:134)

### 3.15 Client-Side Decryption

Clients can either:
- Call the server-side `/decode` endpoint (which awards milestone M5)
- Decode locally by reversing the base64 + prefix scheme (but this won't trigger M5)

To decode locally, a client would:
1. Strip the `"b64:"` prefix from the encrypted string
2. Base64-decode the remaining string
3. Strip the `"PAYLOAD::"` prefix from the decoded result
4. The remaining string is the plaintext answer

### 3.16 Vestigial Fields

[`TraitAnswer`](internal/domain/trait.go:41) contains two fields that are defined but **never populated**:
- [`Cipher`](internal/domain/trait.go:46) (`json:"cipher,omitempty"`)
- [`KeyHintID`](internal/domain/trait.go:47) (`json:"keyHintId,omitempty"`)

These appear to be from an earlier design that planned AES-GCM or similar keyed encryption but was not implemented. Since they use `omitempty`, they never appear in JSON responses.

### 3.17 Tests — encryption_test.go

The [`encryption_test.go`](internal/service/encryption_test.go) file validates the encryption service. Key test cases:

1. **TestEncrypt**: Verifies that [`Encrypt()`](internal/service/encryption.go:26) produces the expected `"b64:"` prefixed base64 output for a known input
2. **TestDecrypt**: Verifies that [`Decrypt()`](internal/service/encryption.go:33) correctly reverses the encryption back to plaintext
3. **TestEncryptDecryptRoundTrip**: Verifies that encrypting then decrypting returns the original plaintext
4. **TestDecryptInvalidBase64**: Verifies error handling for malformed base64 input
5. **TestDecryptWithoutPrefix**: Verifies that [`Decrypt()`](internal/service/encryption.go:33) handles input without the `"b64:"` prefix

### 3.18 Encryption Configuration Summary

| Parameter | Location | Description |
|-----------|----------|-------------|
| [`TraitDefinition.IsEncrypted`](internal/domain/trait.go:30) | Domain struct | `bool`, `json:"-"` (hidden from API clients) |
| [`TraitDefinition.Tier`](internal/domain/trait.go:28) | Domain struct | Encrypted traits use [`TierEncrypted`](internal/domain/trait.go:17) (`"encrypted"`) or [`TierFlaky`](internal/domain/trait.go:18) (`"flaky"`) |
| [`TraitDefinition.Cost`](internal/domain/trait.go:29) | Domain struct | Encrypted traits cost `2`; encrypted+flaky traits cost `4` |

**There are NO environment variables or config fields** in [`config.Config`](config/config.go:9) that control encryption. Encryption is always active — there is no feature flag to disable it.

### 3.19 Answer Type Coercion

At [line 245](internal/service/session_service.go:245), `fmt.Sprintf("%v", answer)` coerces `interface{}` to string before encryption. This means:
- String values (e.g., `"Python"`) are passed through as-is
- Boolean values produce `"true"` or `"false"`
- Numeric values produce their string representation

This coercion is important because [`Encrypt()`](internal/service/encryption.go:26) accepts only `string` input, while [`GetTrait()`](internal/domain/candidate.go) returns `interface{}`.