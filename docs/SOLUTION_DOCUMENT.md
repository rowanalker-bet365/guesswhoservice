# Hackathon Solution Document — Guess Who: Identity Under Fire

## 1) Overview

Students build a small app (CLI or simple UI) that repeatedly solves a "Guess Who" round by identifying a hidden target from a randomly generated board.

Each round is a fresh API session with:
- 64 candidates (the "board")
- 64 trait questions available to ask about the hidden target
- Some answers returned as encrypted payloads
- Some endpoints behaving flakily (scheduled/probabilistic) to reward resilience

Teams are encouraged to automate after their first successful solve and compete on speed and number of successful rounds over the day.

## 2) Participant Objective (what they build)

A program that can:
- Start a new round (session)
- Load the candidate board (64 generated candidates + trait cards)
- Ask trait questions about the hidden target
- Handle encrypted answers via a decode endpoint
- Maintain an elimination set locally and choose the next question
- Submit a final guess
- Repeat (automation loop) to maximize solves/hour and minimize time/questions

## 3) Game Rules (per session)

- The board contains 64 fictional/generated candidates (no real student data).
- Teams can ask any of the 64 trait questions. Each question returns the target's trait answer (sometimes encrypted).
- Teams eliminate candidates locally using the returned answer.
- Guess submissions are limited per session (recommended: 1 guess; optionally allow up to 3 with heavy penalty).
- Rate limits apply to discourage brute force (see section 8).

## 4) Milestones (reward partial progress)

Milestones can be awarded automatically from API telemetry (recommended) and/or manually.

### Core Milestones:

- **M1 — First Round Started**: create session + download board
- **M2 — First Successful Question**: ask one trait and display/use answer
- **M3 — Elimination Working**: reduce candidate set based on answers
- **M4 — Encrypted Answer Handled**: decode at least one encrypted answer and apply it
- **M5 — Resilience**: continue progress during a failure window (e.g., 5 valid answers during chaos)
- **M6 — First Correct Solve**: submit correct guess

### Stretch Milestones:

- **S1 — Efficiency**: solve with ≤ N questions (e.g., 8–10)
- **S2 — Automation**: ≥ 5 consecutive successful solves
- **S3 — Best Reliability**: lowest error rate / fastest recovery

## 5) Scoring (supports "many solves over the day")

Example model (tune as desired):
- **Correct solve**: +1000
- **Time bonus**: timeBonus = max(0, 600 - t)
- **Question efficiency**: qBonus = max(0, 300 - 20*questionsAsked)
- **Reliability penalty**: reliabilityPenalty = 5*failedRequests + 2*timeouts + 10*unhandled5xx
- **Wrong guess** (if allowed): −500 and session ends

### Leaderboard fields (recommended):

`totalScore`, `solves`, `avgSolveTime`, `avgQuestions`, `successRate`, `bestStreak`

## 6) Session Randomization (repeatable + fair)

Each session creates a fresh board and a new hidden target.

Server creates `sessionId` and internal seed.

Use the seed to generate:
- 64 candidates with trait values
- a unique hidden target `candidateId`
- a chaos/failure profile

Return the candidate cards to the client (so elimination is done locally).

### Fairness controls:

- Avoid identical/near-identical trait vectors (regen collisions).
- Keep trait distributions meaningful (avoid traits that are always true/false).

## 7) Trait Catalogue (64 traits)

Each trait corresponds to a question teams can ask about the hidden target. Candidate cards include trait values so teams can eliminate locally.

**Legend**: Type = boolean / enum / numeric-range; Tier = Basic / Encrypted / Flaky (can overlap); Cost = for optional scoring/budgeting.

### A) Appearance (12)

| ID | Trait Key | Question | Type | Values | Tier | Cost |
|----|-----------|----------|------|--------|------|------|
| T01 | hair_color | Hair color? | enum | black,brown,blonde,red | Basic | 1 |
| T02 | hair_length | Hair length? | enum | short,medium,long | Basic | 1 |
| T03 | eye_color | Eye color? | enum | brown,blue,green,gray | Basic | 1 |
| T04 | wears_glasses | Wearing glasses? | boolean | true/false | Basic | 1 |
| T05 | has_beard | Has beard? | boolean | true/false | Basic | 1 |
| T06 | has_moustache | Has moustache? | boolean | true/false | Basic | 1 |
| T07 | has_freckles | Has freckles? | boolean | true/false | Basic | 1 |
| T08 | has_dimples | Has dimples? | boolean | true/false | Basic | 1 |
| T09 | skin_tone | Skin tone category? | enum | A,B,C,D (abstract) | Basic | 1 |
| T10 | face_shape | Face shape? | enum | oval,round,square,heart | Basic | 1 |
| T11 | eyebrow_style | Eyebrow style? | enum | straight,arched,thick,thin | Basic | 1 |
| T12 | smile | Smiling in photo? | boolean | true/false | Basic | 1 |

