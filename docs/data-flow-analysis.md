
# Comprehensive Data Flow Analysis: Guess Who Service

This document traces three critical data flows through the codebase in detail: how characters are fetched when the board is loaded, how questions are fetched, and how traits are stored and used.

---

## Table of Contents

1. [Flow 1: How Characters Are Fetched When the Board Is Loaded](#flow-1-how-characters-are-fetched-when-the-board-is-loaded)
2. [Flow 2: How Questions Are Fetched](#flow-2-how-questions-are-fetched)
3. [Flow 3: How Traits Are Stored](#flow-3-how-traits-are-stored)

---

## Flow 1: How Characters Are Fetched When the Board Is Loaded

This flow involves **two HTTP requests**: starting a session (which generates the board internally) and then fetching the board.

### Step 1: Application Startup — Loading Characters from Disk

Before any HTTP request is served, characters are loaded from disk during application startup.

In [`main()`](../guesswhoserviceapi.go), the character catalog is initialized:

```go
characterCatalog, err := service.NewCharacterCatalogService("data/characters.json")
```

This calls [`NewCharacterCatalogService()`](../internal/service/character_catalog.go) which:

1. Reads the file [`data/characters.json`](../data/characters.json) from disk using `os.ReadFile(dataPath)`
2. Unmarshals the JSON into a slice of [`characterJSON`](../internal/service/character_catalog.go) structs — an intermediate struct with fields `ID`, `Name`, `Image`, and `Traits` (a `map[string]interface{}`)
3. Converts each `characterJSON` into a [`domain.Candidate`](../internal/domain/candidate.go) struct and stores them in an in-memory `map[string]domain.Candidate` keyed by character ID

The [`Candidate`](../internal/domain/candidate.go) struct is:

```go
type Candidate struct {
    CandidateID string                 `json:"candidateId"`
    DisplayName string                 `json:"displayName"`
    ImagePath   string                 `json:"imagePath"`
    Traits      map[string]interface{} `json:"traits"`
}
```

The JSON file contains 64+ characters (IDs like `C01`, `C02`, … `C64`), each with an `id`, `name`, `image` path, and a `traits` object with 64 key-value pairs.

### Step 2: Starting a Session — `POST /sessions/start`

**HTTP endpoint**: `POST /sessions/start` (registered in [`guesswhoserviceapi.go`](../guesswhoserviceapi.go))

**Handler**: [`SessionHandler.StartSession()`](../internal/handler/session_handler.go)

This handler:
1. Extracts the `X-Team-Id` header from the request
2. Calls [`sessionService.StartSession(ctx, teamID)`](../internal/service/session_service.go)

**Inside [`StartSession()`](../internal/service/session_service.go)**:

1. **Generate session ID**: Creates a unique ID like `s_<uuid-prefix>` using `uuid.New()`
2. **Derive a seed**: Calls [`sessionShuffleSeed(sessionID)`](../internal/service/session_service.go) which hashes the session ID using FNV-64a to produce a deterministic `int64` seed
3. **Create the session object**: Calls [`domain.NewSession()`](../internal/domain/session.go) which initializes a [`Session`](../internal/domain/session.go) with `BoardSize: 64`, `TraitsAvailable: 64`, `GuessLimit: 3`, and empty slices for `QuestionsAsked` and `GuessedCandidates`
4. **Generate the board**: Calls [`boardGenerator.GenerateBoard(perSessionSeed, characterCatalog)`](../internal/service/board_generator.go)

   Inside [`GenerateBoard()`](../internal/service/board_generator.go):
   - Calls `characterCatalog.GetAllCharacters()` → [`GetAllCharacters()`](../internal/service/character_catalog.go) returns all characters sorted deterministically by ID
   - Creates independent copies (pointer slice) of each candidate so mutations don't affect the catalog
   - **Shuffles** the candidates using a seeded `rand.Rand` for reproducibility — the same seed always produces the same board order

5. **Filter solved characters**: Reads team data from Redis via [`dbStore.ReadTeamData()`](../internal/db/redis_store.go) and removes any characters the team has already solved. If all characters are solved, it resets and uses the full list.

6. **Select a target**: Calls [`boardGenerator.SelectTarget(availableCandidates, perSessionSeed)`](../internal/service/board_generator.go) which uses `seed + 1000` and picks a random index — this is the character the player must guess

7. **Persist**: The session (with its `Candidates` slice and `TargetCandidate`) is serialized to JSON and saved to Redis via [`dbStore.WriteSession()`](../internal/db/redis_store.go) under key `session:<sessionID>`

8. **Return response**: The handler returns a [`StartSessionResponse`](../internal/handler/session_handler.go) containing `sessionId`, `boardSize`, `traitsAvailable`, `guessLimit`, and `chaosProfile` — but **not** the actual characters

### Step 3: Fetching the Board — `GET /sessions/{sessionId}/board`

**HTTP endpoint**: `GET /sessions/{sessionId}/board` (registered in [`guesswhoserviceapi.go`](../guesswhoserviceapi.go))

**Handler**: [`SessionHandler.GetBoard()`](../internal/handler/session_handler.go)

This handler:
1. Extracts `sessionId` from the URL path
2. Calls [`sessionService.GetSession(ctx, sessionID)`](../internal/service/session_service.go) which calls [`dbStore.ReadSession()`](../internal/db/redis_store.go) — reads JSON from Redis key `session:<sessionID>` and deserializes into `domain.Session`
3. **Builds trait definitions**: Calls [`traitCatalog.GetAllTraits()`](../internal/service/trait_catalog.go) and maps each [`TraitDefinition`](../internal/domain/trait.go) into a simplified map with only `traitKey`, `type`, and `values`
4. **Builds the board array**: Iterates over `session.Candidates` and for each candidate, extracts **only** the `Traits` map — deliberately **excluding** `candidateId`, `displayName`, and `imagePath` to prevent cheating
5. **Returns JSON response** with shape:
   ```json
   {
     "sessionId": "s_abc12345",
     "board": [ { "hair_color": "black", "has_beard": true }, ... ],
     "traitDefinitions": [ { "traitKey": "hair_color", "type": "enum", "values": ["black","brown","blonde","red"] }, ... ]
   }
   ```

### Character Fetching Sequence Diagram

```
Client                  SessionHandler     SessionService     BoardGenerator     CharacterCatalog     Redis
  |                          |                  |                   |                   |                |
  |  POST /sessions/start    |                  |                   |                   |                |
  |------------------------->|                  |                   |                   |                |
  |                          | StartSession()   |                   |                   |                |
  |                          |----------------->|                   |                   |                |
  |                          |                  | GenerateBoard()   |                   |                |
  |                          |                  |------------------>|                   |                |
  |                          |                  |                   | GetAllCharacters()|                |
  |                          |                  |                   |------------------>|                |
  |                          |                  |                   |  []Candidate      |                |
  |                          |                  |                   |<------------------|                |
  |                          |                  |                   | Copy + Shuffle    |                |
  |                          |                  |  []*Candidate     |                   |                |
  |                          |                  |<------------------|                   |                |
  |                          |                  | ReadTeamData()    |                   |                |
  |                          |                  |------------------------------------------------------>|
  |                          |                  | Filter solved     |                   |                |
  |                          |                  | SelectTarget()    |                   |                |
  |                          |                  |------------------>|                   |                |
  |                          |                  |  *Candidate       |                   |                |
  |                          |                  |<------------------|                   |                |
  |                          |                  | WriteSession()    |                   |                |
  |                          |                  |------------------------------------------------------>|
  |                          |  *Session        |                   |                   |                |
  |                          |<-----------------|                   |                   |                |
  |  {sessionId, boardSize}  |                  |                   |                   |                |
  |<-------------------------|                  |                   |                   |                |
  |                          |                  |                   |                   |                |
  | GET /sessions/{id}/board |                  |                   |                   |                |
  |------------------------->|                  |                   |                   |                |
  |                          | GetSession()     |                   |                   |                |
  |                          |----------------->|                   |                   |                |
  |                          |                  | ReadSession()     |                   |                |
  |                          |                  |------------------------------------------------------>|
  |                          |                  |  *Session         |                   |                |
  |                          |                  |<------------------------------------------------------|
  |                          |  *Session        |                   |                   |                |
  |                          |<-----------------|                   |                   |                |
  |                          | Extract Traits   |                   |                   |                |
  |  {board, traitDefs}      |                  |                   |                   |                |
  |<-------------------------|                  |                   |                   |                |
```

---

## Flow 2: How Questions Are Fetched

### Part A: Fetching the Question List — `GET /sessions/{sessionId}/questions`

**HTTP endpoint**: `GET /sessions/{sessionId}/questions` (registered in [`guesswhoserviceapi.go`](../guesswhoserviceapi.go))

**Handler**: [`TraitHandler.GetQuestions()`](../internal/handler/trait_handler.go)

This handler:
1. Calls [`traitCatalog.GetAllTraits()`](../internal/service/trait_catalog.go)
2. The [`TraitCatalogService`](../internal/service/trait_catalog.go) holds all traits in a `map[string]domain.TraitDefinition` keyed by `questionID` (e.g., `"T01"`, `"T02"`, … `"T64"`)
3. [`GetAllTraits()`](../internal/service/trait_catalog.go) sorts the keys lexicographically and returns a deterministically-ordered slice of [`TraitDefinition`](../internal/domain/trait.go) structs
4. The handler maps each `TraitDefinition` into a response map containing: `questionId`, `traitKey`, `type`, `cost`, and `tier`
5. Returns JSON:
   ```json
   {
     "questions": [
       { "questionId": "T01", "traitKey": "hair_color", "type": "enum", "cost": 1, "tier": "basic" },
       { "questionId": "T02", "traitKey": "hair_length", "type": "enum", "cost": 1, "tier": "basic" },
       ...
     ]
   }
   ```

### Where Are Questions Stored?

Questions are **hardcoded in Go source code** inside [`initializeTraits()`](../internal/service/trait_catalog.go). They are NOT in a database or external file. The function builds a map of 64 [`TraitDefinition`](../internal/domain/trait.go) structs organized into 7 categories:

| Category | Question IDs | Examples |
|----------|-------------|----------|
| **A) Appearance** | T01–T12 | hair_color, hair_length, eye_color, wears_glasses, has_beard, skin_tone |
| **B) Clothing** | T13–T22 | top_color, wears_hoodie, bottom_type, shoe_type, has_logo |
| **C) Accessories** | T23–T30 | wears_watch, carries_laptop, phone_os, headphone_type |
| **D) University/Academic** | T31–T42 | year_group, faculty, preferred_editor, os_primary, gpa_band |
| **E) Tech Preferences** | T43–T50 | favorite_language, git_user, two_factor, uses_dark_mode |
| **F) Lifestyle** | T51–T60 | sport, music_genre, food_pref, coffee_order, pet_owner |
| **G) Security/Verification** | T61–T64 | training_provider, id_verified, eligibility, risk_band |

