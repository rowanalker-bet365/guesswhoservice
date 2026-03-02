# Infrastructure

The Guess Who platform is deployed on Google Cloud Platform (GCP) using Terraform. Infrastructure is defined as code in the `terraform/` directory.

---

## Architecture

```
GCP Project
├── Cloud Run
│   ├── guesswhoservice  (Go backend)
│   └── guesswhoui       (Next.js frontend)
├── Artifact Registry    (container images)
├── Redis (Memorystore)  (game state + pub/sub)
└── GitHub Actions Runner (CI/CD)
```

---

## Environments

### Development (`terraform/environments/dev/`)

The dev environment provisions all infrastructure components. State is stored in a GCS backend (`terraform/environments/dev/backend.tf`).

**Key resources:**
- Cloud Run services for both `guesswhoservice` and `guesswhoui`
- Redis instance (Memorystore)
- Artifact Registry for container images
- IAM bindings for service accounts
- GitHub Actions self-hosted runner

### Production (`terraform/environments/prod/`)

Production uses a separate GCS backend for state isolation. The prod environment mirrors the dev structure with production-appropriate sizing and configuration.

---

## Terraform Modules

### `terraform/modules/guesswhoservice/`

Provisions the Go backend Cloud Run service:
- Cloud Run service with configurable image, CPU, memory, and concurrency
- Environment variable injection (Redis URL, JWT secret, API keys, etc.)
- Service account with minimal required permissions

### `terraform/modules/guesswhoui/`

Provisions the Next.js frontend Cloud Run service:
- Cloud Run service with configurable image
- Environment variable injection (`NEXT_PUBLIC_GUESSWHOSERVICE_URL`, `REDIS_URL`, etc.)
- Public ingress (unauthenticated access)

### `terraform/modules/gcr/`

Provisions Google Artifact Registry:
- Docker repository for storing container images
- IAM bindings for push/pull access

### `terraform/modules/github-runner/`

Provisions a self-hosted GitHub Actions runner on GCP:
- Compute instance running the GitHub Actions runner agent
- Used for CI/CD pipelines (build, push, deploy)
- Configured with appropriate IAM roles for Cloud Run deployment and Artifact Registry push

---

## GCP APIs Enabled

The following GCP APIs are enabled in `terraform/environments/dev/apis.tf`:

- `run.googleapis.com` — Cloud Run
- `artifactregistry.googleapis.com` — Artifact Registry
- `redis.googleapis.com` — Memorystore for Redis
- `compute.googleapis.com` — Compute Engine (for GitHub runner)
- `iam.googleapis.com` — IAM
- `cloudresourcemanager.googleapis.com` — Resource Manager

---

## IAM

Service accounts and roles are defined in `terraform/environments/dev/iam.tf`:

- **Cloud Run service account** — runs both Cloud Run services; has access to Secret Manager secrets and Redis
- **GitHub runner service account** — used by CI/CD; has `roles/run.admin`, `roles/artifactregistry.writer`, and `roles/iam.serviceAccountUser`

---

## Environment Variables

### guesswhoservice

| Variable | Description |
|----------|-------------|
| `REDIS_URL` | Redis connection URL (e.g. `redis://host:6379`) |
| `JWT_SECRET` | Secret for signing/verifying JWT tokens |
| `CHAOS_API_KEY` | API key for chaos endpoints |
| `DEBUG_API_KEY` | API key for debug endpoints |
| `CHAOS_ENABLED` | Enable chaos system (`true`/`false`) |
| `CORS_ALLOWED_ORIGINS` | Comma-separated list of allowed CORS origins |
| `PORT` | HTTP server port (default: `8080`) |

### guesswhoui

| Variable | Description |
|----------|-------------|
| `NEXT_PUBLIC_GUESSWHOSERVICE_URL` | Base URL of the Go backend service |
| `REDIS_URL` | Redis connection URL (used by SSE route for pub/sub) |

---

## Deployment

Container images are built and pushed to Artifact Registry via GitHub Actions (using the self-hosted runner). Cloud Run services are updated by deploying new image revisions.

**Build and deploy flow:**
1. Push to main branch triggers GitHub Actions workflow
2. Runner builds Docker images for `guesswhoservice` and `guesswhoui`
3. Images are pushed to Artifact Registry
4. Cloud Run services are updated with the new image revision
5. Traffic is automatically routed to the new revision

### Dockerfiles

- `guesswhoservice/Dockerfile` — multi-stage Go build; produces a minimal binary image
- `guesswhoui/Dockerfile` — Next.js standalone build (`output: 'standalone'` in `next.config.mjs`)