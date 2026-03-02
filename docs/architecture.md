# System Architecture

## Overview

Guess Who: Identity Under Fire is a competitive team-based game where teams race to identify a hidden character from a board of 64 anonymised candidates by asking trait-based questions. The system is composed of two services and a shared Redis instance.

```
┌────────────────────────────────────────────────────────────┐
│                        Browser                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│  │  Home Page   │  │  Team Page   │  │  Auth Pages      │  │
│  │ (master board│  │ (team board, │  │ (login/signup)   │  │
│  │  leaderboard)│  │  milestones) │  │                  │  │
│  └──────┬───────┘  └──────┬───────┘  └────────┬─────────┘  │
└─────────┼─────────────────┼───────────────────┼────────────┘
          │                 │                   │
          ▼                 ▼                   ▼
┌─────────────────────────────────────────────────────────────┐
│                   guesswhoui (Next.js)                      │
│                                                             │
│  BFF API Routes:                                            │
│  /api/auth/login      /api/auth/signup                      │
│  /api/game/leaderboard  /api/game/master-board              │
│  /api/team/progress   /api/team/reset                       │
│  /api/events (SSE — connects directly to Redis)             │
└──────────────────────────┬──────────────────────────────────┘
                           │ HTTP
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                 guesswhoservice (Go)                        │
│                                                             │
│  Session API:    /sessions/*                                │
│  Client API:     /client/*                                  │
│  Chaos API:      /chaos/*                                   │
│  Debug API:      /debug/*                                   │
│  SSE:            /events                                    │
└──────────────────────────┬──────────────────────────────────┘
                           │
                           ▼
                    ┌─────────────┐
                    │    Redis    │
                    │             │
                    │  Sessions   │
                    │  Teams      │
                    │  Leaderboard│
                    │  Pub/Sub    │
                    └─────────────┘
```

## Components

### guesswhoservice (Go Backend)

The Go service is the authoritative game engine. It manages all game state, enforces rules, and persists data to Redis.

**Internal packages:**

| Package | Responsibility |
|---------|---------------|
| `handler` | HTTP request handling, request parsing, response serialisation |
| `service` | Business logic (sessions, scoring, chaos, encryption, milestones) |
| `domain` | Core data types (Session, Candidate, Trait, Score, Milestone) |
| `db` | Redis data access layer with Lua scripts for atomic operations |
| `middleware` | JWT authentication, token-bucket rate limiting |
| `logging` | Structured JSON logging with request context |
| `config` | Environment variable loading |

**Service dependency graph:**

```
SessionService
├── BoardGeneratorService   (shuffles 64 characters with seeded RNG)
├── CharacterCatalogService (loads characters.json at startup)
├── TraitCatalogService     (loads 64 trait definitions at startup)
├── ScoringService          (calculates score from time/questions/reliability)
├── MilestoneService        (awards milestone bonuses idempotently)
├── ChaosService            (injects failures based on active chaos profile)
├── EncryptionService       (encrypts trait answers with AES-256 or XOR)
└── db.Store                (Redis operations)
```

### guesswhoui (Next.js Frontend)

The Next.js app serves as both the UI and a Backend-for-Frontend (BFF). Its API routes proxy requests to `guesswhoservice`, adding authentication headers from server-side cookies. The SSE route connects directly to Redis pub/sub.

**Pages:**

| Route | Auth | Description |
|-------|------|-------------|
| `/` | Public | Master board showing all 64 characters + leaderboard |
| `/auth/login` | Public (redirects if authed) | Team login form |
| `/auth/signup` | Public (redirects if authed) | Team registration form |
| `/team` | Required | Team dashboard: metrics, milestones, team-specific board |

**State management:** Zustand store (`game-store.ts`) holds auth state, session ID, characters, leaderboard, and team progress. `GameContext` wraps the app and restores session from cookies/localStorage on mount.

### Redis

Redis serves dual roles: persistent data store and pub/sub message bus.

**Data store:** Teams, sessions, leaderboard (sorted set), masterboard (hash + sets), solved character tracking.

**Pub/Sub:** The `game_updates` channel carries `{"teamId":"...","characterId":"..."}` payloads on every correct solve. The Next.js SSE route subscribes to this channel and streams events to browsers.

---

## Data Flow

### Game Session Flow

```
Team → POST /api/auth/login
     ← JWT token + team info

Team → POST /sessions/start  (X-Team-Id header)
     ← sessionId, board (64 chars with fake IDs P01..Pn), 20 traits per candidate

Team → GET /questions
     ← list of {questionId, traitKey, type}

Team → POST /sessions/{id}/ask  {questionId: "T07"}
     ← {answer: <value|encrypted>, status: ""|"encrypted"}
        (40% chance encrypted, never on first question)
        (50–200ms random jitter added)

Team → POST /sessions/{id}/guess  {candidateId: "P12"}
     ← correct: {correct: true, score: {...}, characterId: "C34"}
        incorrect: {correct: false, guessesRemaining: 2}
        (max 3 guesses; wrong guess = −200 pts)

     OR

Team → POST /sessions/{id}/reveal  (requires ≥5 questions asked)
     ← {characterId: "C34", penalty: -500}
        (−500 pts applied)
```