### B) Clothing (10)

| ID | Trait Key | Question | Type | Values | Tier | Cost |
|----|-----------|----------|------|--------|------|------|
| T13 | top_color | Top color? | enum | black,white,blue,green,red,gray | Basic | 1 |
| T14 | wears_hoodie | Wearing hoodie? | boolean | true/false | Basic | 1 |
| T15 | wears_jacket | Wearing jacket? | boolean | true/false | Basic | 1 |
| T16 | wears_shirt_collar | Collar visible? | boolean | true/false | Basic | 1 |
| T17 | bottom_type | Bottom type? | enum | jeans,chinos,shorts,skirt | Basic | 1 |
| T18 | shoe_type | Shoe type? | enum | trainers,boots,formal,sandals | Basic | 1 |
| T19 | hat_type | Wearing a hat? | enum | none,cap,beanie | Basic | 1 |
| T20 | pattern | Clothing pattern? | enum | plain,striped,checked | Basic | 1 |
| T21 | primary_style | Style? | enum | casual,sporty,smart,alt | Basic | 1 |
| T22 | wears_uniform | Wearing uniform? | boolean | true/false | Basic | 1 |

### C) Accessories (8)

| ID | Trait Key | Question | Type | Values | Tier | Cost |
|----|-----------|----------|------|--------|------|------|
| T23 | wears_watch | Wearing watch? | boolean | true/false | Basic | 1 |
| T24 | wears_ring | Wearing ring? | boolean | true/false | Basic | 1 |
| T25 | wears_necklace | Wearing necklace? | boolean | true/false | Basic | 1 |
| T26 | wears_earrings | Wearing earrings? | boolean | true/false | Basic | 1 |
| T27 | carries_backpack | Has backpack? | boolean | true/false | Basic | 1 |
| T28 | carries_laptop | Has laptop? | boolean | true/false | Basic | 1 |
| T29 | phone_os | Phone OS? | enum | iOS,Android,other | Encrypted | 2 |
| T30 | headphone_type | Headphones? | enum | none,in_ear,over_ear | Basic | 1 |

### D) University / Academic (12)

| ID | Trait Key | Question | Type | Values | Tier | Cost |
|----|-----------|----------|------|--------|------|------|
| T31 | year_group | Year group? | enum | 1,2,3,4 | Basic | 1 |
| T32 | faculty | Faculty? | enum | Eng,Sci,Biz,Arts | Basic | 1 |
| T33 | timetable_morning | Has morning class today? | boolean | true/false | Basic | 1 |
| T34 | lab_user | Has a lab module? | boolean | true/false | Basic | 1 |
| T35 | group_project | In a group project? | boolean | true/false | Basic | 1 |
| T36 | preferred_editor | Preferred editor? | enum | VSCode,IntelliJ,Vim,Other | Encrypted | 2 |
| T37 | os_primary | Primary OS? | enum | Windows,macOS,Linux | Encrypted | 2 |
| T38 | attends_society | In a society? | boolean | true/false | Basic | 1 |
| T39 | commute_type | Commute type? | enum | walk,bike,bus,train,car | Basic | 1 |
| T40 | library_frequency | Library use? | enum | low,medium,high | Basic | 1 |
| T41 | caffeine | Drinks caffeine? | boolean | true/false | Basic | 1 |
| T42 | part_time_job | Has part-time job? | boolean | true/false | Basic | 1 |

### E) Tech Preferences / Habits (8)

| ID | Trait Key | Question | Type | Values | Tier | Cost |
|----|-----------|----------|------|--------|------|------|
| T43 | favorite_language | Favorite language? | enum | Python,JS,Java,C#,Other | Encrypted | 2 |
| T44 | git_user | Uses git daily? | boolean | true/false | Basic | 1 |
| T45 | cloud_interest | Interested in cloud? | boolean | true/false | Basic | 1 |
| T46 | ai_interest | Interested in AI? | boolean | true/false | Basic | 1 |
| T47 | gaming | Plays games weekly? | boolean | true/false | Basic | 1 |
| T48 | keyboard_layout | Keyboard layout? | enum | QWERTY,AZERTY,Other | Encrypted | 2 |
| T49 | two_factor | Uses 2FA? | boolean | true/false | Flaky | 3 |
| T50 | password_manager | Uses password manager? | boolean | true/false | Flaky | 3 |

