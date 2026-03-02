# Chaos System

The chaos system injects controlled failures into the game to simulate unreliable infrastructure. Teams must handle these failures gracefully in their automation, and doing so during a chaos window awards bonus points.

---

## Overview

When chaos is active, the `POST /sessions/{id}/ask` endpoint may return error responses instead of trait answers. Teams that successfully ask questions during a chaos window earn the **S3 milestone** (2000 pts).

The chaos system tracks failures per session:
- `FailedRequests` — requests that returned a chaos error
- `Timeouts` — requests that timed out
- `Unhandled5xx` — requests that returned 5xx errors

These counters feed into the **reliability penalty** in the scoring system.

---

## Chaos Modes

| Mode | Behaviour |
|------|-----------|
| `"none"` | Chaos disabled; all requests succeed normally |
| `"flaky"` | Random proportion of requests fail with a chaos error response |
| `"timeout"` | Requests experience artificial delays (simulating slow responses) |
| `"error"` | Requests return HTTP 5xx error responses |

---

## Triggering Chaos

Chaos is triggered via the admin API:

```
POST /chaos/trigger
X-Key-Id: <CHAOS_API_KEY>

{
  "mode": "flaky",
  "windowSeconds": 300,
  "intervalSeconds": 30
}
```

**Parameters:**
- `mode` — chaos mode (see table above)
- `windowSeconds` — how long the chaos window lasts (seconds)
- `intervalSeconds` — interval between chaos events within the window

**Checking chaos status:**
```
GET /chaos/status
X-Key-Id: <CHAOS_API_KEY>
```

Response:
```json
{
  "enabled": true,
  "mode": "flaky",
  "windowSeconds": 300,
  "intervalSeconds": 30,
  "active": true
}
```

---

## Chaos Error Responses

When chaos causes a failure on `POST /sessions/{id}/ask`, the response is:

```
HTTP 503 Service Unavailable

{
  "error": "chaos",
  "message": "<chaos-specific message>",
  "retryAfter": <seconds>
}
```

Teams should implement retry logic with exponential backoff to handle these failures.

---

## Effect on Scoring

Chaos-induced failures are tracked per session and applied as a **reliability penalty** to the final score:

```
reliabilityPenalty = -(failedRequests * penaltyPerFailure)
                   - (timeouts * penaltyPerTimeout)
                   - (unhandled5xx * penaltyPer5xx)
```

The penalty is subtracted from the total score on a correct solve. Teams that handle chaos gracefully (retry successfully) will have lower failure counts and smaller penalties.

---

## Milestone: S3 (Chaos Survivor)

**Award:** 2000 pts  
**Condition:** Successfully ask a question (`POST /sessions/{id}/ask`) during an active chaos window

This milestone is awarded once per team (idempotent). It rewards teams that implement robust retry logic and continue playing during chaos events.

---

## Configuration

| Environment Variable | Description | Default |
|---------------------|-------------|---------|
| `CHAOS_ENABLED` | Whether the chaos system is enabled | `false` |
| `CHAOS_API_KEY` | API key required to trigger/query chaos | (required if enabled) |

---

## Implementation Notes

- Chaos state is held in-memory in the `ChaosService` (not persisted to Redis)
- Chaos affects only the `POST /sessions/{id}/ask` endpoint
- The chaos profile is stored in each `Session` struct (`ChaosProfile` field) for scoring purposes
- Chaos is designed to be triggered by event organisers during the game, not by teams