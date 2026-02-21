# Degrees of Separation

Find the shortest path between any two actors based on shared movie credits. Type in two actor names and discover how they're connected through the films they've appeared in.

Built with Go, Neo4j, and HTMX. Data sourced from [TMDb](https://www.themoviedb.org/).

## Quick Start

```bash
cp .env.example .env  # add your TMDb API key
make up               # start Neo4j and server
make seed             # load a small dataset
```

Server at http://localhost:8080, Neo4j browser at http://localhost:7474.

For live reload during development:

```bash
make watch
```

Run `make help` to see all available targets.

## Tech Stack

- **Go** — HTTP server, ingestion pipeline, graph queries
- **Neo4j** — graph database for actor relationships
- **HTMX** — lightweight frontend interactivity
- **GCP** — Cloud Run + Compute Engine for production hosting

## Project Layout

```
cmd/server/          Web server entrypoint
cmd/ingest/          Batch ingestion CLI
internal/            Application packages (graph, tmdb, handlers, middleware)
web/                 Templates and static assets
deploy/              Dockerfile, Terraform, CI/CD config
docs/                Spec and implementation plan
```

## Documentation

- [Spec](docs/Spec.md) — features, data model, API endpoints
- [Implementation Plan](docs/Implementation-Plan.md) — phased build plan with task checklists

## License

MIT — see [LICENSE](LICENSE) for details.