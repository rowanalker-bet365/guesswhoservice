# Security

This document describes the security measures implemented in `guesswhoservice` to increase the difficulty of the game and protect the API.

---

## Authentication

### JWT Tokens

Teams authenticate using JWT (JSON Web Tokens) issued by the `/client/login` endpoint.

**Token structure:**
- `team_id` — unique team identifier
- `team_name` — team display name
- `exp` — expiry timestamp

**Configuration:**
- Secret: `JWT_SECRET` environment variable
- Algorithm: HMAC-SHA256

**Protected endpoints:**
- `GET /client/team/progress` — requires `Authorization: Bearer <token>`
- `POST /client/team/reset` — requires `Authorization: Bearer <token>`

The Next.js BFF reads the JWT from the `guesswho_authtoken` cookie (set at login) and forwards it as a `Bearer` token to the backend. The middleware validates the token signature and expiry on every protected request.

### Admin Endpoints

Chaos and debug endpoints use separate API key authentication:
- **Chaos endpoints:** `X-Key-Id: <CHAOS_API_KEY>` header
- **Debug endpoints:** `X-Debug-Key: <DEBUG_API_KEY>` header

---

## Rate Limiting

Token-bucket rate limiting is applied to high-frequency endpoints to prevent brute-force attacks and automated solving.

| Endpoint | Capacity | Refill Rate |
|----------|----------|-------------|
| `POST /sessions/start` | 10 tokens | 10/second |
| `POST /sessions/{id}/ask` | 60 tokens | 12/second |

Rate limiters are per-endpoint (global, not per-team). Exceeding the limit returns `429 Too Many Requests`.

---

## Answer Encryption

To prevent teams from trivially reading trait answers, the service encrypts a proportion of responses.

### How It Works

1. At session creation, a **random 32-byte encryption key** is generated and stored in the session (hex-encoded in `EncryptionKey`)
2. A **cipher type** is randomly selected: `AES-256-GCM`, `AES-256-CBC`, or `XOR`
3. When answering a question (`POST /sessions/{id}/ask`):
   - The **first question is never encrypted** (to allow teams to establish a baseline)
   - Subsequent questions have a **40% probability** of being encrypted
4. When encrypted, the response contains:
   ```json
   {
     "answer": "<base64-encoded ciphertext>",
     "status": "encrypted"
   }
   ```
5. Teams must decrypt the answer using the key and cipher type from their session

### Cipher Details

| Cipher | Mode | Notes |
|--------|------|-------|
| `AES-256-GCM` | Authenticated encryption | Includes authentication tag; nonce prepended to ciphertext |
| `AES-256-CBC` | Block cipher | IV prepended to ciphertext; PKCS7 padding |
| `XOR` | Stream cipher | Key repeated/truncated to match plaintext length |

The cipher type is fixed for the duration of a session. Teams can determine the cipher type from the session start response.

### Key Management

- Keys are generated using `crypto/rand` (cryptographically secure)
- Keys are stored in the session object in Redis (24-hour TTL)
- Each session has a unique key — keys are never reused across sessions

---

## Fake Candidate IDs

Real character IDs (`C01`–`C64`) are never exposed to teams during a session. Instead, each session assigns **fake IDs** (`P01`, `P02`, …) to candidates.

- `FakeIDMap`: real ID → fake ID (stored in session)
- `RealIDMap`: fake ID → real ID (reverse lookup)

When a team submits a guess with a fake ID, the service resolves it to the real ID internally. This prevents teams from correlating answers across sessions or pre-computing character identities.

---

## Timing Obfuscation

The `POST /sessions/{id}/ask` endpoint adds a **random 50–200ms delay** before responding. This prevents timing attacks that could be used to infer whether an answer is encrypted or to correlate response times with specific trait values.

---

## Trait Subset Obfuscation

Each candidate on the board is assigned a **random subset of 20 traits** (from the full set of 64). This means:
- Teams cannot ask all 64 questions and get answers for every candidate
- Different sessions have different trait subsets per candidate
- The target character's traits are always answerable (the subset is drawn from the target's actual traits)

The `/questions` endpoint intentionally omits the `question` text and `values` fields, returning only `questionId`, `traitKey`, and `type`. This forces teams to reason about traits without a complete enumeration of possible values.

---

## Board Randomisation

Each session uses a **cryptographically random 64-bit seed** (`crypto/rand`) to:
1. Shuffle the board (Fisher-Yates shuffle via `math/rand` seeded with the crypto seed)
2. Select the target character

This ensures that:
- The target cannot be predicted from the session ID
- Board order varies between sessions
- Previously solved characters are filtered out before the board is presented

---

## Concurrent Session Limit

Each team is limited to **2 concurrent active sessions**. Attempting to start a third session returns `429`. This prevents teams from running many parallel sessions to increase their chances of a quick solve.

---

## Guess Limit

Each session allows a maximum of **3 guesses**. Wrong guesses cost **−200 points** each. When all guesses are exhausted, the session ends without a solve. This prevents brute-force guessing.

---

## Reveal Restriction

The `POST /sessions/{id}/reveal` endpoint (which reveals the target character at a −500 point penalty) requires that **at least 5 questions have been asked** before it can be used (unless the session has already ended due to exhausted guesses). This prevents teams from immediately revealing the answer without engaging with the game.

---

## Password Security

Team passwords are stored as **bcrypt hashes** in Redis. Plain-text passwords are never stored or logged.

---

## Security Measures Summary

| Measure | Implementation |
|---------|---------------|
| JWT authentication | HMAC-SHA256, validated on every protected request |
| Rate limiting | Token bucket on `/sessions/start` and `/ask` |
| Answer encryption | 40% of answers encrypted with AES-256-GCM/CBC or XOR |
| Fake candidate IDs | Real IDs never exposed; per-session fake ID mapping |
| Timing jitter | 50–200ms random delay on `/ask` |
| Trait subsets | 20 of 64 traits per candidate, randomly selected |
| Board randomisation | Crypto-random seed per session |
| Concurrent session limit | Max 2 active sessions per team |
| Guess limit | Max 3 guesses per session; −200 pts per wrong guess |
| Reveal restriction | Requires ≥5 questions asked |
| Password hashing | bcrypt |