### F) Lifestyle / Preferences (10)

| ID | Trait Key | Question | Type | Values | Tier | Cost |
|----|-----------|----------|------|--------|------|------|
| T51 | sport | Plays a sport? | boolean | true/false | Basic | 1 |
| T52 | music_genre | Music genre? | enum | pop,rock,hiphop,jazz,edm,other | Basic | 1 |
| T53 | food_pref | Food preference? | enum | veg,nonveg,vegan,other | Basic | 1 |
| T54 | sleep_pattern | Sleep pattern? | enum | early,normal,nightowl | Basic | 1 |
| T55 | pet_person | Likes pets? | boolean | true/false | Basic | 1 |
| T56 | coffee_order | Coffee order? | enum | latte,americano,tea,none | Encrypted | 2 |
| T57 | weekend_style | Weekend style? | enum | chill,study,outdoors,work | Basic | 1 |
| T58 | travel | Traveled this year? | boolean | true/false | Basic | 1 |
| T59 | reading | Reads books monthly? | boolean | true/false | Basic | 1 |
| T60 | social_media | Uses social daily? | boolean | true/false | Basic | 1 |

### G) Security / Verification (4)

These traits explicitly drive resilience/error-handling and can depend on an intentionally flaky upstream.

| ID | Trait Key | Question | Type | Values | Tier | Cost |
|----|-----------|----------|------|--------|------|------|
| T61 | training_provider | Training provider? | enum | A,B,C,D | Flaky + Encrypted | 4 |
| T62 | id_verified | Identity verified? | boolean | true/false | Flaky | 4 |
| T63 | eligibility | Eligible for withdrawal? | boolean | true/false | Flaky | 4 |
| T64 | risk_band | Risk band? | enum | low,medium,high | Flaky + Encrypted | 4 |

## 8) API Specification (minimal but complete)

### Authentication

- Header: `X-Team-Id: <provided_on_the_day>`
- Optional: `X-Api-Key: <provided_on_the_day>`

### Rate limits (recommended)

- `/v1/sessions/start`: 10/min/team
- `/v1/sessions/{sessionId}/ask`: 5 req/sec/team burst, 60 req/min/team
- `/v1/sessions/{sessionId}/guess`: 1 per session (or max 3/session with penalties)

### Endpoints

#### 8.1 Start a session

```http
POST /v1/sessions/start
{ "boardSize": 64, "difficulty": "standard" }
```

Response:

```json
{
  "sessionId": "s_2f1c...",
  "boardSize": 64,
  "traitsAvailable": 64,
  "guessLimit": 1,
  "chaosProfile": {
    "mode": "scheduled",
    "windowSeconds": 90
  }
}
```

#### 8.2 Fetch the board (candidate "cards")

```http
GET /v1/sessions/{sessionId}/board
```

Response:

```json
{
  "sessionId": "s_2f1c...",
  "candidates": [
    {
      "candidateId": "c_001",
      "displayName": "Candidate 001",
      "traits": {
        "hair_color": "brown",
        "wears_glasses": true,
        "year_group": 2
      }
    }
  ],
  "traitDefinitions": [
    {
      "traitKey": "hair_color",
      "type": "enum",
      "values": ["black","brown","blonde","red"]
    }
  ]
}
```

#### 8.3 List questions (optional)

```http
GET /v1/sessions/{sessionId}/questions
```

Response:

```json
{
  "questions": [
    {
      "questionId": "T01",
      "traitKey": "hair_color",
      "type": "enum",
      "cost": 1,
      "tier": "basic"
    },
    {
      "questionId": "T43",
      "traitKey": "favorite_language",
      "type": "enum",
      "cost": 2,
      "tier": "encrypted"
    }
  ]
}
```

#### 8.4 Ask a question about the target

```http
POST /v1/sessions/{sessionId}/ask
{ "questionId": "T04" }
```

Plain response:

```json
{
  "questionId": "T04",
  "traitKey": "wears_glasses",
  "answer": true
}
```

