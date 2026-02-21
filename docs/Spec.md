# Degrees of Separation — Spec (v2)
**Date:** 2026-02-21

## Overview

A web application that finds the shortest path (degrees of separation) between any two actors based on shared movie credits. Built on a graph database populated from TMDb data. Designed as a portfolio piece demonstrating backend engineering, graph modeling, data pipelines, and production operations.

## Core Features

### Actor Search
- Prefix autocomplete search for actor names (type "Leo" → "Leonardo DiCaprio")
- Two search inputs: Actor A and Actor B
- Actor B defaults to Kevin Bacon but is user-selectable
- Debounced typeahead: fires after 300ms of inactivity

### Degrees Query
- Returns the shortest path between two actors
- Displays the chain: Actor → Movie → Actor → Movie → ... → Actor
- Shows the degree count (number of hops)
- Handles edge cases: same actor, no path found, actor not in dataset

### Stats Dashboard
- Total actors and movies in the graph
- Most connected actor (highest degree)
- Average degrees of separation (sampled)
- Dataset freshness (last ingestion timestamp)

## Tech Stack

| Component       | Choice                                    |
|-----------------|-------------------------------------------|
| Language        | Go                                        |
| Graph DB        | Neo4j (local via Docker Compose, self-managed on GCP) |
| Frontend        | HTMX + Go templates + minimal CSS         |
| Data Source     | TMDb API                                  |
| Hosting         | GCP (Cloud Run + Compute Engine Neo4j)    |
| Observability   | OpenTelemetry + Cloud Monitoring          |
| CI/CD           | GitHub Actions                            |

## Data Model

### Nodes
- **Actor**: `name`, `tmdb_id`, `profile_path` (optional headshot URL)

### Edges
- **COSTARRED**: between two Actor nodes, properties: `movie_title`, `tmdb_movie_id`, `year`

### Ingestion Logic
1. Fetch movies from TMDb (paginated): popular/top-rated for MVP, full catalog via `/discover/movie` for complete coverage
2. For each movie, fetch cast list (capped at top N billed to manage pairwise edge volume)
3. Upsert Actor nodes by `tmdb_id`
4. Create COSTARRED edges between all pairs of actors in the cast
5. Track ingestion progress for resumability

### Dataset Scope
- **MVP:** Popular and top-rated movies from TMDb (~500 pages, thousands of movies)
- **Target:** Full TMDb catalog (hundreds of thousands of movies, 900k+ actors)
- Ingestion is incremental and resumable to support full catalog scale

## API Endpoints

| Method | Path                  | Description                        |
|--------|-----------------------|------------------------------------|
| GET    | `/`                   | Main page with search UI           |
| GET    | `/search?q=`          | Actor prefix autocomplete (returns HTMX fragment) |
| GET    | `/degrees?a=&b=`      | Shortest path result (returns HTMX fragment) |
| GET    | `/stats`              | Stats dashboard (returns HTMX fragment) |
| GET    | `/healthz`            | Liveness probe                     |
| GET    | `/readyz`             | Readiness probe (checks Neo4j connection) |
| GET    | `/metrics`            | Prometheus metrics endpoint        |

## Development Environment

- Local Neo4j via `docker-compose.dev.yaml` — no remote DB dependency for development
- `Makefile` with targets for common workflows (`dev-up`, `seed`, `test`, etc.)
- All development and testing runs against the local graph

## Production Readiness Requirements

### Error Handling
- Structured logging (slog) with request context
- Graceful degradation when Neo4j is unavailable
- User-facing error messages that don't leak internals
- Panic recovery middleware

### Rate Limiting
- Per-IP rate limiting on API endpoints (`golang.org/x/time/rate`)
- TMDb API rate limiting in the ingestion pipeline (respect their 40 req/10s limit)

### Security
- Input sanitization on search queries (Cypher injection prevention via parameterized queries)
- CORS headers configured for production origin
- Request timeout middleware

### Health & Diagnostics
- `/healthz` for liveness (app is running)
- `/readyz` for readiness (Neo4j is reachable, dataset is loaded)
- Structured request logging with trace IDs

## Non-Goals
- User accounts or authentication
- Real-time data updates (batch ingestion is sufficient)
- Mobile-specific UI (responsive CSS is fine, no native app)

## Future Enhancements
- Fuzzy matching (Levenshtein distance) for typo tolerance in actor search
- Actor profile images (headshots from TMDb)
- "Random pair" button for exploration
- Interactive graph visualization of the path (D3.js or similar)