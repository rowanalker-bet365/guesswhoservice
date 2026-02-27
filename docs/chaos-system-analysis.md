# Chaos / Failure-Injection System — Analysis

**Document date:** 2026-02-27
**Branch analysed:** `Deploy-miletsones` (HEAD `92fd103`)

---

## Table of Contents

1. [Overview](#1-overview)
2. [Configuration](#2-configuration)
3. [Chaos Profile](#3-chaos-profile)
4. [Chaos Windows](#4-chaos-windows)
5. [Flaky Traits](#5-flaky-traits)
6. [Decision Flow — ShouldFail](#6-decision-flow--shouldfail)
7. [Degraded Response Format](#7-degraded-response-format)
8. [Session Tracking](#8-session-tracking)
9. [Retry Mechanism](#9-retry-mechanism)
10. [Milestones](#10-milestones)
11. [Sequence Diagram](#11-sequence-diagram)
12. [Pain Points and Observations](#12-pain-points-and-observations)

---

## 1. Overview

The chaos system is a server-side **failure-injection** layer. When active, it deliberately returns degraded (error-like) responses to a subset of question requests, simulating an unreliable third-party data provider. The goal is to teach players to write **resilient client code**: detect the degraded status, respect the retry hint, and re-submit the question after the window has passed.

The chaos system produces a secondary gameplay effect: a stretch milestone (S3 Resilience) is awarded when a player successfully retrieves a valid answer from a **flaky trait** while chaos is currently active. This creates a skill-based reward for observing chaos windows and timing requests accordingly.

The system is entirely server-side. No client code changes are required to observe failures; however, clients that do not handle `"status": "degraded"` will appear to get empty/broken answers during chaos windows.

---

## 2. Configuration

### Environment Variables

All chaos configuration is loaded at startup by `config/config.go:Load()`.

| Environment Variable       | Type   | Default | Description |
|----------------------------|--------|---------|-------------|
| `CHAOS_ENABLED`            | bool   | `false` | Master switch. When `false`, `ShouldFail` always returns `false`. |
| `CHAOS_INTERVAL_SECONDS`   | int    | `240`   | Length of one full chaos cycle in seconds (4 minutes). |
| `CHAOS_WINDOW_SECONDS`     | int    | `90`    | Duration of the "chaos on" portion at the start of each cycle. |

```go
// config/config.go lines 29-31
ChaosEnabled:         getEnvBool("CHAOS_ENABLED", false),
ChaosIntervalSeconds: getEnvInt("CHAOS_INTERVAL_SECONDS", 240),
ChaosWindowSeconds:   getEnvInt("CHAOS_WINDOW_SECONDS", 90),
```

### Wiring at Startup

`guesswhoserviceapi.go` (lines 97–115) creates both services and passes the same values to each:

```go
// guesswhoserviceapi.go lines 97-115
chaosService := service.NewChaosService(cfg.ChaosEnabled)

sessionService := service.NewSessionService(
    ...
    chaosService,
    ...
    service.SessionServiceConfig{
        ChaosEnabled:  cfg.ChaosEnabled,
        ChaosInterval: cfg.ChaosIntervalSeconds,
        ChaosWindow:   cfg.ChaosWindowSeconds,
    },
)
```

The `ChaosService` receives only the `enabled` flag. The interval and window are stored on the `sessionService` and stamped into each session's `ChaosProfile` at session-start time (see Section 3).

### Startup Log

On startup the server logs:

```
configuration loaded  rateLimitEnabled=... chaosEnabled=... chaosInterval=... chaosWindow=...
```

(`guesswhoserviceapi.go` lines 211–215)

---

## 3. Chaos Profile

### Domain Struct

```go
// internal/domain/session.go lines 8-20
type ChaosMode string

const (
    ChaosModeScheduled     ChaosMode = "scheduled"
    ChaosModeProbabilistic ChaosMode = "probabilistic"
)

type ChaosProfile struct {
    Mode            ChaosMode `json:"mode"`
    WindowSeconds   int       `json:"windowSeconds"`
    IntervalSeconds int       `json:"intervalSeconds,omitempty"`
}
```

### Fields

| Field             | JSON key          | Description |
|-------------------|-------------------|-------------|
| `Mode`            | `mode`            | Controls which algorithm is used to decide chaos windows. |
| `WindowSeconds`   | `windowSeconds`   | Duration in seconds of each chaos "on" period. |
| `IntervalSeconds` | `intervalSeconds` | Full cycle length in seconds (`omitempty`, so omitted if zero). |

### Modes

Two modes are declared as constants in `internal/domain/session.go`:

| Mode              | Constant                    | Behaviour |
|-------------------|-----------------------------|-----------|
| `"scheduled"`     | `ChaosModeScheduled`        | **Active and used.** Chaos fires on a repeating timer tied to session creation time. |
| `"probabilistic"` | `ChaosModeProbabilistic`    | **Declared but never used.** `isInChaosWindow` returns `false` for any mode that is not `"scheduled"`. |

### How the Profile Is Stamped onto Sessions

`StartSession` in `internal/service/session_service.go` (lines 87–91) constructs the profile from the config values read at startup:

```go
// internal/service/session_service.go lines 87-91
chaosProfile := domain.ChaosProfile{
    Mode:            domain.ChaosModeScheduled,
    WindowSeconds:   s.chaosWindow,
    IntervalSeconds: s.chaosInterval,
}
```

The mode is **hard-coded** to `ChaosModeScheduled`. The interval and window come from env vars via `SessionServiceConfig`. The profile is then stored inside the `Session` struct and persisted to Redis, making the chaos schedule session-specific and immutable after session creation.

The profile is also returned verbatim in the `POST /sessions/start` response so clients can compute window boundaries client-side if they wish:

```go
// internal/handler/session_handler.go lines 51-57
response := StartSessionResponse{
    SessionID:       session.SessionID,
    ...
    ChaosProfile:    session.ChaosProfile,
}
```

---

## 4. Chaos Windows

### Algorithm

`isInChaosWindow` in `internal/service/chaos.go` (lines 66–87):

```go
func (s *chaosService) isInChaosWindow(session *domain.Session) bool {
    if session.ChaosProfile.Mode != domain.ChaosModeScheduled {
        return false
    }

    elapsed := time.Since(session.CreatedAt).Seconds()
    intervalSeconds := float64(session.ChaosProfile.IntervalSeconds)
    windowSeconds := float64(session.ChaosProfile.WindowSeconds)

    if intervalSeconds == 0 {
        intervalSeconds = 240 // Default 4 minutes
    }
    if windowSeconds == 0 {
        windowSeconds = 90 // Default 90 seconds
    }

    // Calculate position within the current interval
    positionInInterval := float64(int(elapsed) % int(intervalSeconds))

    // Check if we're in the chaos window (starts at the beginning of each interval)
    return positionInInterval < windowSeconds
}
```

### Step-by-Step Calculation

Given defaults (`interval=240`, `window=90`):

1. `elapsed` = seconds since `session.CreatedAt`
2. `positionInInterval` = `elapsed mod 240` — where in the current 4-minute cycle we are
3. `return positionInInterval < 90` — the first 90 seconds of every cycle is "chaos on"

### Timeline (default values)

```
Session created
│
├─ T+0s   ──────────────── Chaos ON (90s) ──────────────── T+90s
│
├─ T+90s  ──────────────── Chaos OFF (150s) ─────────────── T+240s
│
├─ T+240s ──────────────── Chaos ON (90s) ──────────────── T+330s
│
├─ T+330s ──────────────── Chaos OFF (150s) ─────────────── T+480s
│
└─ (repeats indefinitely)
```

### Default Fallback in isInChaosWindow

The function has in-place defaults (`240` and `90`) for the case where the session's `ChaosProfile` has zero values. In practice this cannot happen with current wiring because `config.go` also defaults both to the same values, and the profile is stamped at session-start — but the fallback guards against the profile being persisted with zeroes from an older schema.

---

## 5. Flaky Traits

Only traits with `IsFlaky: true` are eligible for chaos failure. There are **6 flaky traits** out of the full catalog of 64. They span two groups and two cost tiers.

### Group E — Tech Preferences / Habits

| Question ID | Trait Key          | Question                   | Type    | Tier    | Cost | Also Encrypted? |
|-------------|--------------------|----------------------------|---------|---------|------|-----------------|
| T49         | `two_factor`       | Uses 2FA?                  | boolean | flaky   | 3    | No              |
| T50         | `password_manager` | Uses password manager?     | boolean | flaky   | 3    | No              |

### Group G — Security / Verification

| Question ID | Trait Key           | Question                      | Type    | Tier    | Cost | Also Encrypted? |
|-------------|---------------------|-------------------------------|---------|---------|------|-----------------|
| T61         | `training_provider` | Training provider?            | enum    | flaky   | 4    | Yes             |
| T62         | `id_verified`       | Identity verified?            | boolean | flaky   | 4    | No              |
| T63         | `eligibility`       | Eligible for withdrawal?      | boolean | flaky   | 4    | No              |
| T64         | `risk_band`         | Risk band?                    | enum    | flaky   | 4    | Yes             |

Defined in `internal/service/trait_catalog.go` lines 120–139.

**Notable:** T61 (`training_provider`) and T64 (`risk_band`) have **both** `IsFlaky: true` and `IsEncrypted: true`. If a request for one of these traits successfully passes through the chaos check, the answer is additionally encrypted before being returned. This means a client must handle both failure injection and the encryption layer to retrieve data from these traits.

---

## 6. Decision Flow — ShouldFail

`ShouldFail` is called once per `AskQuestion` invocation in `internal/service/session_service.go` (line 211).

```go
// internal/service/session_service.go line 211
if s.chaosService.ShouldFail(session, traitDef) {
```

### Full ShouldFail Logic

```go
// internal/service/chaos.go lines 33-52
func (s *chaosService) ShouldFail(session *domain.Session, traitDef *domain.TraitDefinition) bool {
    if !s.enabled {
        return false  // Gate 1: chaos master switch
    }

    if !traitDef.IsFlaky {
        return false  // Gate 2: only flaky traits can fail
    }

    if !s.isInChaosWindow(session) {
        return false  // Gate 3: must be inside an active chaos window
    }

    // Gate 4: probabilistic — 60% chance of failure during the window
    failureRate := 0.6
    return s.rng.Float64() < failureRate
}
```

### Decision Tree

```
ShouldFail(session, traitDef)
│
├── chaos enabled?
│     No  → return false (no failure)
│     Yes ↓
│
├── traitDef.IsFlaky?
│     No  → return false (no failure)
│     Yes ↓
│
├── isInChaosWindow(session)?
│     No  → return false (no failure)
│     Yes ↓
│
└── random float < 0.60?
      No  (40%) → return false (lucky — normal answer returned)
      Yes (60%) → return true  (degraded response returned)
```

### Conditions Summary

| Condition                           | Effect when false       |
|-------------------------------------|-------------------------|
| `ChaosService.enabled == true`      | Never fail              |
| `traitDef.IsFlaky == true`          | Never fail              |
| Currently inside chaos window       | Never fail              |
| `rand.Float64() < 0.60`             | Fail (degraded)         |

**Effective failure rate:** Only 6 of 64 traits are flaky. During a chaos window those 6 traits have a 60% per-request chance of returning a degraded response. Outside a window they always succeed.

---

## 7. Degraded Response Format

When `ShouldFail` returns `true`, `AskQuestion` returns a `domain.TraitAnswer` with the following fields populated:

```go
// internal/service/session_service.go lines 217-222
return &domain.TraitAnswer{
    QuestionID: questionID,
    TraitKey:   traitDef.TraitKey,
    Status:     "degraded",
    RetryAfter: s.chaosService.GetRetryDelay(),
}, nil
```

### TraitAnswer Struct (relevant fields)

```go
// internal/domain/trait.go lines 41-50
type TraitAnswer struct {
    QuestionID string      `json:"questionId"`
    TraitKey   string      `json:"traitKey"`
    Answer     interface{} `json:"answer,omitempty"`      // omitted on degraded
    Encrypted  string      `json:"encrypted,omitempty"`   // omitted on degraded
    Cipher     string      `json:"cipher,omitempty"`      // omitted on degraded
    KeyHintID  string      `json:"keyHintId,omitempty"`   // omitted on degraded
    Status     string      `json:"status,omitempty"`      // "degraded"
    RetryAfter int         `json:"retryAfterMs,omitempty"` // milliseconds
}
```

### Example JSON response on degraded path

```json
{
  "questionId": "T62",
  "traitKey": "id_verified",
  "status": "degraded",
  "retryAfterMs": 1247
}
```

Key observations:

- The HTTP status code is still **200 OK**. A degraded response is not an HTTP error; it is a business-level failure signal.
- `answer`, `encrypted`, `cipher`, and `keyHintId` are all absent (`omitempty`).
- `retryAfterMs` is a millisecond value despite the field name using the `Ms` suffix — its JSON key is `retryAfterMs` (defined on line 49 of `internal/domain/trait.go`).

---

## 8. Session Tracking

### Failure Counters

`internal/domain/session.go` (lines 35–37) declares three separate failure counters on the session:

```go
FailedRequests int `json:"failedRequests"`
Timeouts       int `json:"timeouts"`
Unhandled5xx   int `json:"unhandled5xx"`
```

`IncrementFailure` (lines 71–80) routes increments by a string key:

```go
func (s *Session) IncrementFailure(failureType string) {
    switch failureType {
    case "failed":
        s.FailedRequests++
    case "timeout":
        s.Timeouts++
    case "5xx":
        s.Unhandled5xx++
    }
}
```

### What Gets Incremented on Chaos Failure

When `ShouldFail` returns `true`, the caller uses:

```go
// internal/service/session_service.go lines 212-213
session.IncrementFailure("failed")
s.dbStore.WriteSession(ctx, session)
```

Only `FailedRequests` is ever incremented by the chaos system. `Timeouts` and `Unhandled5xx` exist to allow players to classify their own client-side failure types (presumably by calling a future API endpoint), but as of the current codebase they are **never incremented by any server code**.

### Counters in the GuessResult Response

When a guess is submitted, `SessionStats` is returned:

```go
// internal/domain/score.go lines 19-24
type SessionStats struct {
    TimeSeconds    int `json:"timeSeconds"`
    QuestionsAsked int `json:"questionsAsked"`
    FailedRequests int `json:"failedRequests"`
}
```

`FailedRequests` is exposed here, giving players visibility into how many chaos hits they took in a session.

### Reliability Penalty (Scoring Impact)

Accumulated failures directly reduce the score at guess time via `calculateReliabilityPenalty` in `internal/service/scoring.go` (lines 48–53):

```go
func (s *scoringService) calculateReliabilityPenalty(session *domain.Session) int {
    penalty := (5 * session.FailedRequests) +
               (2 * session.Timeouts) +
               (10 * session.Unhandled5xx)
    return penalty
}
```

| Counter          | Penalty per occurrence |
|------------------|------------------------|
| `FailedRequests` | 5 points               |
| `Timeouts`       | 2 points               |
| `Unhandled5xx`   | 10 points              |

With chaos enabled and default 60% failure rate, a player who asks a flaky question 10 times during a window and gets 6 degraded responses will lose 30 points from their final score. The penalty is deducted from the scoring breakdown as `ReliabilityPenalty` and cannot reduce the total score below zero (clamped at `session_service.go` line 316).

---

## 9. Retry Mechanism

`GetRetryDelay` in `internal/service/chaos.go` (lines 54–57):

```go
func (s *chaosService) GetRetryDelay() int {
    // Random retry delay between 500ms and 2000ms
    return 500 + s.rng.Intn(1500)
}
```

- The delay is in **milliseconds**.
- It is uniformly random in the range `[500, 1999]` ms.
- A fresh random value is generated for every degraded response.
- It is returned in `TraitAnswer.RetryAfter`, serialised as `"retryAfterMs"` in JSON.

The retry delay is a **hint** only. The server does not enforce a cooldown; a client can re-submit the same question immediately. The degraded response will be re-evaluated by `ShouldFail` on the next request — which may fail again (60% chance) if still inside the chaos window.

The intended client pattern is:

1. Receive `"status": "degraded"`.
2. Wait at least `retryAfterMs` milliseconds.
3. Re-submit the same question.
4. If still in the chaos window, repeat.
5. If the window has passed (position in interval >= windowSeconds), the request will always succeed.

---

## 10. Milestones

Two milestones interact directly with the chaos system:

### M2 — First Successful Question

```go
// internal/domain/milestone.go line 35
{MilestoneM2, "First Successful Question", "Received your first non-degraded answer", CoreMilestoneScore},
```

M2 is awarded when the first non-degraded answer is returned (i.e., `ShouldFail` returned `false`). The description explicitly calls out the "non-degraded" qualifier, implying chaos is expected to be active and the player must navigate past it.

Awarded at `internal/service/session_service.go` line 236, inside the block that only executes when `ShouldFail` returned `false`.

Score bonus: **1,000 points**.

### S3 — Resilience (stretch)

```go
// internal/domain/milestone.go line 40
{MilestoneS3, "Resilience", "Got a valid answer from a flaky trait during a chaos window", StretchMilestoneScore},
```

S3 is the chaos-specific stretch milestone. It is awarded when:

- The answered trait is flaky (`traitDef.IsFlaky == true`), **and**
- The session is currently inside a chaos window (`chaosService.IsInChaosWindow(session) == true`), **and**
- The answer was not degraded (i.e., the 40% "lucky" path through `ShouldFail`)

```go
// internal/service/session_service.go lines 245-247
if traitDef.IsFlaky && s.chaosService.IsInChaosWindow(session) {
    s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneS3)
}
```

Score bonus: **2,000 points**.

This milestone can only be earned if chaos is enabled. If chaos is disabled, `IsInChaosWindow` still evaluates the session's `ChaosProfile.Mode` — and since the profile is always stamped with `ChaosModeScheduled`, the window calculation runs regardless of the `enabled` flag. See [Section 12](#12-pain-points-and-observations) for the implication.

---

## 11. Sequence Diagram

```
Client                   SessionHandler          SessionService           ChaosService
  |                           |                       |                       |
  |-- POST /sessions/{id}/ask |                       |                       |
  |   { questionId: "T62" }   |                       |                       |
  |                           |                       |                       |
  |                           |-- AskQuestion() ------>|                       |
  |                           |                       |                       |
  |                           |                       |-- ReadSession() -----> Redis
  |                           |                       |<-- session ----------- Redis
  |                           |                       |                       |
  |                           |                       |-- GetTraitByID("T62") |
  |                           |                       |<-- traitDef           |
  |                           |                       |  (IsFlaky=true)       |
  |                           |                       |                       |
  |                           |                       |-- ShouldFail(session, traitDef) -->|
  |                           |                       |                       |
  |                           |                       |   [Gate 1] enabled?   |
  |                           |                       |   [Gate 2] IsFlaky?   |
  |                           |                       |   [Gate 3] InWindow?  |
  |                           |                       |   [Gate 4] rand<0.60? |
  |                           |                       |                       |
  |                           |     ---- PATH A: ShouldFail returns true -----+
  |                           |                       |<-- true               |
  |                           |                       |                       |
  |                           |                       |-- IncrementFailure("failed")
  |                           |                       |-- WriteSession() ----> Redis
  |                           |                       |                       |
  |                           |                       |-- GetRetryDelay() --> rng.Intn()
  |                           |                       |<-- retryMs            |
  |                           |                       |                       |
  |                           |<-- TraitAnswer --------|                       |
  |                           |  { status:"degraded",  |                       |
  |                           |    retryAfterMs:1247 } |                       |
  |<-- 200 OK { degraded } ---|                       |                       |
  |                           |                       |                       |
  |   (client waits retryMs)  |                       |                       |
  |                           |                       |                       |
  |     ---- PATH B: ShouldFail returns false --------+                       |
  |                           |                       |<-- false              |
  |                           |                       |                       |
  |                           |                       |-- GetTrait("id_verified")
  |                           |                       |-- RecordQuestion("T62")
  |                           |                       |-- WriteSession() ----> Redis
  |                           |                       |                       |
  |                           |                       |-- AwardIfAbsent(M2)   |
  |                           |                       |                       |
  |                           |                       |-- IsInChaosWindow()? -->|
  |                           |                       |<-- true/false          |
  |                           |                       |                       |
  |                           |                       |-- (if flaky+window) AwardIfAbsent(S3)
  |                           |                       |                       |
  |                           |<-- TraitAnswer --------|                       |
  |                           |  { answer: true }      |                       |
  |<-- 200 OK { answer } -----|                       |                       |
```

---

## 12. Pain Points and Observations

### 1. `ChaosModeProbabilistic` is declared but unreachable

`internal/domain/session.go` defines `ChaosModeProbabilistic = "probabilistic"` (line 12), but `isInChaosWindow` (`chaos.go` line 67) returns `false` for any mode that is not `ChaosModeScheduled`. There is no code path that sets the mode to `"probabilistic"`, and even if a session were manually patched in Redis to use it, the window check would always return `false`, disabling chaos entirely for that session. The constant is dead code.

### 2. S3 can be awarded when chaos is disabled

`IsInChaosWindow` checks the session's `ChaosProfile.Mode` and computes the window based on the stored `IntervalSeconds` / `WindowSeconds` — it does not check `s.enabled`. The `ChaosProfile` is always stamped with `ChaosModeScheduled` and non-zero interval/window values regardless of whether `CHAOS_ENABLED=false`. This means if chaos is disabled globally:

- `ShouldFail` returns `false` at Gate 1 — no degraded responses are generated.
- But `IsInChaosWindow` can still return `true` during the scheduled window period.
- Therefore S3 can be awarded even in a no-chaos deployment, as long as the player asks a flaky question during the computed window period and happens to get a successful answer (which they always will, since `ShouldFail` is `false`).

This is likely unintended. A fix would be to add an `s.enabled` guard inside `IsInChaosWindow`:

```go
func (s *chaosService) isInChaosWindow(session *domain.Session) bool {
    if !s.enabled {
        return false  // proposed guard
    }
    // ... rest of logic
}
```

### 3. `Timeouts` and `Unhandled5xx` counters are never incremented

`Session` has three failure counters (`FailedRequests`, `Timeouts`, `Unhandled5xx`). The reliability penalty formula in `scoring.go` uses all three. However, only `FailedRequests` is ever incremented by server code (`IncrementFailure("failed")`). The `"timeout"` and `"5xx"` branches of `IncrementFailure` are unreachable with current code. The scoring weights (2 and 10 respectively) and the declared fields are effectively dead. If there is a plan to expose an endpoint for client-side failure reporting, this infrastructure is ready; otherwise the dead counters add confusion.

### 4. Retry delay is a hint, not enforced

`GetRetryDelay` returns a random millisecond value. The server does not enforce any cooldown between requests. A client can immediately re-submit a question and will be re-evaluated by `ShouldFail`. This is intentional (it allows players to learn through rapid iteration) but should be documented in the API contract so players do not assume the server will rate-limit re-submissions on their behalf.

### 5. Degraded response returns HTTP 200, not 503

The degraded response is returned with HTTP 200 OK. Some players may expect a 503 Service Unavailable or 429 Too Many Requests for a "failed" dependency scenario. This design choice (200 + body inspection) is valid for a game mechanic, but it diverges from real-world service degradation patterns and could cause confusion if players model their client error-handling on HTTP status codes rather than the `"status"` field.

### 6. `ShouldFail` uses a shared, unsynchronised `*rand.Rand`

`chaosService.rng` is a `*rand.Rand` initialised once at startup. In Go, `math/rand.Rand` is **not safe for concurrent use**. Under concurrent requests this can cause a data race. The standard library's top-level `rand.Float64()` is safe (it uses a mutex internally), but a `*rand.Rand` instance is not. Under load this can cause a panic or corrupted float values. The fix is either to use `rand.Float64()` (global, mutex-protected) or to guard `s.rng` with a `sync.Mutex`.

### 7. The `IntervalSeconds` field is `omitempty`

```go
IntervalSeconds int `json:"intervalSeconds,omitempty"`
```

If `IntervalSeconds` is zero (which it would be if the env var is unset and the default wasn't applied before serialisation), the field is omitted from JSON, and `isInChaosWindow` falls back to its hard-coded `240` default. This dual-default pattern (one in `config.go`, one in `chaos.go`) means the session's stored profile does not always accurately reflect the window that will be calculated. Prefer storing the resolved values explicitly and removing the fallback inside `isInChaosWindow`.

### 8. No chaos-specific test file exists

There is no `chaos_test.go` or equivalent. The scoring tests in `scoring_test.go` do exercise `FailedRequests` through the penalty calculation (line 54–67 of that file), but there is no test for:

- `ShouldFail` returning `false` when chaos is disabled
- `ShouldFail` returning `false` for non-flaky traits
- `isInChaosWindow` correctly cycling
- The probabilistic failure rate being approximately 60%
- The retry delay range

This is a gap given the chaos system is a core gameplay mechanic.
