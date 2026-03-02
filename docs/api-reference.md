# API Reference

All endpoints are served by `guesswhoservice`. The Next.js frontend (`guesswhoui`) proxies most requests through its BFF API routes.

## Global Middleware

All requests pass through:
- **CORS** — configured via `CORS_ALLOWED_ORIGINS` env var
- **Structured logging** — JSON request/response logging
- **60-second timeout** — applied to all handlers

---

## Authentication

Protected endpoints require a JWT in the `Authorization: Bearer <token>` header.

**JWT Claims:**
- `team_id` — the team's unique identifier
- `team_name` — the team's display name
- Standard `exp` claim (expiry)

**JWT Secret:** configured via `JWT_SECRET` env var.

Tokens are issued by `POST /client/login` and `POST /client/signup`.

---

## Session Endpoints

### Start Session

```
POST /sessions/start
```

**Rate limit:** Token bucket — capacity 10, refill rate 10/s  
**Headers:** `X-Team-Id: <teamId>` (required)  
**Auth:** None (team ID from header)

**Request body:** None

**Response `200`:**
```json
{
  "sessionId": "uuid",
  "board": [
    {
      "candidateId": "P01",
      "imagePath": "/public/images/C12.png",
      "traits": [
        { "questionId": "T07", "traitKey": "hair_color", "answer": null, "status": "" }
      ]
    }
  ]
}
```

**Notes:**
- Board contains all unsolved characters (up to 64), each with a fake ID (`P01`, `P02`, …)
- Each candidate is assigned a random subset of 20 traits
- A cryptographically random seed is used to shuffle the board and select the target
- Maximum 2 concurrent active sessions per team; returns `429` if exceeded
- Awards milestone **M1** (1000 pts) on session start

**Error responses:**
- `400` — missing `X-Team-Id` header
- `404` — team not found
- `429` — max concurrent sessions reached or rate limit exceeded

---

### Get Board

```
GET /sessions/{sessionId}/board
```

**Auth:** None  
**Rate limit:** None

**Response `200`:**
```json
{
  "sessionId": "uuid",
  "board": [
    {
      "candidateId": "P01",
      "imagePath": "/public/images/C12.png",
      "traits": [
        { "questionId": "T07", "traitKey": "hair_color", "answer": null, "status": "" }
      ]
    }
  ]
}
```

**Notes:**
- Returns the current board state with fake candidate IDs
- Trait answers are `null` until revealed via `/ask`

---

### Ask Question

```
POST /sessions/{sessionId}/ask
```

**Rate limit:** Token bucket — capacity 60, refill rate 12/s  
**Auth:** None

**Request body:**
```json
{
  "questionId": "T07"
}
```

**Response `200`:**
```json
{
  "questionId": "T07",
  "traitKey": "hair_color",
  "answer": "brown",
  "status": ""
}
```

**Or when encrypted (40% probability, never on first question):**
```json
{
  "questionId": "T07",
  "traitKey": "hair_color",
  "answer": "<base64-encoded ciphertext>",
  "status": "encrypted"
}
```

**Notes:**
- Adds 50–200ms random jitter before responding (timing obfuscation)
- First question is never encrypted
- Subsequent questions have 40% chance of encryption
- The cipher type (`AES-256-GCM`, `AES-256-CBC`, or `XOR`) is fixed per session
- The encryption key is included in the session start response (teams must decrypt themselves)
- Trait answers are revealed session-wide (all candidates share the same answer for a given trait)
- Awards milestone **M2** (1000 pts) on first question, **M3** (1000 pts) on third question
- Awards milestone **S3** (2000 pts) if asked during an active chaos window
- Chaos may cause this endpoint to return errors (see Chaos System)

**Error responses:**
- `400` — invalid or missing `questionId`
- `404` — session not found or question not in candidate's trait subset
- `409` — session already completed
- `429` — rate limit exceeded
- `503` — chaos-induced failure

---

### Submit Guess

```
POST /sessions/{sessionId}/guess
```

**Auth:** None  
**Rate limit:** None

**Request body:**
```json
{
  "candidateId": "P12"
}
```