### Part B: Asking a Question — `POST /sessions/{sessionId}/ask`

**HTTP endpoint**: `POST /sessions/{sessionId}/ask` (registered in [`guesswhoserviceapi.go`](../guesswhoserviceapi.go), rate-limited to 60 requests per 5 seconds)

**Handler**: [`SessionHandler.AskQuestion()`](../internal/handler/session_handler.go)

**Request body**: `{ "questionId": "T01" }`

The handler calls [`sessionService.AskQuestion(ctx, sessionID, questionID)`](../internal/service/session_service.go):

1. **Load session** from Redis via [`dbStore.ReadSession()`](../internal/db/redis_store.go)
2. **Validate**: Check session is not completed
3. **Look up trait definition**: Calls [`traitCatalog.GetTraitByID(questionID)`](../internal/service/trait_catalog.go) — searches the in-memory map by `questionID`
4. **Chaos check**: Calls [`chaosService.ShouldFail(session, traitDef)`](../internal/service/chaos.go). If chaos is enabled AND the trait is flaky AND we're in a chaos window AND random probability triggers, returns a **degraded response**:
   ```json
   { "questionId": "T01", "traitKey": "hair_color", "status": "degraded", "retryAfterMs": 1200 }
   ```
5. **Get the answer**: Calls [`session.TargetCandidate.GetTrait(traitDef.TraitKey)`](../internal/domain/candidate.go) — looks up the trait key in the target candidate's `Traits` map
6. **Record the question**: Appends the questionID to `session.QuestionsAsked` and saves session back to Redis
7. **Award milestones**: M2 (first successful question), M3 (3+ questions asked), S3 (resilience — flaky trait during chaos window)
8. **Build response** as a [`TraitAnswer`](../internal/domain/trait.go):
   - For **non-encrypted traits** (`IsEncrypted: false`): Returns the answer directly
     ```json
     { "questionId": "T01", "traitKey": "hair_color", "answer": "black" }
     ```
   - For **encrypted traits** (`IsEncrypted: true`): Calls [`encryptionService.Encrypt(plaintext)`](../internal/service/encryption.go) which base64-encodes the answer with a `"PAYLOAD::"` prefix and returns `"b64:<base64>"`
     ```json
     { "questionId": "T29", "traitKey": "phone_os", "encrypted": "b64:UEFZTE9BRDo6QW5kcm9pZA==" }
     ```