Encrypted response:

```json
{
  "questionId": "T43",
  "traitKey": "favorite_language",
  "encrypted": "b64:AES256_encrypted_blob_here"
}
```

#### 8.5 Decode an encrypted answer

```http
POST /v1/sessions/{sessionId}/decode
{ "encrypted": "b64:AES256_encrypted_blob_here" }
```

Response:

```json
{
  "decrypted": "Python"
}
```

#### 8.6 Submit a guess

```http
POST /v1/sessions/{sessionId}/guess
{ "candidateId": "c_042" }
```

Success response:

```json
{
  "correct": true,
  "target": "c_042",
  "questionsAsked": 7,
  "timeElapsed": 42.3,
  "score": 1500
}
```

Failure response:

```json
{
  "correct": false,
  "target": "c_015",
  "penalty": -500,
  "sessionEnded": true
}
```

#### 8.7 Session status (optional)

```http
GET /v1/sessions/{sessionId}/status
```

Response:

```json
{
  "sessionId": "s_2f1c...",
  "active": true,
  "questionsAsked": 5,
  "guessesRemaining": 1,
  "startTime": "2026-01-15T10:23:45Z",
  "elapsedSeconds": 38
}
```

## 9) Chaos/Flaky Behavior Design

To reward resilience, some endpoints intentionally fail during windows:

### Scheduled Chaos Window
- Server picks a 90-second window during the session
- During this window, flaky endpoints may return:
  - 503 Service Unavailable (50% chance)
  - 429 Rate Limit (20% chance)
  - Timeout (no response for 10s)
  - Normal response (30% chance)

### Probabilistic Failures
- Flaky-tier trait questions have a base 15% failure rate outside chaos windows
- During chaos: 50% failure rate
- Encrypted endpoints add 5% decryption failure chance

### Recommended Client Strategy
- Retry with exponential backoff
- Track failed traits and retry later
- Continue asking other questions during failures
- Implement timeout handling (5-10s recommended)

## 10) Leaderboard & Telemetry

### Data Captured per Session
- Session ID, team ID, timestamp
- Questions asked (order, timing)
- Encrypted answers encountered
- Errors/retries/timeouts
- Final guess (correct/incorrect)
- Score breakdown

### Real-time Leaderboard Fields
- **Team Name**
- **Total Score**: cumulative across all sessions
- **Solves**: number of correct guesses
- **Avg Solve Time**: average seconds per successful solve
- **Avg Questions**: average questions asked per solve
- **Success Rate**: solves / total_sessions
- **Best Streak**: longest consecutive solves
- **Reliability Score**: (1 - error_rate) × 1000

### Update Frequency
- Real-time updates after each session completion
- Displayed on shared screen/dashboard
- Top 10 teams highlighted

## 11) Implementation Notes

### Server Requirements
- Node.js/Python/Go backend
- In-memory session store (Redis recommended)
- Rate limiting middleware
- Encryption library (AES-256)
- Random seed generator for reproducibility

### Client Starter Kit (optional to provide)
- Basic session management
- HTTP client with retry logic
- Local candidate filtering logic
- Simple CLI interface
- Example: "Ask question by ID, see answer, eliminate candidates"

### Testing & Validation
- Test chaos windows with mock failures
- Verify rate limits don't block legitimate play
- Ensure trait distributions are fair
- Check encryption/decryption round-trip
- Load test with 20-50 concurrent teams

### Day-of Operations
- Provide API endpoint URL and auth tokens
- Display leaderboard publicly
- Monitor server health/errors
- Have backup instance ready
- Award milestones progressively throughout the day

---

**End of Specification**
## 12) Technical Implementation Recommendations

> **Note:** For complete technical implementation details using Go and the Gin framework, see [`TECHNICAL_SOLUTION.md`](TECHNICAL_SOLUTION.md:1). This section provides language-agnostic guidance applicable to any technology stack.

### Technology Stack Considerations

When choosing a technology stack for this hackathon API, consider the following requirements:

#### Core Requirements
- **RESTful API**: Clean HTTP endpoints with JSON request/response
- **Request Validation**: Automatic or explicit validation of incoming data
- **JSON Handling**: Native support for JSON serialization/deserialization
- **Concurrent Requests**: Ability to handle 20-50 teams simultaneously
- **Fast Development**: Quick iteration and minimal boilerplate

