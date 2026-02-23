# Degrees of Separation — Implementation Plan (v2)

**Date:** 2026-02-21

See: [[Degrees of Separation - Spec]]

## Changes from v1

- **Dev environment first:** Docker Compose with local Neo4j introduced in Phase 1 (was Phase 4). All development targets a local graph — no remote DB dependency.
- **Production containerization** stays in Phase 4 (multi-stage Dockerfile, GCP deployment).
- **Search clarified:** Prefix autocomplete as core behavior; fuzzy matching deferred to future enhancement.
- **Full dataset as goal:** Ingestion designed for full TMDb catalog (incremental, resumable). Popular movies scoped as MVP; full coverage follows.

## Project Structure

```
degrees/
├── cmd/
│   ├── server/             # Web server entrypoint
│   └── ingest/             # Batch ingestion CLI
├── internal/
│   ├── graph/              # Neo4j queries and connection management
│   ├── tmdb/               # TMDb API client
│   ├── handler/            # HTTP handlers
│   └── middleware/         # Rate limiting, logging, recovery, timeouts
├── web/
│   ├── templates/          # Go HTML templates
│   └── static/             # CSS, minimal JS (HTMX loaded via CDN)
├── deploy/
│   ├── Dockerfile          # Production multi-stage build (Phase 4)
│   ├── cloudbuild.yaml
│   └── terraform/          # GCP infrastructure
├── scripts/
│   └── seed.sh             # Quick-start data load
├── docker-compose.dev.yaml # Local Neo4j + dev tooling
├── Makefile                # dev-up, dev-down, ingest, seed, test, etc.
├── go.mod
└── README.md
```

## Phase 1 — Dev Environment & Data Layer

### 1.0 Dev Environment

- [x] Create `docker-compose.yaml` with Neo4j 5 service
    - Image: `neo4j:latest`
    - Ports: `7474:7474` (browser UI), `7687:7687` (bolt)
    - Environment: `NEO4J_AUTH=neo4j/devpassword`
    - Named volume for data persistence across restarts
- [x] Create `Makefile` with targets:
    - **Dev environment:** `up`, `down`, `reset` — manage local Neo4j via Docker Compose
    - **Data:** `ingest` — run full ingestion; `seed` — ingest small dataset for quick dev
    - **Go development:** `build`, `run`, `fmt`, `vet`, `fix`, `tidy`, `clean`
    - **Testing:** `test` — unit tests; `test-integration` — integration tests (requires Neo4j); `coverage` — tests with HTML coverage report
    - **Quality:** `validate` — format, vet, and test in one command
    - `help` — list all targets (default)
- [x] Add `.env.example` with local defaults (`NEO4J_URI`, `NEO4J_USER`, `NEO4J_PASSWORD`, `TMDB_API_KEY`)
- [x] Document dev setup in `README.md` (clone → fill in `.env` → `make up` → `make seed`)

### 1.1 TMDb Client

- [x] Create `internal/tmdb/` client with API key config
- [x] Implement `GetPopularMovies(page int)` with pagination (MVP)
- [x] Implement `GetMovieCast(movieID int)` returning cast list
    - Accept a `maxCast` parameter to cap pairwise edge explosion (default: top 20 billed)
- [x] Add rate limiter: token bucket at 40 requests per 10 seconds
- [x] Add retry with exponential backoff on 429s
- [x] Write integration tests against TMDb (skippable via build tag)

### 1.2 Neo4j Graph Layer

- [x] Create `internal/graph/` with connection pool setup
- [x] Implement `UpsertActor(tmdbID int, name string)`
- [x] Implement `CreateCostarEdge(actorA, actorB, movieTitle, movieID)`
- [x] Implement `ShortestPath(actorA, actorB string)` using Cypher `shortestPath`
- [x] Implement `SearchActors(prefix string, limit int)` — prefix autocomplete via full-text index
    - Uses Neo4j full-text index with query like `Leo*` for prefix matching
    - Returns top N matches sorted by relevance
    - Future enhancement: add Levenshtein-based fuzzy matching for typo tolerance
- [x] Implement `GetStats()` — counts, most connected, avg degree
- [x] Add Neo4j constraints: unique on `Actor.tmdb_id`
- [x] Add Neo4j index: full-text index on `Actor.name` for prefix search
- [x] Connection health check method for readiness probe
- [x] All queries use parameterized Cypher (no string interpolation — Cypher injection prevention)