**Response `200` (correct guess):**
```json
{
  "correct": true,
  "characterId": "C34",
  "score": {
    "base": 1000,
    "timeBonus": 450,
    "questionBonus": 200,
    "reliabilityPenalty": -100,
    "total": 1550
  }
}
```

**Response `200` (incorrect guess):**
```json
{
  "correct": false,
  "guessesRemaining": 2
}
```

**Response `200` (final incorrect guess — session ends):**
```json
{
  "correct": false,
  "guessesRemaining": 0,
  "sessionEnded": true
}
```

**Notes:**
- `candidateId` must be a fake ID (`P01`, `P02`, …) — real IDs are never exposed
- Maximum 3 guesses per session
- Wrong guess: −200 pts applied immediately
- Correct guess: awards **M4** (1000 pts), **S1** (2000 pts) if solved in ≤3 questions, **S2** (2000 pts) if fastest solve
- Correct guess publishes a `game_update` SSE event to all connected clients

**Error responses:**
- `400` — invalid or missing `candidateId`
- `404` — session not found or candidate not on board
- `409` — session already completed or no guesses remaining

---

### Get Session Status

```
GET /sessions/{sessionId}/status
```

**Auth:** None  
**Rate limit:** None

**Response `200`:**
```json
{
  "sessionId": "uuid",
  "completed": false,
  "guessesRemaining": 3,
  "questionsAsked": ["T07", "T12"],
  "startTime": "2026-02-28T22:00:00Z"
}
```

---

### Reveal Target

```
POST /sessions/{sessionId}/reveal
```

**Auth:** None  
**Rate limit:** None

**Request body:** None

**Response `200`:**
```json
{
  "characterId": "C34",
  "penalty": -500
}
```

**Notes:**
- Requires at least **5 questions asked** if the session is not already completed
- Applies −500 pts penalty
- Marks session as completed and removes it from active sessions
- If session is already completed (all guesses used), the 5-question requirement is waived

**Error responses:**
- `400` — fewer than 5 questions asked (session not completed)
- `404` — session not found
- `409` — session already completed via reveal

---

## Trait Endpoints

### Get Questions

```
GET /questions
```

**Auth:** None  
**Rate limit:** None

**Response `200`:**
```json
{
  "questions": [
    {
      "questionId": "T01",
      "traitKey": "hair_color",
      "type": "enum"
    }
  ]
}
```

**Notes:**
- Returns all 64 trait definitions
- **Does not** return `question` text or `values` — these are intentionally omitted to prevent trivial enumeration
- `type` is one of `"boolean"`, `"enum"`, or `"numeric"`

---

## Client Endpoints

These endpoints are used by the Next.js BFF layer.

### Sign Up

```
POST /client/signup
```

**Auth:** None

**Request body:**
```json
{
  "teamName": "string",
  "password": "string"
}
```

**Response `201`:**
```json
{
  "message": "Team registered successfully"
}
```

**Error responses:**
- `400` — missing fields or team name already taken
- `409` — team name already exists

---

### Login

```
POST /client/login
```

**Auth:** None

**Request body:**
```json
{
  "teamName": "string",
  "password": "string"
}
```

**Response `200`:**
```json
{
  "token": "<jwt>",
  "team": {
    "id": "string",
    "name": "string",
    "color": "string"
  }
}
```

**Error responses:**
- `400` — missing fields
- `401` — invalid credentials

---

### Get Leaderboard

```
GET /client/game/leaderboard
```

**Auth:** None

**Response `200`:**
```json
{
  "entries": [
    {
      "teamId": "string",
      "name": "string",
      "color": "string",
      "solves": 5,
      "totalScore": 8500,
      "avgTimeSeconds": 142.3,
      "avgQuestions": 4.2,
      "successRate": 0.83,
      "bestStreak": 3,
      "fastestSolveMs": 45000
    }
  ]
}
```

**Notes:**
- Sorted by total score descending (ZREVRANGE on Redis sorted set)
- Uses a single Redis pipeline for all team data (one round-trip)

---

### Get Master Board

```
GET /client/game/master-board
```

**Auth:** None

**Response `200`:**
```json
{
  "characters": [
    {
      "characterId": "C01",
      "imagePath": "/public/images/C01.png",
      "solvedByTeams": ["team-id-1", "team-id-2"]
    }
  ]
}
```