### Real-Time Update Flow

```
Correct solve occurs in guesswhoservice
  → PublishGameUpdate("game_updates", {teamId, characterId})
    → Redis pub/sub channel

Next.js /api/events SSE route (subscribed to Redis)
  → streams: event: game_update\ndata: {teamId, characterId}

Browser useGameEvents hook
  → mutate('/api/game/master-board')   — updates all characters' solve status
  → mutate('/api/game/leaderboard')    — updates scores/rankings
  → mutate('/api/team/progress')       — only if teamId matches current team
```

---

## Domain Model

### Session

Core game state, persisted as JSON in Redis with 24-hour TTL.

| Field | Type | Description |
|-------|------|-------------|
| `SessionID` | `string` | UUID |
| `TeamID` | `string` | Owning team |
| `StartTime` | `time.Time` | Used for time bonus calculation |
| `Completed` | `bool` | Whether session has ended |
| `GuessesRemaining` | `int` | Starts at 3 |
| `Board` | `[]*Candidate` | All characters (shuffled, filtered for already-solved) |
| `TargetID` | `string` | Real character ID of the hidden target |
| `FakeIDMap` | `map[string]string` | real ID → fake ID (P01, P02, …) |
| `RealIDMap` | `map[string]string` | fake ID → real ID (reverse lookup) |
| `CandidateTraits` | `map[string][]TraitAnswer` | Per-candidate 20-trait subset |
| `RevealedTraits` | `map[string]TraitAnswer` | Traits revealed session-wide |
| `QuestionsAsked` | `[]string` | Ordered list of question IDs asked |
| `EncryptionKey` | `string` | 32-byte random key, hex-encoded |
| `CipherType` | `string` | `"AES-256-GCM"`, `"AES-256-CBC"`, or `"XOR"` |
| `ChaosProfile` | `ChaosProfile` | Active chaos configuration |
| `FailedRequests` | `int` | Chaos-induced failures (affects reliability penalty) |
| `Timeouts` | `int` | Chaos-induced timeouts |
| `Unhandled5xx` | `int` | Chaos-induced 5xx errors |

### Candidate

| Field | Type | Description |
|-------|------|-------------|
| `CandidateID` | `string` | C01–C64 |
| `DisplayName` | `string` | Always empty (anonymised) |
| `ImagePath` | `string` | `/public/images/C{nn}.png` |
| `Traits` | `map[string]interface{}` | All 64 trait values |

### TraitDefinition

| Field | Type | Description |
|-------|------|-------------|
| `QuestionID` | `string` | T01–T64 |
| `TraitKey` | `string` | e.g. `"hair_color"` |
| `Question` | `string` | Human-readable question text |
| `Type` | `TraitType` | `"boolean"`, `"enum"`, or `"numeric"` |
| `Values` | `[]string` | Valid values for enum/numeric types |

### TraitAnswer

| Field | Type | Description |
|-------|------|-------------|
| `QuestionID` | `string` | |
| `TraitKey` | `string` | |
| `Answer` | `interface{}` | Trait value (may be encrypted ciphertext) |
| `Status` | `string` | `"encrypted"` when encrypted, `""` otherwise |

### ScoreBreakdown

| Field | Type | Description |
|-------|------|-------------|
| `Base` | `int` | 1000 for correct guess |
| `TimeBonus` | `int` | Decreases as elapsed time increases |
| `QuestionBonus` | `int` | Decreases as more questions are asked |
| `ReliabilityPenalty` | `int` | Based on chaos-induced failures |

### Milestone

Type `string`. Values: `M1`, `M2`, `M3`, `M4`, `M5` (1000 pts each), `S1`, `S2`, `S3` (2000 pts each).

### LeaderboardEntry

| Field | Type | Description |
|-------|------|-------------|
| `TeamID` | `string` | |
| `Solves` | `int` | Total correct guesses |
| `AvgTimeSeconds` | `float64` | Average solve time |
| `AvgQuestions` | `float64` | Average questions per solve |
| `TotalScore` | `int` | Cumulative score |
| `SuccessRate` | `float64` | Correct guesses / total guesses |
| `BestStreak` | `int` | Longest consecutive correct guess streak |

---

## Redis Schema

| Key Pattern | Type | TTL | Contents |
|-------------|------|-----|----------|
| `team:<teamID>` | HASH | None | `name`, `password_hash`, `registered_at`, `color`, `score`, `solves`, `fastest_solve_ns`, `milestones` (comma-separated), `active_session_id` |
| `team:<teamID>:solved_characters` | SET | None | Character IDs solved by this team |
| `active_sessions:team:<teamID>` | SET | 24h | Active session IDs (max 2 enforced) |
| `session:<sessionID>` | STRING | 24h | JSON-marshalled `domain.Session` |
| `leaderboard` | ZSET | None | member=teamID, score=total score |
| `masterboard` | HASH | None | field=characterID, value=JSON array of teamIDs that have solved that character |