### 1.3 Ingestion CLI

- [x] Create `cmd/ingest/` entrypoint
- [x] Accept flags: `--pages` (how many TMDb pages to ingest), `--max-cast` (cast cap per movie), `--resume`
- [x] For each movie: fetch cast, upsert actors, create pairwise edges (up to `maxCast`)
- [x] Store ingestion watermark in Neo4j (last page processed) for resumability
- [x] Log progress: movies processed, actors created, edges created
- [x] Graceful shutdown on SIGINT (finish current movie, then stop)

## Phase 2 — Web Server

### 2.1 HTTP Server Setup

- [ ] Create `cmd/server/` entrypoint with graceful shutdown
- [ ] Wire up `net/http` with `http.ServeMux` (stdlib is fine)
- [ ] Embed `web/` directory with `go:embed`
- [ ] Config via environment variables: `PORT`, `NEO4J_URI`, `NEO4J_USER`, `NEO4J_PASSWORD`

### 2.2 Middleware Stack

- [ ] Request logging middleware (slog, structured, with request ID)
- [ ] Panic recovery middleware
- [ ] Request timeout middleware (10s default)
- [ ] Per-IP rate limiter middleware (`golang.org/x/time/rate`, ~30 req/min)
- [ ] CORS middleware

### 2.3 Handlers

- [ ] `GET /` — render main page template
- [ ] `GET /search?q=` — return HTMX fragment with actor suggestions (prefix autocomplete)
- [ ] `GET /degrees?a=&b=` — return HTMX fragment with path result
- [ ] `GET /stats` — return HTMX fragment with graph stats
- [ ] `GET /healthz` — return 200
- [ ] `GET /readyz` — check Neo4j connectivity, return 200/503

### 2.4 Templates & Frontend

- [ ] Base layout template (HTML head, HTMX script tag, minimal CSS)
- [ ] Search input component with `hx-get="/search"` and `hx-trigger="keyup changed delay:300ms"`
    - Dropdown renders clickable actor names below the input
    - Selecting an actor populates a hidden input with the actor's `tmdb_id`
- [ ] Results display: chain of Actor → Movie → Actor → Movie → ... → Actor with names and years
- [ ] Stats section loaded via `hx-get="/stats"` on page load
- [ ] Error state fragments (no results, service unavailable)
- [ ] Classless CSS framework (pico.css or similar) to avoid writing CSS

## Phase 3 — Production Hardening

### 3.1 Observability

- [ ] Add OpenTelemetry SDK (`go.opentelemetry.io/otel`)
- [ ] Instrument HTTP handlers with tracing spans
- [ ] Instrument Neo4j queries with tracing spans
- [ ] Add Prometheus metrics via `promhttp`:
    - `http_requests_total` (counter, labels: method, path, status)
    - `http_request_duration_seconds` (histogram)
    - `neo4j_query_duration_seconds` (histogram, label: query_name)
    - `graph_actors_total` (gauge, updated periodically)
    - `graph_edges_total` (gauge)
    - `ingest_movies_processed_total` (counter)
- [ ] Export traces to Cloud Trace (OTLP exporter)
- [ ] `GET /metrics` endpoint for Prometheus scraping

### 3.2 Structured Logging

- [ ] Use `log/slog` throughout with JSON handler in production
- [ ] Include `request_id`, `method`, `path`, `status`, `duration` on every request
- [ ] Log Neo4j query durations at debug level
- [ ] Log errors with stack context
- [ ] Ship logs to Cloud Logging (stdout in Cloud Run is auto-captured)

### 3.3 Error Handling

- [ ] Define app-level error types (`ErrNotFound`, `ErrNeo4jUnavailable`, `ErrRateLimited`)
- [ ] Map errors to appropriate HTTP status codes in a central handler
- [ ] HTMX error fragments that render user-friendly messages
- [ ] Never expose internal error details to the client

### 3.4 Testing

- [ ] Unit tests for handler logic (mock graph layer)
- [ ] Integration tests for Neo4j queries (`testcontainers-go` with Neo4j image)
- [ ] End-to-end test: ingest small dataset → query degrees → verify path
- [ ] Add `make test` and `make test-integration` targets