**Notes:**
- Returns all 64 characters with the list of teams that have solved each
- Self-heals: if the masterboard hash is missing from Redis, it is re-initialised

---

### Get Team Progress

```
GET /client/team/progress
```

**Auth:** JWT required (`Authorization: Bearer <token>`)  
**Headers:** `X-Team-Id: <teamId>` (required)

**Response `200`:**
```json
{
  "teamId": "string",
  "name": "string",
  "color": "string",
  "score": 4500,
  "solves": 3,
  "fastestSolveMs": 45000,
  "milestones": ["M1", "M2", "M3", "M4"],
  "solvedCharacters": ["C12", "C34", "C56"]
}
```

---

### Reset Team Board

```
POST /client/team/reset
```

**Auth:** JWT required (`Authorization: Bearer <token>`)  
**Headers:** `X-Team-Id: <teamId>` (required)

**Request body:** None

**Response `200`:**
```json
{
  "message": "Team board reset successfully"
}
```

**Notes:**
- Clears the team's `solved_characters` set, allowing previously solved characters to appear on the board again
- Does not reset score or milestones

---

## Chaos Endpoints

These endpoints require the `X-Key-Id` header set to the value of the `CHAOS_API_KEY` environment variable.

### Trigger Chaos

```
POST /chaos/trigger
```

**Auth:** `X-Key-Id: <CHAOS_API_KEY>`

**Request body:**
```json
{
  "mode": "flaky",
  "windowSeconds": 300,
  "intervalSeconds": 30
}
```

**Chaos modes:**
- `"none"` — disables chaos
- `"flaky"` — random request failures (returns chaos error responses)
- `"timeout"` — introduces artificial delays
- `"error"` — returns 5xx errors

**Response `200`:**
```json
{
  "message": "Chaos triggered",
  "mode": "flaky",
  "windowSeconds": 300
}
```

---

### Get Chaos Status

```
GET /chaos/status
```

**Auth:** `X-Key-Id: <CHAOS_API_KEY>`

**Response `200`:**
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

## Debug Endpoints

These endpoints require the `X-Debug-Key` header set to the value of the `DEBUG_API_KEY` environment variable.

### Get Team Debug Info

```
GET /debug/team/{teamId}
```

**Auth:** `X-Debug-Key: <DEBUG_API_KEY>`

**Response `200`:** Full team data including internal Redis fields.

---

### Flush All Data

```
POST /debug/flush
```

**Auth:** `X-Debug-Key: <DEBUG_API_KEY>`

**Response `200`:**
```json
{
  "message": "All data flushed and masterboard re-seeded"
}
```

**Notes:**
- Wipes all Redis data (`FLUSHALL`)
- Re-seeds the masterboard hash with all 64 character IDs
- **Destructive** — use only in development/testing

---

## SSE Endpoint

### Game Events Stream

```
GET /events
```

**Auth:** None (on the Go service — auth is enforced by the Next.js BFF `/api/events` route)  
**Headers:** `Accept: text/event-stream` (required)

**Response:** Server-Sent Events stream

**Event types:**

```
event: connection
data: {"message":"SSE connection established"}

event: game_update
data: {"teamId":"team-abc","characterId":"C34"}

: keepalive
```

**Notes:**
- Keepalive comment sent every 15 seconds
- The Next.js `/api/events` route connects directly to Redis pub/sub (bypassing the Go service's `/events` endpoint) and enforces JWT presence via cookie
- On reconnect, the frontend revalidates all SWR keys to catch missed events

---

## Next.js BFF API Routes

The Next.js app exposes its own API routes that proxy to `guesswhoservice`. These are what the browser calls directly.

| Method | Path | Proxies To | Auth |
|--------|------|-----------|------|
| `POST` | `/api/auth/login` | `/client/login` | None |
| `POST` | `/api/auth/signup` | `/client/signup` | None |
| `GET` | `/api/game/leaderboard` | `/client/game/leaderboard` | None |
| `GET` | `/api/game/master-board` | `/client/game/master-board` | None |
| `GET` | `/api/team/progress` | `/client/team/progress` | Cookie → Bearer token |
| `POST` | `/api/team/reset` | `/client/team/reset`