# Guess Who: Identity Under Fire - Hackathon API Service

A modular Go service that provides an API for a "Guess Who" hackathon challenge where students build apps to identify hidden targets from randomly generated boards.

## Features

- **Session Management**: Create and manage game sessions with unique boards
- **64 Candidates**: Randomly generated fictional candidates per session
- **64 Trait Questions**: Wide variety of traits (appearance, clothing, tech preferences, etc.)
- **Encryption**: Some answers returned as encrypted payloads
- **Resilience Testing**: Flaky endpoints to reward robust implementations
- **Scoring System**: Points for correct solves, time bonuses, efficiency rewards
- **Leaderboard**: Track team performance across multiple solves

## Project Structure

```
GuessWho/
├── cmd/
│   └── server/           # Application entry point
├── internal/
│   ├── domain/           # Domain models
│   ├── service/          # Business logic services
│   ├── handler/          # HTTP handlers
│   ├── repository/       # Data storage (in-memory)
│   └── middleware/       # HTTP middleware
├── pkg/
│   ├── config/           # Configuration management
│   └── util/             # Utility functions
└── go.mod
```

## Architecture

The service follows clean architecture principles with dependency injection:

- **Domain Layer**: Core business entities (Session, Candidate, Trait)
- **Service Layer**: Business logic (session management, board generation, scoring)
- **Handler Layer**: HTTP request/response handling
- **Repository Layer**: Data persistence (in-memory for hackathon)

## API Endpoints

### Session Management
- `POST /v1/sessions/start` - Start a new game session
- `GET /v1/sessions/{sessionId}/board` - Fetch the candidate board
- `GET /v1/sessions/{sessionId}/questions` - List available questions

### Gameplay
- `POST /v1/sessions/{sessionId}/ask` - Ask a trait question about the target
- `POST /v1/decode` - Decode encrypted answers
- `POST /v1/sessions/{sessionId}/guess` - Submit final guess

### Leaderboard
- `GET /v1/leaderboard` - View team rankings

## Running the Service

```bash
# Install dependencies
go mod download

# Run the service
go run cmd/server/main.go

# Run tests
go test ./...

# Run with custom configuration
PORT=8080 go run cmd/server/main.go
```

## Configuration

Environment variables:
- `PORT`: Server port (default: 8080)
- `RATE_LIMIT_ENABLED`: Enable rate limiting (default: true)
- `CHAOS_ENABLED`: Enable failure injection (default: true)
- `CHAOS_INTERVAL_SECONDS`: Time between chaos windows (default: 240)
- `CHAOS_WINDOW_SECONDS`: Duration of chaos window (default: 90)

## Testing

The service includes comprehensive unit tests with mocked dependencies:

```bash
# Run all tests
go test ./... -v

# Run tests with coverage
go test ./... -cover

# Run specific package tests
go test ./internal/service/... -v
```

## Milestones

Students progress through these milestones:
- **M1**: First Round Started
- **M2**: First Successful Question
- **M3**: Elimination Working
- **M4**: Encrypted Answer Handled
- **M5**: Resilience (progress during failures)
- **M6**: First Correct Solve

## Scoring

- Correct solve: +1000 points
- Time bonus: max(0, 600 - seconds)
- Question efficiency: max(0, 300 - 20 * questions)
- Reliability penalty: deductions for failures

## License

MIT License