## Phase 4 — GCP Deployment

### 4.1 Production Containerization

- [ ] Multi-stage Dockerfile: build with `golang:1.22` → run with `gcr.io/distroless/static`
- [ ] Embed web assets at build time
- [ ] Health check in Dockerfile

### 4.2 Infrastructure (Terraform)

- [ ] GCP project setup with required APIs enabled
- [ ] Cloud Run service for the web server
    - Min instances: 0 (scale to zero for cost)
    - Max instances: 2 (portfolio project, keep it cheap)
    - Memory: 256MB
    - CPU: 1
- [ ] Neo4j on Compute Engine (self-managed)
    - `e2-small` or `e2-medium` instance (depending on dataset size)
    - Neo4j Community Edition in Docker (or installed directly)
    - Persistent disk for graph data
    - Firewall: bolt port open only to Cloud Run egress / your IP
    - Startup script for Neo4j provisioning
    - Snapshot schedule for backups
- [ ] Artifact Registry for container images
- [ ] Secret Manager for Neo4j credentials and TMDb API key
- [ ] Cloud Run service account with least-privilege IAM

### 4.3 CI/CD (GitHub Actions)

- [ ] On push to `main`:
    1. Run tests
    2. Build container image
    3. Push to Artifact Registry
    4. Deploy to Cloud Run
- [ ] On pull request: run tests only
- [ ] Cache Go modules and build cache

### 4.4 DNS & TLS

- [ ] Cloud Run provides HTTPS by default on `*.run.app`
- [ ] Optional: custom domain via Cloud Run domain mapping

## Phase 5 — SRE / Ops Extras

These are portfolio-value additions that demonstrate operational maturity.

### 5.1 SLO Definition

- [ ] Define SLOs in a `slo.yaml` or doc:
    - Availability: 99% (measured via Cloud Monitoring uptime check)
    - Latency: p95 < 500ms for `/degrees` queries
    - Error rate: < 1% of requests return 5xx
- [ ] Create Cloud Monitoring alert policies for SLO breaches

### 5.2 Dashboarding

- [ ] Cloud Monitoring dashboard with:
    - Request rate
    - Error rate (4xx, 5xx)
    - Latency percentiles (p50, p95, p99)
    - Neo4j query latency
    - Active connections to Neo4j
    - Cloud Run instance count
    - Ingestion progress (if running)

### 5.3 Alerting

- [ ] PagerDuty or email alerts (keep it simple) for:
    - Readiness probe failures (Neo4j down)
    - Error rate spike (>5% 5xx over 5 min)
    - Latency spike (p95 > 2s over 5 min)
- [ ] Alert on ingestion job failures (if run as Cloud Run Job)

### 5.4 Runbook

- [ ] Create a `docs/runbook.md` covering:
    - How to trigger a data re-ingestion
    - How to check Neo4j health
    - How to roll back a bad deploy
    - How to investigate slow queries (Cypher EXPLAIN/PROFILE)
    - How to scale up if traffic spikes

### 5.5 Chaos / Resilience

- [ ] Test behavior when Neo4j is unreachable (graceful error pages)
- [ ] Test behavior under rate limiting (verify 429 responses)
- [ ] Load test with `hey` or `vegeta` to find breaking point
- [ ] Document findings in runbook

### 5.6 Batch Job Operations

- [ ] Run ingestion as a Cloud Run Job (separate from the web service)
- [ ] Cloud Scheduler trigger: weekly re-ingestion of new movies
- [ ] Job logs and metrics visible in same dashboard
- [ ] Idempotent ingestion: safe to re-run without duplicating data

## Build Order

1. **Dev environment & data layer** — `docker compose up`, get data flowing into local Neo4j
2. **Web server** — serve queries over HTTP with HTMX, iterate against local graph
3. **Production hardening** — observability, error handling, tests
4. **GCP deployment** — production Dockerfile, Terraform, ship it
5. **SRE extras** — SLOs, dashboards, alerting, runbook

## Future Enhancements

- Fuzzy search (Levenshtein distance) for typo tolerance
- Full TMDb catalog ingestion via `/discover/movie` endpoint
- Actor profile images (headshots from TMDb)
- "Random pair" button for exploration
- Visualization of the path as an interactive graph (D3.js or similar)