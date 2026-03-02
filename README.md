# guesswhoservice

The Go backend service for **Guess Who: Identity Under Fire** — a competitive team-based API challenge.

## Overview

`guesswhoservice` is the authoritative game engine. It manages all game state, enforces rules, handles scoring, and persists data to Redis. The service exposes a REST API consumed by the Next.js frontend (`guesswhoui`) and directly by competing teams.

## Quick Start

### Prerequisites

- Go 1.22+
- Redis (local or remote)

### Run Locally

```bash
cd guesswhoservice

# Set required environment variables
export REDIS_URL=redis://localhost:6379
export JWT_SECRET=your-secret-here
export CHAOS_API_KEY=your-chaos-key
export DEBUG_API_KEY=your-debug-key

# Run the service
go run guesswhoserviceapi.go
```

The service starts on port `8080` by default (configurable via `PORT` env var).

### Run with Docker

```bash
docker build -t guesswhoservice .
docker run -p 8080:8080 \
  -e REDIS_URL=redis://host.docker.internal:6379 \
  -e JWT_SECRET=your-secret \
  guesswhoservice
```

## Configuration

| Environment Variable | Description | Required |
|---------------------|-------------|----------|
| `REDIS_URL` | Redis connection URL | Yes |
| `JWT_SECRET` | JWT signing secret | Yes |
| `CHAOS_API_KEY` | API key for chaos endpoints | Yes |
| `DEBUG_API_KEY` | API key for debug endpoints | Yes |
| `CHAOS_ENABLED` | Enable chaos system (`true`/`false`) | No (default: `false`) |
| `CORS_ALLOWED_ORIGINS` | Comma-separated allowed origins | No |
| `PORT` | HTTP server port | No (default: `8080`) |

## API

See [`docs/api-reference.md`](docs/api-reference.md) for the complete API reference.

**Endpoint groups:**
- `/sessions/*` — game session management (start, ask, guess, reveal)
- `/questions` — trait question catalogue
- `/client/*` — team auth, leaderboard, master board, team progress
- `/chaos/*` — chaos injection (admin)
- `/debug/*` — debug utilities (admin)
- `/events` — Server-Sent Events stream

## Documentation

| Document | Description |
|----------|-------------|
| [`docs/architecture.md`](docs/architecture.md) | System architecture, data flow, domain model, Redis schema |
| [`docs/api-reference.md`](docs/api-reference.md) | Complete API reference with request/response shapes |
| [`docs/security.md`](docs/security.md) | Security measures: JWT, encryption, rate limiting, obfuscation |
| [`docs/chaos-system.md`](docs/chaos-system.md) | Chaos injection system |
| [`docs/scoring-milestones.md`](docs/scoring-milestones.md) | Scoring formula, milestones, leaderboard |
| [`docs/infrastructure.md`](docs/infrastructure.md) | GCP infrastructure, Terraform, deployment |
| [`docs/game_instructions.md`](docs/game_instructions.md) | Player-facing game instructions |
| [`docs/To-do-enhancements.md`](docs/To-do-enhancements.md) | Planned enhancements and future work |

## Project Structure

```
guesswhoservice/
├── guesswhoserviceapi.go       # Entry point, route registration, server setup
├── config/
│   └── config.go               # Environment variable loading
├── data/
│   └── characters.json         # 64 character definitions with trait data
├── internal/
│   ├── domain/                 # Core data types
│   │   ├── session.go
│   │   ├── candidate.go
│   │   ├── trait.go
│   │   ├── score.go
│   │   └── milestone.go
│   ├── handler/                # HTTP handlers
│   │   ├── session_handler.go
│   │   ├── trait_handler.go
│   │   ├── client_handler.go
│   │   ├── chaos_handler.go
│   │   ├── debug_handler.go
│   │   └── errors.go
│   ├── service/                # Business logic
│   │   ├── session_service.go
│   │   ├── scoring.go
│   │   ├── chaos.go
│   │   ├── encryption.go
│   │   ├── board_generator.go
│   │   ├── character_catalog.go
│   │   ├── trait_catalog.go
│   │   └── milestone_service.go
│   ├── db/
│   │   └── redis_store.go      # Redis data access layer
│   ├── middleware/
│   │   ├── auth_middleware.go  # JWT validation
│   │   └── rate_limiter.go     # Token-bucket rate limiting
│   └── logging/                # Structured JSON logging
└── docs/                       # Documentation
```

## Testing

```bash
go test ./...
```

Key test files:
- `internal/service/scoring_test.go` — scoring formula tests
- `internal/service/board_generator_test.go` — board generation tests
- `internal/service/encryption_test.go` — encryption/decryption tests