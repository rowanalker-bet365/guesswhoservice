# Flaky Traits — Deep-Dive Analysis

> Document produced by analysis of the `Deploy-milestones` branch (commit `92fd103`).
> All file paths are relative to the repository root `guesswhoservice/`.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Trait Tier System](#2-trait-tier-system)
3. [Complete Flaky Trait Inventory](#3-complete-flaky-trait-inventory)
4. [How Flaky Traits Fail — The Decision Tree](#4-how-flaky-traits-fail--the-decision-tree)
5. [Interaction with the Chaos System](#5-interaction-with-the-chaos-system)
6. [Interaction with Encryption (T61 and T64)](#6-interaction-with-encryption-t61-and-t64)
7. [Degraded Response Format](#7-degraded-response-format)
8. [Retry Behaviour](#8-retry-behaviour)
9. [Scoring Impact](#9-scoring-impact)
10. [Milestone Connections](#10-milestone-connections)
11. [Client-Side Expectations](#11-client-side-expectations)
12. [Current Pain Points and Observations](#12-current-pain-points-and-observations)

---

## 1. Overview

**Flaky traits** are a category of trait questions that may refuse to return a real answer under certain conditions, responding instead with a *degraded* response. Rather than a trait value, the client receives a status string (`"degraded"`) and a suggested `retryAfterMs` delay.

The purpose is to simulate unreliable external data sources — verification bureaus, risk-banding APIs, identity-check services — and to require client implementations to handle transient failures gracefully.

### How flaky traits differ from other traits

| Dimension | Basic | Encrypted | Flaky | Flaky + Encrypted |
|---|---|---|---|---|
| Tiers | `basic` | `encrypted` | `flaky` | `flaky` |
| Cost (score points) | 1 | 2 | 3 or 4 | 4 |
| Can return a degraded response | No | No | Yes | Yes |
| Answer is base64-encoded | No | Yes | No | Yes (on success) |
| `IsEncrypted` flag | `false` | `true` | `false` | `true` |
| `IsFlaky` flag | `false` | `false` | `true` | `true` |
| Requires chaos mode to fail | N/A | N/A | Yes | Yes |

Both `IsEncrypted` and `IsFlaky` are defined on `domain.TraitDefinition` but are tagged with `json:"-"`, meaning they are never exposed to clients via the API. Clients must infer flakiness from the `"flaky"` tier value returned by `GET /sessions/{sessionId}/questions`.

```go
// internal/domain/trait.go  lines 22-32
type TraitDefinition struct {
    QuestionID  string    `json:"questionId"`
    TraitKey    string    `json:"traitKey"`
    Question    string    `json:"question"`
    Type        TraitType `json:"type"`
    Values      []string  `json:"values,omitempty"`
    Tier        TraitTier `json:"tier"`
    Cost        int       `json:"cost"`
    IsEncrypted bool      `json:"-"`  // server-only
    IsFlaky     bool      `json:"-"`  // server-only
}
```

---

## 2. Trait Tier System

There are three tiers defined in `internal/domain/trait.go`:

```go
// internal/domain/trait.go  lines 15-19
const (
    TierBasic     TraitTier = "basic"
    TierEncrypted TraitTier = "encrypted"
    TierFlaky     TraitTier = "flaky"
)
```

### Tier descriptions

**`basic` — cost 1**
Plain text answer. No encoding, no failure injection. 52 of the 64 traits are basic. A successful response carries the answer directly in the `answer` field.

**`encrypted` — cost 2**
The answer is base64-encoded with a `PAYLOAD::` prefix before encoding, resulting in a `b64:<base64string>` value in the `encrypted` field. The client must call `POST /sessions/{sessionId}/decode` to retrieve the plaintext. These traits never fail due to chaos. There are 8 encrypted-only traits (T29, T36, T37, T43, T48, T56, and two others).

**`flaky` — cost 3 or 4**
These traits sit behind the chaos injection layer. When chaos is active and the session falls inside a chaos window, there is a 60% chance the request is rejected with a degraded response. There are 6 flaky traits total:

- **Cost 3, not encrypted**: T49, T50 (Tech Preferences category)
- **Cost 4, not encrypted**: T62, T63 (Security/Verification category)
- **Cost 4, also encrypted**: T61, T64 (Security/Verification category)

Note: there is **no standalone `"flaky+encrypted"` tier value** — both T61 and T64 carry `Tier: domain.TierFlaky` and `IsEncrypted: true`. The tier label seen by clients is simply `"flaky"`.

---

## 3. Complete Flaky Trait Inventory

All 6 flaky traits are defined in `internal/service/trait_catalog.go` lines 120-139.

### Group E — Tech Preferences / Habits (cost 3, not encrypted)

| ID | TraitKey | Question | Type | Cost | IsEncrypted |
|---|---|---|---|---|---|
| T49 | `two_factor` | Uses 2FA? | boolean | 3 | false |
| T50 | `password_manager` | Uses password manager? | boolean | 3 | false |

```go
// internal/service/trait_catalog.go  lines 120-121
traits["T49"] = domain.TraitDefinition{..., TraitKey: "two_factor",        ..., Tier: domain.TierFlaky, Cost: 3, IsEncrypted: false, IsFlaky: true}
traits["T50"] = domain.TraitDefinition{..., TraitKey: "password_manager",  ..., Tier: domain.TierFlaky, Cost: 3, IsEncrypted: false, IsFlaky: true}
```

### Group G — Security / Verification (cost 4, mixed encryption)

```go
// internal/service/trait_catalog.go  lines 136-139
traits["T61"] = domain.TraitDefinition{..., TraitKey: "training_provider", ..., Tier: domain.TierFlaky, Cost: 4, IsEncrypted: true,  IsFlaky: true}
traits["T62"] = domain.TraitDefinition{..., TraitKey: "id_verified",       ..., Tier: domain.TierFlaky, Cost: 4, IsEncrypted: false, IsFlaky: true}
traits["T63"] = domain.TraitDefinition{..., TraitKey: "eligibility",       ..., Tier: domain.TierFlaky, Cost: 4, IsEncrypted: false, IsFlaky: true}
traits["T64"] = domain.TraitDefinition{..., TraitKey: "risk_band",         ..., Tier: domain.TierFlaky, Cost: 4, IsEncrypted: true,  IsFlaky: true}
```

| ID | TraitKey | Question | Type | Values | Cost | IsEncrypted |
|---|---|---|---|---|---|---|
| T61 | `training_provider` | Training provider? | enum | A, B, C, D | 4 | **true** |
| T62 | `id_verified` | Identity verified? | boolean | — | 4 | false |
| T63 | `eligibility` | Eligible for withdrawal? | boolean | — | 4 | false |
| T64 | `risk_band` | Risk band? | enum | low, medium, high | 4 | **true** |

---

## 4. How Flaky Traits Fail — The Decision Tree

The entry point for every question is `AskQuestion` in `internal/service/session_service.go`. The failure check happens before any answer is looked up.

```
POST /sessions/{sessionId}/ask  { "questionId": "T61" }
         |
         v
  session_service.AskQuestion()
         |
         +-- chaosService.ShouldFail(session, traitDef)
                    |
                    +-- chaos enabled? (cfg.ChaosEnabled)
                    |         NO  --> return false (no failure)
                    |
                    +-- traitDef.IsFlaky?
                    |         NO  --> return false (basic/encrypted traits never fail)
                    |
                    +-- isInChaosWindow(session)?
                    |         NO  --> return false (outside window, flaky traits succeed)
                    |
                    +-- rand.Float64() < 0.60?
                              YES --> return true  (60% failure rate inside window)
                              NO  --> return false (40% success rate inside window)
         |
         +-- ShouldFail == true?
         |         YES --> session.IncrementFailure("failed")
         |               save session
         |               return TraitAnswer{ Status: "degraded", RetryAfter: <500-2000ms> }
         |
         +-- ShouldFail == false
                   --> look up answer from TargetCandidate.GetTrait(traitDef.TraitKey)
                   --> session.RecordQuestion(questionID)  (counted toward question bonus)
                   --> award applicable milestones (M2, M3, S3)
                   --> encrypt if IsEncrypted, else return plain answer
```

The critical path in code:

```go
// internal/service/session_service.go  lines 211-223
if s.chaosService.ShouldFail(session, traitDef) {
    session.IncrementFailure("failed")
    s.dbStore.WriteSession(ctx, session)

    logging.Warn(ctx, "chaos: degraded response", "questionId", questionID)

    return &domain.TraitAnswer{
        QuestionID: questionID,
        TraitKey:   traitDef.TraitKey,
        Status:     "degraded",
        RetryAfter: s.chaosService.GetRetryDelay(),
    }, nil
}
```

### Key behavioural points

- **A degraded response is an HTTP 200**, not a 4xx or 5xx. The question call "succeeds" at the transport layer; the degradation is in the payload.
- **The question is NOT recorded** in `session.QuestionsAsked` when a degraded response is returned. The `RecordQuestion` call happens after the chaos check.
- **The session's `FailedRequests` counter IS incremented** (`session.IncrementFailure("failed")`), which feeds directly into the reliability penalty at score time.
- **No score is deducted immediately** on a degraded response; the cost is indirect, via the reliability penalty applied at guess time.

---

## 5. Interaction with the Chaos System

The chaos system is implemented in `internal/service/chaos.go`. The only chaos mode currently wired up is `ChaosModeScheduled`.

### How a chaos window is calculated

```go
// internal/service/chaos.go  lines 66-87
func (s *chaosService) isInChaosWindow(session *domain.Session) bool {
    if session.ChaosProfile.Mode != domain.ChaosModeScheduled {
        return false
    }

    elapsed := time.Since(session.CreatedAt).Seconds()
    intervalSeconds := float64(session.ChaosProfile.IntervalSeconds)
    windowSeconds   := float64(session.ChaosProfile.WindowSeconds)

    if intervalSeconds == 0 { intervalSeconds = 240 } // default 4 minutes
    if windowSeconds   == 0 { windowSeconds   = 90  } // default 90 seconds

    positionInInterval := float64(int(elapsed) % int(intervalSeconds))
    return positionInInterval < windowSeconds
}
```

**Timeline example** with the defaults (`interval=240s`, `window=90s`):

```
Session age:   0s ──────────── 90s ──────────────── 240s ──────────── 330s ──────── 480s
               [  CHAOS WINDOW  ]   [  safe zone  ]  [  CHAOS WINDOW  ]  [  safe  ]
               <- 60% fail rate ->                   <- 60% fail rate ->
```

The window always starts at the beginning of each interval — it is the first `windowSeconds` of every `intervalSeconds` cycle.

### Environment variables controlling chaos

```
CHAOS_ENABLED=true|false        (default: false)
CHAOS_INTERVAL_SECONDS=<int>    (default: 240)
CHAOS_WINDOW_SECONDS=<int>      (default: 90)
```

These are loaded in `config/config.go` lines 29-31 and passed through to `NewChaosService` and `SessionServiceConfig`.

### Can a flaky trait succeed during a chaos window?

**Yes.** `ShouldFail` only returns `true` in 60% of cases inside a window. In the remaining 40% the trait resolves normally. This is intentional: the chaos window makes failures more likely, not certain.

### Can a flaky trait fail outside a chaos window?

**No.** If `isInChaosWindow` returns `false`, `ShouldFail` returns `false` immediately, regardless of the 60% roll. Outside a chaos window, all flaky traits behave identically to basic traits.

### What if chaos is disabled?

If `CHAOS_ENABLED=false` (the default), `ShouldFail` returns `false` at the first check and the whole chaos pathway is bypassed. Flaky traits then always return a successful answer, as if they were basic traits.

---

## 6. Interaction with Encryption (T61 and T64)

T61 (`training_provider`) and T64 (`risk_band`) are both `IsFlaky: true` AND `IsEncrypted: true`. The two systems interact in a strict sequence: chaos runs first, encryption runs only on success.

### Failure path (degraded response)

```
AskQuestion("T61")
  |
  +-- ShouldFail() == true
        --> return TraitAnswer{
               QuestionID: "T61",
               TraitKey:   "training_provider",
               Status:     "degraded",
               RetryAfter: <500-2000>,
               // Answer, Encrypted, Cipher, KeyHintID are all absent
            }
```

Encryption is **never attempted** on a degraded path. The `Encrypted`, `Cipher`, and `KeyHintID` fields are all empty strings and are omitted from the JSON (`omitempty`).

### Success path (encrypted answer)

```
AskQuestion("T61")
  |
  +-- ShouldFail() == false
  +-- GetTrait("training_provider") --> e.g. "B"
  +-- RecordQuestion("T61")
  +-- Award M2, M3 milestones as applicable
  +-- Award S3 milestone if IsInChaosWindow (40% success inside window)
  |
  +-- traitDef.IsEncrypted == true
        --> encryptionService.Encrypt("B")
            --> payload = "PAYLOAD::B"
            --> encoded = base64("PAYLOAD::B")
            --> return "b64:<encoded>"
        --> return TraitAnswer{
               QuestionID: "T61",
               TraitKey:   "training_provider",
               Encrypted:  "b64:UEFZTE9BRDo6Qg==",
               // Answer, Status, RetryAfter are all absent
            }
```

The client must then decode the `encrypted` field via `POST /sessions/{sessionId}/decode`.

### Encryption implementation

```go
// internal/service/encryption.go  lines 26-31
func (s *encryptionService) Encrypt(plaintext string) (string, error) {
    payload := fmt.Sprintf("PAYLOAD::%s", plaintext)
    encoded := base64.StdEncoding.EncodeToString([]byte(payload))
    return fmt.Sprintf("b64:%s", encoded), nil
}
```

This is standard Base64 — not cryptographic. Any client can decode it directly without needing the decode endpoint. However, calling `POST /sessions/{sessionId}/decode` awards milestone M5.

---

## 7. Degraded Response Format

A degraded response is a normal HTTP `200 OK` with the following JSON body.

### Degraded response (all flaky traits including encrypted ones)

```json
{
  "questionId": "T61",
  "traitKey":   "training_provider",
  "status":     "degraded",
  "retryAfterMs": 1243
}
```

### Normal response — plain flaky trait (T49, T50, T62, T63)

```json
{
  "questionId": "T49",
  "traitKey":   "two_factor",
  "answer":     true
}
```

### Normal response — encrypted flaky trait (T61, T64)

```json
{
  "questionId": "T61",
  "traitKey":   "training_provider",
  "encrypted":  "b64:UEFZTE9BRDo6Qg=="
}
```

### Field presence summary

| Field | Degraded | Normal (plain) | Normal (encrypted) |
|---|---|---|---|
| `questionId` | always | always | always |
| `traitKey` | always | always | always |
| `status` | `"degraded"` | absent | absent |
| `retryAfterMs` | non-zero integer | absent | absent |
| `answer` | absent | present | absent |
| `encrypted` | absent | absent | `b64:...` string |
| `cipher` | absent | absent | absent* |
| `keyHintId` | absent | absent | absent* |

*`cipher` and `keyHintId` exist in the `TraitAnswer` struct but are never populated by the current server code. They are reserved fields.

The `TraitAnswer` struct is the source of truth for all field names:

```go
// internal/domain/trait.go  lines 41-50
type TraitAnswer struct {
    QuestionID string      `json:"questionId"`
    TraitKey   string      `json:"traitKey"`
    Answer     interface{} `json:"answer,omitempty"`
    Encrypted  string      `json:"encrypted,omitempty"`
    Cipher     string      `json:"cipher,omitempty"`
    KeyHintID  string      `json:"keyHintId,omitempty"`
    Status     string      `json:"status,omitempty"`
    RetryAfter int         `json:"retryAfterMs,omitempty"`
}
```

---

## 8. Retry Behaviour

When a degraded response is returned, the `retryAfterMs` field carries a suggested wait time before retrying.

```go
// internal/service/chaos.go  lines 54-57
func (s *chaosService) GetRetryDelay() int {
    // Random retry delay between 500ms and 2000ms
    return 500 + s.rng.Intn(1500)
}
```

**Range**: 500 ms to 1999 ms (inclusive).

**Distribution**: Uniform random. There is no jitter strategy beyond this; every degraded response picks a new random value.

**Consistency**: The delay is **not** correlated with the chaos window timing. A degraded response at second 0 of a window gets the same statistical distribution as one at second 89. The delay does not tell the client how long the chaos window will last.

**Important**: The `retryAfterMs` value is advisory only. The server will not enforce it. If the client retries immediately, the chaos window and the 60% probability are still the governing factors. If the client is still in the same chaos window when it retries, it faces the same 60% failure probability again.

---

## 9. Scoring Impact

### Trait cost

A question asked against a flaky trait costs more score points than a basic or encrypted question:

| Tier | Cost |
|---|---|
| basic | 1 |
| encrypted | 2 |
| flaky (T49, T50) | 3 |
| flaky (T61–T64) | 4 |

However, the cost here is the **question cost** in the question bonus formula — it is separate from the reliability penalty. Looking at the scoring service:

```go
// internal/service/scoring.go  lines 39-45
func (s *scoringService) calculateQuestionBonus(session *domain.Session) int {
    questionsAsked := session.GetQuestionsAskedCount()
    bonus := 300 - (20 * questionsAsked)
    if bonus < 0 {
        bonus = 0
    }
    return bonus
}
```

The question bonus deducts 20 points per question asked. A degraded response does **not** increment `QuestionsAsked` (that counter only goes up when `RecordQuestion` is called, which happens after the chaos check). A degraded question therefore does **not** consume any of the 300-point question bonus budget.

### Reliability penalty

Every degraded response **does** increment `session.FailedRequests`:

```go
// internal/service/session_service.go  line 212
session.IncrementFailure("failed")
```

At score time, each failed request costs 5 points:

```go
// internal/service/scoring.go  lines 48-52
func (s *scoringService) calculateReliabilityPenalty(session *domain.Session) int {
    penalty := (5 * session.FailedRequests) +
               (2 * session.Timeouts)       +
               (10 * session.Unhandled5xx)
    return penalty
}
```

So calling a flaky trait and receiving a degraded response costs the team 5 points at guess time. Calling it again (retry) and receiving another degraded response costs another 5 points.

### Wrong guess penalty (separate from flaky traits)

For completeness: a wrong guess always deducts 200 points immediately, regardless of how many flaky trait calls were made.

### Example score calculation

Session: correct guess, 60 seconds elapsed, 5 successful questions, 2 degraded responses.

```
Base:                1000
Time bonus:          600 - 60 = 540
Question bonus:      300 - (20 × 5) = 200  (degraded calls don't count)
Reliability penalty: 5 × 2 = 10
Total:               1000 + 540 + 200 - 10 = 1730
```

---

## 10. Milestone Connections

### S3 — Resilience (stretch milestone, 2000 points)

This is the milestone most directly tied to flaky traits.

```go
// internal/domain/milestone.go  line 40
{MilestoneS3, "Resilience", "Got a valid answer from a flaky trait during a chaos window", StretchMilestoneScore},
```

**Trigger condition** (from `internal/service/session_service.go` lines 244-247):

```go
// S3: Resilience — awarded when a successful answer is received for a flaky trait
// while the session is inside a chaos window. Chaos must be enabled for this to trigger.
if traitDef.IsFlaky && s.chaosService.IsInChaosWindow(session) {
    s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneS3)
}
```

This means S3 requires:
1. `CHAOS_ENABLED=true` (otherwise `IsInChaosWindow` always returns false — see `chaos.go` line 36).
2. The session to be inside a chaos window at the moment the request is processed.
3. The specific question asked to be a flaky trait (`IsFlaky: true`).
4. The chaos roll to land in the 40% success range (i.e., `ShouldFail` returned `false`).

A degraded response never awards S3. Only a genuine successful answer for a flaky trait, received while inside a chaos window, triggers it.

S3 is awarded once per team (idempotent via `AwardIfAbsent`). The 2000-point bonus is applied immediately to the team's leaderboard score.

### M2 — First Successful Question (core milestone, 1000 points)

```go
// internal/service/session_service.go  line 236
s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneM2)
```

This milestone is awarded on the first non-degraded answer of any kind. A successful answer from a flaky trait qualifies. A degraded response does not (the milestone call is after the chaos check).

### M3 — Elimination Working (core milestone, 1000 points)

Triggered when `session.GetQuestionsAskedCount() >= 3`. Since degraded responses don't increment `QuestionsAsked`, teams that receive repeated degraded responses may need more raw API calls to reach this threshold.

### M5 — Encrypted Answer Handled (core milestone, 1000 points)

Awarded when `POST /sessions/{sessionId}/decode` is called successfully. Relevant to T61 and T64 when they succeed — the client must decode the encrypted answer to trigger this milestone.

---

## 11. Client-Side Expectations

### Detecting a degraded response

Check for `"status": "degraded"` in the response body. Do not rely on HTTP status codes — a degraded response is HTTP 200.

```python
response = requests.post(f"/sessions/{session_id}/ask", json={"questionId": "T61"})
data = response.json()

if data.get("status") == "degraded":
    retry_delay_ms = data.get("retryAfterMs", 1000)
    time.sleep(retry_delay_ms / 1000)
    # retry the same question
else:
    # process data["answer"] or data["encrypted"]
```

### Recommended retry pattern

1. On receiving `"degraded"`, wait at least `retryAfterMs` milliseconds.
2. Retry the same `questionId`.
3. Apply exponential backoff if retries continue to fail (the server-provided delay is advisory, not guaranteed to clear the window).
4. Track the session's `chaosProfile.windowSeconds` and `chaosProfile.intervalSeconds` returned at session start — this tells you the maximum duration of a chaos window and allows you to estimate when to expect a safe window.

### Rate limiting on the ask endpoint

The ask endpoint is rate-limited in `guesswhoserviceapi.go`:

```go
// guesswhoserviceapi.go  line 156
publicMux.Handle("POST /sessions/{sessionId}/ask", rateLimiter.Limit(60, 5)(http.HandlerFunc(sessionHandler.AskQuestion)))
```

The token bucket is configured with:
- **Capacity**: 60 tokens
- **Refill rate**: 5 tokens per second

Aggressive retry loops (especially without backoff) risk hitting the rate limiter and receiving HTTP `429 Too Many Requests`. The rate limiter operates per team per path (keyed on `teamID + ":" + r.URL.Path`).

### What fields to expect on success for encrypted flaky traits

For T61 and T64 on a successful response, no `answer` field will be present. The client must read `encrypted`, strip the `b64:` prefix, base64-decode it, then strip the `PAYLOAD::` prefix to get the plaintext value.

Alternatively, call `POST /sessions/{sessionId}/decode` with `{"encrypted": "<value>"}` and read `decrypted` from the response. This also earns milestone M5.

### Question not recorded on degraded response

A degraded response does not consume a slot in the `QuestionsAsked` list. You may re-ask the same flaky trait after a retry without impacting your question bonus, but each failed attempt does add 5 points to your reliability penalty.

---

## 12. Current Pain Points and Observations

### 1. No chaos window boundary signalling

The client receives the `chaosProfile` at session start with `windowSeconds` and `intervalSeconds`, but there is no endpoint or event stream to announce when a chaos window begins or ends. The client must calculate this locally from the session's start time. Any clock skew between client and server will cause mispredictions.

### 2. Uniform random retry delay has no relationship to window duration

`GetRetryDelay()` returns 500–2000 ms. The default chaos window is 90 seconds. A client that naively sleeps for `retryAfterMs` and retries will almost certainly still be inside the same chaos window. The delay name implies it is a meaningful wait, but it is not — it is just a random number within a 1500 ms range. Clients should use the `chaosProfile` to estimate window boundaries, not `retryAfterMs`.

### 3. Degraded responses are silent on the question bonus budget

A degraded response costs 5 reliability-penalty points but does not consume the question bonus budget (since `QuestionsAsked` is not incremented). However, a client that does not understand this distinction may over-ask questions trying to "get one through" during a chaos window, inadvertently burning the question bonus on the successful retries.

### 4. T61 and T64 combine two complexity layers without signalling

A client that knows a trait is `"flaky"` tier and receives a successful response must also know to look for an `encrypted` field rather than an `answer` field. There is no per-field signal in the questions catalog that indicates "this flaky trait also encrypts on success". The `IsEncrypted` flag is `json:"-"`. Clients must discover this behaviour empirically or via documentation.

### 5. `cipher` and `keyHintId` fields are dead

`TraitAnswer.Cipher` and `TraitAnswer.KeyHintID` (lines 46–47 of `internal/domain/trait.go`) are populated by nothing in the codebase. They appear to be stubs for a more complex key-management scheme that was never implemented. They should either be removed from the struct or documented as reserved.

### 6. Chaos is session-wide, not trait-specific

Every flaky trait in a session shares the same chaos window. There is no per-trait chaos schedule. This means that during a chaos window, all six flaky traits face the same 60% failure rate simultaneously. A client cannot diversify by switching to a different flaky trait during a bad window.

### 7. `ShouldFail` uses a shared RNG

```go
// internal/service/chaos.go  line 29
rng: rand.New(rand.NewSource(time.Now().UnixNano())),
```

The `chaosService` is a singleton (created once in `guesswhoserviceapi.go` line 97). All sessions across all teams share the same `rng` instance with a mutex implicitly via `rand.Rand`. Under high concurrency, the random outcomes are still statistically independent, but the sequence is not isolated per session. This is unlikely to cause functional issues but is worth noting if deterministic chaos testing is ever needed.

### 8. No test coverage for flaky trait behaviour

The test files found are:

- `internal/service/encryption_test.go`
- `internal/service/scoring_test.go`
- `internal/service/board_generator_test.go`

There are no tests for `chaos.go`, `session_service.go`'s `AskQuestion` flaky path, or trait-level integration tests. The chaos window calculation, the 60% failure rate, and the S3 milestone award condition are all untested.