#### Essential Features
- **CORS Support**: Enable cross-origin requests from participant applications
- **Rate Limiting**: Per-team request throttling to prevent abuse
- **Error Handling**: Clear, consistent error responses with appropriate HTTP status codes
- **Encryption**: AES-256 or similar for encrypting trait answers
- **Session Management**: Thread-safe storage of active game sessions

#### Recommended Technology Characteristics
- **Type Safety** (optional but helpful): Catches errors during development
- **Auto-Documentation** (highly recommended): Reduces support burden on organizers
- **Middleware Support**: Easy integration of logging, CORS, rate limiting
- **Performance**: Low latency, efficient memory usage
- **Deployment**: Simple deployment process (single binary, container, or cloud platform)

### In-Memory Storage Strategy

For a one-day hackathon, in-memory data structures are **strongly recommended** over a database.

#### Why In-Memory Storage?

1. **Simplicity**: No database setup, migrations, or connection pooling
2. **Performance**: Sub-millisecond lookups, perfect for real-time gameplay
3. **Stateless Sessions**: Each round is independent; no need for persistence
4. **Development Speed**: Build and test in hours, not days
5. **Zero Infrastructure**: No database to manage, backup, or scale
6. **Session Lifecycle**: Sessions only need to live ~2-10 minutes each
7. **Acceptable Data Loss**: If server restarts, teams just start new rounds

#### Data Storage Requirements

**Session Data:**
- Session ID (unique identifier)
- Team ID (for tracking)
- Board state (64 candidates with traits)
- Hidden target candidate
- Random seed (for reproducibility)
- Chaos window timing (start/end times)
- Questions asked counter
- Guesses remaining
- Timestamps (created, last activity)

**Thread Safety:**
- Use appropriate concurrency primitives (mutexes, locks, atomic operations)
- Read operations should not block other reads
- Write operations must be exclusive
- Consider using read-write locks for better performance

**Memory Management:**
- Implement session cleanup (remove expired sessions)
- Set session TTL (e.g., 30 minutes)
- Monitor memory usage in production
- Consider limiting maximum concurrent sessions

#### What to Store In-Memory

- **Active Sessions**: Map of session ID to session data
- **Team Tracking**: Map of team ID to list of session IDs (for analytics)
- **Leaderboard** (optional): Cumulative team scores and statistics
- **Trait Definitions**: Load once at startup (static data)
- **Rate Limit Counters**: Per-team request counts with sliding window

#### Persistence Strategy (Optional)

For leaderboard persistence across server restarts:
- Periodically snapshot to file (JSON, every 5 minutes)
- Load snapshot on startup
- Or accept that restarts reset the leaderboard (acceptable for hackathon)

### Session Management Implementation

#### Core Concepts

**Session Creation:**
- Generate unique session ID (UUID or similar)
- Create random seed for reproducibility
- Generate candidate board (64 candidates)
- Select hidden target from board
- Schedule chaos window timing
- Store session with timestamp

**Seed-Based Randomization:**
- Use cryptographically secure random seed
- Seed determines candidate selection and order
- Same seed = same board (for testing/debugging)
- Different seeds = fair, unique games

**Session Lifecycle:**
- **Active**: Game in progress, accepting questions/guesses
- **Won**: Correct guess submitted
- **Lost**: Wrong guess or timeout
- **Expired**: Session TTL exceeded

**Background Cleanup:**
- Periodically remove expired sessions
- Prevent memory leaks
- Run cleanup every 5-10 minutes
- Remove sessions older than 30 minutes

### Rate Limiting Implementation

**Purpose:** Prevent abuse and ensure fair play among teams.

**Strategy:**
- Implement per-team rate limiting using team identifier
- Different limits for different endpoint types
- Return clear error messages when limits exceeded
- Include rate limit headers in responses

**Recommended Limits:**
- `/sessions/start`: 5-10 requests per minute (prevent session spam)
- `/sessions/{id}/ask`: 30-60 requests per minute (gameplay)
- `/sessions/{id}/guess`: 3-5 requests per session (prevent brute force)
- `/sessions/{id}/board`: 100 requests per minute (read-only)

**Rate Limit Headers:**
```
X-RateLimit-Limit: 60          # Maximum requests allowed
X-RateLimit-Remaining: 45      # Requests remaining
X-RateLimit-Reset: 1640000000  # Unix timestamp when limit resets
```

**Error Response:**
```json
{
  "error": "rate_limit_exceeded",
  "message": "Too many requests. Please wait before trying again.",
  "retry_after": 30
}
```