### Part C: Decoding Encrypted Answers — `POST /sessions/{sessionId}/decode`

**HTTP endpoint**: `POST /sessions/{sessionId}/decode` (registered in [`guesswhoserviceapi.go`](../guesswhoserviceapi.go))

**Handler**: [`TraitHandler.Decode()`](../internal/handler/trait_handler.go)

**Request body**: `{ "encrypted": "b64:UEFZTE9BRDo6QW5kcm9pZA==" }`

The handler:
1. Parses the [`DecodeRequest`](../internal/handler/trait_handler.go) from the JSON body — expects a single `encrypted` field
2. Calls [`encryptionService.Decrypt(encrypted)`](../internal/service/encryption.go)

**Inside [`Decrypt()`](../internal/service/encryption.go)**:
1. Strips the `"b64:"` prefix (first 4 characters)
2. Base64-decodes the remaining string using `base64.StdEncoding.DecodeString()`
3. Strips the `"PAYLOAD::"` prefix (first 9 characters) from the decoded bytes
4. Returns the plaintext answer

For example:
- Input: `"b64:UEFZTE9BRDo6QW5kcm9pZA=="`
- After removing `"b64:"`: `"UEFZTE9BRDo6QW5kcm9pZA=="`
- After base64 decode: `"PAYLOAD::Android"`
- After removing `"PAYLOAD::"`: `"Android"`

