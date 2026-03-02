# Scoring & Milestones

This document describes how points are calculated and how milestones are awarded in Guess Who: Identity Under Fire.

---

## Score Calculation

Points are awarded on a **correct guess** (`POST /sessions/{id}/guess`). The total score for a solve is:

```
total = base + timeBonus + questionBonus - reliabilityPenalty
```

### Base Score

**1000 points** for every correct guess.

### Time Bonus

Rewards fast solves. The bonus decreases as elapsed time increases:

| Time Elapsed | Bonus |
|-------------|-------|
| ≤ 60s | 500 pts |
| ≤ 120s | 400 pts |
| ≤ 180s | 300 pts |
| ≤ 300s | 200 pts |
| ≤ 600s | 100 pts |
| > 600s | 0 pts |

### Question Bonus

Rewards solving with fewer questions. The bonus decreases as more questions are asked:

| Questions Asked | Bonus |
|----------------|-------|
| 1–3 | 300 pts |
| 4–6 | 200 pts |
| 7–10 | 100 pts |
| > 10 | 0 pts |

### Reliability Penalty

Penalises teams for chaos-induced failures that were not handled gracefully. Tracked per session:

```
reliabilityPenalty = (failedRequests × 50) + (timeouts × 75) + (unhandled5xx × 100)
```

This penalty is subtracted from the total score. Teams that implement robust retry logic will have lower failure counts.

---

## Score Adjustments (Penalties)

These deductions are applied immediately (not just on correct solve):

| Event | Adjustment |
|-------|-----------|
| Wrong guess | −200 pts |
| Reveal target (`/reveal`) | −500 pts |

---

## Milestones

Milestones are one-time bonus awards. Each milestone is awarded **at most once per team** (idempotent — duplicate awards are silently ignored). Milestone bonuses are added to the team's total score immediately when earned.

### Core Milestones (1000 pts each)

| ID | Name | Condition |
|----|------|-----------|
| M1 | First Steps | Start a session (`POST /sessions/start`) |
| M2 | First Question | Ask your first question (`POST /sessions/{id}/ask`) |
| M3 | Getting Warmer | Ask your third question in a session |
| M4 | Got One! | Submit a correct guess (`POST /sessions/{id}/guess`) |
| M5 | *(reserved)* | *(not yet implemented)* |

### Stretch Milestones (2000 pts each)

| ID | Name | Condition |
|----|------|-----------|
| S1 | Sharp Shooter | Solve a character in 3 or fewer questions |
| S2 | Speed Demon | Achieve the fastest solve time across all teams |
| S3 | Chaos Survivor | Successfully ask a question during an active chaos window |

### Milestone Behaviour

- Milestones are checked and awarded automatically by the service — teams do not need to claim them
- All milestone awards are **fire-and-forget**: failures in milestone processing never block game flow
- Milestone IDs are stored as a comma-separated string in Redis (`team:<id>` hash, `milestones` field)
- The team progress endpoint (`GET /client/team/progress`) returns the list of earned milestone IDs

---

## Leaderboard

The leaderboard ranks all teams by **total score** (descending).

**Leaderboard entry fields:**

| Field | Description |
|-------|-------------|
| `teamId` | Team identifier |
| `name` | Team display name |
| `color` | Team colour (hex) |
| `solves` | Total correct guesses |
| `totalScore` | Cumulative score (including milestones and penalties) |
| `avgTimeSeconds` | Average time per solve |
| `avgQuestions` | Average questions asked per solve |
| `successRate` | Correct guesses ÷ total guesses |
| `bestStreak` | Longest consecutive correct guess streak |
| `fastestSolveMs` | Fastest single solve time in milliseconds |

**Implementation:**
- Scores are stored in a Redis sorted set (`leaderboard` key)
- Score updates use a Lua script for atomic `HINCRBY` + `ZINCRBY` operations
- Fastest solve is updated conditionally via a separate Lua script (only if the new time is faster)
- The leaderboard endpoint uses a single Redis pipeline (one round-trip) to fetch all team data

---

## Atomic Score Updates

Score updates on correct solve are performed atomically using a Lua script:

```lua
HINCRBY team:<teamID> score <delta>
HINCRBY team:<teamID> solves 1
SADD team:<teamID>:solved_characters <characterID>
ZINCRBY leaderboard <delta> <teamID>
SADD masterboard:<characterID> <teamID>
```

This ensures that score, solve count, solved character tracking, leaderboard position, and masterboard are all updated in a single atomic operation.