**Implementation Considerations:**
- Use sliding window or token bucket algorithm
- Store counters in memory (hash map with expiration)
- Identify teams by header (e.g., `X-Team-ID`)
- Fallback to IP address if team ID not provided

### Chaos/Flaky Behavior Implementation

**Purpose:** Test participant resilience and error handling.

**Chaos Window Approach:**
- Schedule a random 90-second failure window during each session
- Window starts 30-240 seconds after session creation
- During window, endpoints have increased failure rates

**Failure Types:**
- `503 Service Unavailable` (50% of failures)
- `429 Too Many Requests` (20% of failures)
- Timeouts - delay response by 10+ seconds (optional)
- Corrupted responses - invalid JSON (optional)

**Probabilistic Failures:**
- Base failure rate for "flaky" tier traits: 15%
- During chaos window: 50% failure rate
- Encrypted traits add 5% additional failure chance

**Implementation Strategy:**
```
On each request to flaky endpoint:
1. Check if current time is within chaos window
2. Generate random number (0.0 to 1.0)
3. If random < failure_rate:
   - Select failure type randomly
   - Return error response
4. Otherwise, proceed normally
```

**Response Examples:**
```json
// 503 Service Unavailable
{
  "error": "service_temporarily_unavailable",
  "message": "Service is experiencing issues. Please retry."
}

// 429 Rate Limit (chaos-injected)
{
  "error": "rate_limit_exceeded",
  "message": "Too many requests",
  "retry_after": 5
}
```

**Best Practices:**
- Make chaos injection configurable (enable/disable via environment variable)
- Log all chaos events for debugging
- Ensure chaos doesn't affect critical endpoints (session creation, health check)
- Provide clear error messages so participants understand it's intentional

### Encryption/Decryption Implementation

**Purpose:** Add complexity and reward participants who implement decryption.

**Encryption Requirements:**
- Use symmetric encryption (AES-256 recommended)
- Encrypt specific trait answers (marked as "encrypted" tier)
- Each session can use same encryption key or derive per-session keys

**Encryption Flow:**
```
1. Client asks encrypted question
2. Server retrieves answer from target candidate
3. Server encrypts answer using encryption key
4. Server returns encrypted payload (base64-encoded)
5. Client calls /decode endpoint with encrypted payload
6. Server decrypts and returns plaintext answer
```

**Response Format:**
```json
// Encrypted answer response
{
  "questionId": "T43",
  "traitKey": "favorite_language",
  "encrypted": "b64:AES256_encrypted_blob_here"
}

// Decode request
{
  "encrypted": "b64:AES256_encrypted_blob_here"
}

// Decode response
{
  "decrypted": "Python"
}
```

**Security Considerations:**
- Use cryptographically secure encryption library
- Store encryption key in environment variable, never in code
- Use authenticated encryption (AES-GCM) to prevent tampering
- Include random IV/nonce with each encryption
- Base64-encode encrypted bytes for JSON transport

**Alternative Approach:**
- Provide decryption key to participants
- Let participants decrypt client-side
- Reduces server load but requires participants to implement crypto

### Deployment Options

The API server needs to be publicly accessible for hackathon participants. Choose a deployment strategy based on your infrastructure familiarity and requirements.

#### Option 1: Cloud Platform (Recommended)

**Popular Platforms:**
- **DigitalOcean App Platform**: Simple, auto-HTTPS, good free tier
- **Railway**: Quick deployment, generous free tier
- **Render**: Easy setup, free tier available
- **Fly.io**: Fast edge deployment, multi-region support
- **Heroku**: Simple but paid

**Advantages:**
- Quick setup (5-15 minutes)
- Automatic HTTPS/SSL certificates
- Built-in monitoring and logs
- Easy scaling if needed
- No server management

**General Deployment Steps:**
1. Containerize application (Docker) or use platform-native deployment
2. Configure environment variables (encryption keys, API keys)
3. Set up HTTPS/SSL certificates
4. Configure CORS for participant applications
5. Monitor logs and metrics
6. Plan for graceful restarts

#### Option 2: Docker Container

**Advantages:**
- Consistent across all environments
- Easy to deploy anywhere (cloud, VPS, local)
- Reproducible builds
- Simple scaling

**Basic Dockerfile Structure:**