3. **Award milestone M5**: If a valid `teamID` is found in the JWT context (via [`middleware.TeamIDKey`](../internal/middleware/auth_middleware.go)), calls [`milestoneService.AwardIfAbsent(ctx, teamID, domain.MilestoneM5)`](../internal/service/milestone_service.go) — "Encrypted Answer Handled"
4. Returns a [`DecodeResponse`](../internal/handler/trait_handler.go):
   ```json
   { "decrypted": "Android" }
   ```

### Which Traits Are Encrypted?

From the [`initializeTraits()`](../internal/service/trait_catalog.go) function, traits with `IsEncrypted: true` are:

| Question ID | Trait Key | Tier | Cost |
|-------------|-----------|------|------|
| T29 | `phone_os` | encrypted | 2 |
| T36 | `preferred_editor` | encrypted | 2 |
| T37 | `os_primary` | encrypted | 2 |
| T43 | `favorite_language` | encrypted | 2 |
| T48 | `keyboard_layout` | encrypted | 2 |
| T56 | `coffee_order` | encrypted | 2 |
| T61 | `training_provider` | flaky | 4 |
| T64 | `risk_band` | flaky | 4 |

Note: T61 and T64 are **both** encrypted AND flaky — they may return degraded responses during chaos windows, and when they do return answers, those answers are encrypted.

### Question Flow Sequence Diagram

```
Client              TraitHandler / SessionHandler   SessionService      EncryptionService    TraitCatalog     Redis
  |                          |                           |                     |                  |              |
  | GET /questions           |                           |                     |                  |              |
  |------------------------->|                           |                     |                  |              |
  |                          | GetAllTraits()            |                     |                  |              |
  |                          |--------------------------------------------------------->         |              |
  |                          |  []TraitDefinition        |                     |                  |              |
  |                          |<---------------------------------------------------------         |              |
  |  {questions: [...]}      |                           |                     |                  |              |
  |<-------------------------|                           |                     |                  |              |
  |                          |                           |                     |                  |              |
  | POST /ask {questionId}   |                           |                     |                  |              |
  |------------------------->|                           |                     |                  |              |
  |                          | AskQuestion()             |                     |                  |              |
  |                          |-------------------------->|                     |                  |              |
  |                          |                           | ReadSession()       |                  |              |
  |                          |                           |--------------------------------------------->        |
  |                          |                           | GetTraitByID()      |                  |              |
  |                          |                           |------------------------------->        |              |
  |                          |                           | ShouldFail()        |                  |              |
  |                          |                           | GetTrait()          |                  |              |
  |                          |                           | [if encrypted]      |                  |              |
  |                          |                           | Encrypt(plaintext)  |                  |              |
  |                          |                           |-------------------->|                  |              |
  |                          |                           |  "b64:..."          |                  |              |
  |                          |                           |<--------------------|                  |              |
  |                          |                           | WriteSession()      |                  |              |
  |                          |                           |--------------------------------------------->        |
  |                          |  TraitAnswer              |                     |                  |              |
  |                          |<--------------------------|                     |                  |              |
  |  {answer or encrypted}   |                           |                     |                  |              |
  |<-------------------------|                           |                     |                  |              |
  |                          |                           |                     |                  |              |
  | POST /decode {encrypted} |                           |                     |                  |              |
  |------------------------->|                           |                     |                  |              |
  |                          | Decrypt(encrypted)        |                     |                  |              |
  |                          |-------------------------------------->          |                  |              |
  |                          |  plaintext                |                     |                  |              |
  |                          |<--------------------------------------          |                  |              |
  |  {decrypted: "Android"}  |                           |                     |                  |              |
  |<-------------------------|                           |                     |                  |              |
```

---

## Flow 3: How Traits Are Stored

Traits exist in three distinct storage locations in the codebase, each serving a different purpose:

### Storage Location 1: Trait Definitions — Hardcoded in Go Source

**File**: [`internal/service/trait_catalog.go`](../internal/service/trait_catalog.go)

**Function**: [`initializeTraits()`](../internal/service/trait_catalog.go)

This is the **schema** — it defines what traits exist, their types, possible values, and behavioral flags. The function creates a `map[string]domain.TraitDefinition` with 64 entries keyed by question ID (`"T01"` through `"T64"`).

Each [`TraitDefinition`](../internal/domain/trait.go) struct contains:

| Field | Type | Purpose |
|-------|------|---------|
| `QuestionID` | `string` | Unique identifier (e.g., `"T01"`) |
| `TraitKey` | `string` | The key used in character data (e.g., `"hair_color"`) |
| `Question` | `string` | Human-readable question text |
| `Type` | [`TraitType`](../internal/domain/trait.go) | `"boolean"`, `"enum"`, or `"numeric"` |
| `Values` | `[]string` | Allowed values for enum types (nil for boolean) |
| `Tier` | [`TraitTier`](../internal/domain/trait.go) | `"basic"`, `"encrypted"`, or `"flaky"` |
| `Cost` | `int` | Score cost: 1 (basic), 2 (encrypted), 3–4 (flaky) |
| `IsEncrypted` | `bool` | If true, answers are base64-encoded before returning |
| `IsFlaky` | `bool` | If true, answers may fail during chaos windows |

This data is **immutable at runtime** — it is created once in [`NewTraitCatalogService()`](../internal/service/trait_catalog.go) and never modified.

### Storage Location 2: Trait Values — JSON File on Disk

**File**: [`data/characters.json`](../data/characters.json)

This is the **data** — it contains the actual trait values for each character. The file is a JSON array of 64 character objects, each with a `traits` map containing values for all 64 trait keys.

Example structure:
```json
[
  {
    "id": "C01",
    "name": "Character One",
    "image": "images/c01.png",
    "traits": {
      "hair_color": "black",
      "hair_length": "short",
      "eye_color": "brown",
      "wears_glasses": false,
      "has_beard": true,
      "year_group": "2",
      "favorite_language": "Python",
      "risk_band": "low",
      ...
    }
  },
  ...
]
```

This file is loaded **once at startup** by [`NewCharacterCatalogService("data/characters.json")`](../internal/service/character_catalog.go) and stored in an in-memory `map[string]domain.Candidate`. The data is **never reloaded** from disk after startup — a server restart is required to pick up changes.