```dockerfile
# Multi-stage build for minimal image size
FROM builder-image AS build
WORKDIR /app
COPY . .
RUN build-application

FROM runtime-image
WORKDIR /app
COPY --from=build /app/binary ./
EXPOSE 8080
CMD ["./binary"]
```

**Docker Compose for Local Development:**
```yaml
version: '3.8'
services:
  api:
    build: .
    ports:
      - "8080:8080"
    environment:
      - ENCRYPTION_KEY=local-dev-key-32bytes-long
      - PORT=8080
    restart: unless-stopped
```

#### Option 3: Virtual Private Server (VPS)

**Providers:** DigitalOcean Droplets, Linode, AWS EC2, Google Compute Engine

**Advantages:**
- Full control over environment
- Can run multiple services
- SSH access for debugging
- Cost-effective for simple deployments

**Basic Setup:**
1. Provision Linux server (Ubuntu/Debian recommended)
2. Install runtime dependencies
3. Copy application files
4. Configure reverse proxy (nginx/caddy)
5. Set up systemd service for auto-restart
6. Configure firewall rules

#### Option 4: Serverless/Functions (Not Recommended)

**Why Not Recommended:**
- In-memory session storage doesn't work across function invocations
- Cold starts add latency
- Requires external state management (Redis, database)
- More complex for hackathon timeframe

### Deployment Best Practices

**Critical Configuration for In-Memory Storage:**
- Deploy as **single instance only** (no auto-scaling, no load balancing)
- Multiple instances will cause session inconsistencies
- All requests must hit the same server instance

**Environment Variables:**
```
PORT=8080
ENCRYPTION_KEY=your-32-byte-key-for-aes
SESSION_TTL=30m
CLEANUP_INTERVAL=5m
RATE_LIMIT_ENABLED=true
CHAOS_ENABLED=true
ALLOWED_ORIGINS=http://localhost:3000,https://yourdomain.com
```

**Health Check Endpoint:**
Essential for monitoring:
```
GET /health
Response: {"status": "healthy", "uptime": 3600, "sessions": 42}
```

**Monitoring Checklist:**
- [ ] Health endpoint returns 200 OK
- [ ] API responds within acceptable latency (<100ms)
- [ ] Memory usage stays within limits
- [ ] No crash/restart loops
- [ ] Rate limiting working correctly
- [ ] CORS headers present for cross-origin requests

### Security Considerations

**Input Validation:**
- Validate all incoming data (team IDs, session IDs, trait keys)
- Use allowlists for trait keys (only accept predefined values)
- Sanitize inputs to prevent injection attacks
- Enforce length limits on string inputs

**Authentication/Authorization:**
- Use API keys or team tokens for authentication
- Validate team IDs on every request
- Consider JWT tokens for stateless authentication (optional)
- Rate limit per team, not just per IP

**Encryption Best Practices:**
- Store encryption keys in environment variables
- Use strong encryption (AES-256)
- Include nonce/IV with each encryption
- Use authenticated encryption (AES-GCM) to prevent tampering

**CORS Configuration:**
- Specify exact allowed origins (avoid `*` in production)
- Only allow necessary HTTP methods
- Expose only required headers
- Set appropriate max age for preflight caching

**Error Messages:**
- Don't leak sensitive information in error responses
- Use generic error messages for security issues
- Log detailed errors server-side only
- Return appropriate HTTP status codes

### Implementation Checklist

**Core Features:**
- [ ] Session creation with unique IDs
- [ ] Random seed generation and storage
- [ ] Candidate board generation (64 candidates)
- [ ] Hidden target selection
- [ ] Trait question endpoint
- [ ] Answer encryption/decryption
- [ ] Guess validation endpoint
- [ ] Game state management (active/won/lost)

**Quality Features:**
- [ ] Rate limiting per team
- [ ] Input validation on all endpoints
- [ ] Error handling with clear messages
- [ ] Health check endpoint
- [ ] Session cleanup (remove expired)
- [ ] CORS configuration
- [ ] Logging for debugging

**Optional Features:**
- [ ] Chaos injection middleware
- [ ] Leaderboard tracking
- [ ] Analytics/telemetry
- [ ] API documentation
- [ ] Comprehensive test coverage

### Development Best Practices

- Start with core functionality, add features incrementally
- Test each endpoint as you build it
- Use version control (git) from the start
- Document API endpoints for participants
- Keep deployment simple and reproducible
- Monitor logs during the hackathon
- Have a rollback plan if something breaks

---

**End of Technical Implementation Recommendations**