### Storage Location 3: Session State — Redis

**Key pattern**: `session:<sessionID>` (e.g., `session:s_abc12345`)

When a session is started, the full list of board candidates (with all their trait values) and the target candidate are serialized to JSON and stored in Redis via [`dbStore.WriteSession()`](../internal/db/redis_store.go).

The session JSON in Redis includes:
- `candidates`: Array of [`Candidate`](../internal/domain/candidate.go) objects — each with full `traits` map
- `targetCandidate`: The single [`Candidate`](../internal/domain/candidate.go) the player must guess — also with full `traits` map
- `questionsAsked`: Array of question IDs asked so far
- `completed`, `correctGuess`, `guessesRemaining`: Session state

When a question is asked, the answer is retrieved from `session.TargetCandidate.Traits[traitKey]` — it reads from Redis, not from the original character catalog or disk.

### How Traits Flow Through the System

```
┌─────────────────────────┐
│  data/characters.json   │  ← 64 characters × 64 traits (disk)
│  (loaded once at start) │
└────────────┬────────────┘
             │ os.ReadFile + json.Unmarshal
             ▼
┌─────────────────────────┐
│  CharacterCatalogService│  ← In-memory map[string]Candidate
│  (immutable at runtime) │     keyed by character ID
└────────────┬────────────┘
             │ GetAllCharacters()
             ▼
┌─────────────────────────┐
│  BoardGeneratorService  │  ← Copies + shuffles candidates
│  GenerateBoard()        │     using per-session seed
└────────────┬────────────┘
             │ []*Candidate (shuffled)
             ▼
┌─────────────────────────┐
│  Session in Redis       │  ← session:<id> key
│  - candidates[]         │     Full trait data persisted
│  - targetCandidate      │     per session
│  - questionsAsked[]     │
└────────────┬────────────┘
             │ ReadSession() → TargetCandidate.GetTrait(key)
             ▼
┌─────────────────────────┐     ┌──────────────────────────┐
│  TraitCatalogService    │     │   AskQuestion() handler  │
│  (trait definitions)    │────▶│   Combines:              │
│  - type, tier, cost     │     │   - Definition (schema)  │
│  - isEncrypted, isFlaky │     │   - Value (from session) │
└─────────────────────────┘     │   → TraitAnswer response │
                                └──────────────────────────┘
```

### The Three Trait Tiers and Their Behavior

| Tier | Trait IDs | `IsEncrypted` | `IsFlaky` | Cost | Behavior |
|------|-----------|---------------|-----------|------|----------|
| **basic** | T01–T28, T30–T35, T38–T42, T44–T47, T51–T55, T57–T60 | `false` | `false` | 1 | Answer returned in plaintext |
| **encrypted** | T29, T36, T37, T43, T48, T56 | `true` | `false` | 2 | Answer base64-encoded; requires `/decode` call |
| **flaky** | T49, T50, T62, T63 | `false` | `true` | 3–4 | May return `"degraded"` during chaos windows |
| **flaky + encrypted** | T61, T64 | `true` | `true` | 4 | Both behaviors: may degrade AND encrypts when successful |

### Key Design Decisions

1. **Separation of schema and data**: Trait definitions (types, tiers, costs) are hardcoded in Go, while trait values are in a JSON file. This means adding a new character doesn't require code changes, but adding a new trait type does.

2. **Per-session copies**: Characters are deep-copied into each session so that any in-flight mutations to trait data don't affect the master catalog or other sessions.

3. **Deterministic ordering**: Both [`GetAllCharacters()`](../internal/service/character_catalog.go) and [`GetAllTraits()`](../internal/service/trait_catalog.go) sort their keys lexicographically before returning, ensuring stable iteration order across different Go map implementations.

4. **Board hides identity**: The [`GetBoard()`](../internal/handler/session_handler.go) handler deliberately strips `candidateId`, `displayName`, and `imagePath` from the response, returning only the traits map for each board position. Players must identify characters solely by their trait combinations.

5. **Redis as session store**: Trait values are duplicated into Redis per session rather than looked up from the catalog at query time. This ensures consistency even if the server restarts with different character data mid-session.