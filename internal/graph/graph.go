// Package graph wraps the Neo4j driver with OTel instrumentation.
// It imports only the OTel API, not the SDK — the SDK is wired in by
// internal/telemetry.
package graph

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j"

	"github.com/mark-c-hall/degrees-of-separation/internal/config"
	"github.com/mark-c-hall/degrees-of-separation/internal/models"
)

// Driver wraps the Neo4j driver with OTel tracing and metrics instruments.
type Driver struct {
	driver        neo4j.Driver
	tracer        trace.Tracer
	queryDuration metric.Float64Histogram
	actorsGauge   metric.Int64ObservableGauge
	edgesGauge    metric.Int64ObservableGauge
}

type PathStep struct {
	Actor      *models.Actor
	MovieTitle string
	MovieYear  int
}

type Stats struct {
	ActorCount         int
	EdgeCount          int
	MostConnectedActor string
	MostConnectedCount int
}

func NewDriver(ctx context.Context, cfg config.Config) (*Driver, error) {
	driver, err := neo4j.NewDriver(
		cfg.DB.URI,
		neo4j.BasicAuth(cfg.DB.User, cfg.DB.Pass, ""),
	)
	if err != nil {
		return nil, fmt.Errorf("error creating neo4j driver: %w", err)
	}

	if err = driver.VerifyAuthentication(ctx, nil); err != nil {
		return nil, fmt.Errorf("error authenticating into neo4j: %w", err)
	}

	d := &Driver{driver: driver}

	// Instruments are resolved against the global providers set by internal/telemetry.
	meter := otel.Meter("degrees-of-separation/graph")
	d.tracer = otel.Tracer("degrees-of-separation/graph")

	d.queryDuration, err = meter.Float64Histogram("neo4j.query.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of Neo4j Cypher queries"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1.0),
	)
	if err != nil {
		return nil, fmt.Errorf("creating query duration histogram: %w", err)
	}

	d.actorsGauge, err = meter.Int64ObservableGauge("graph.actors.total",
		metric.WithDescription("Total number of Actor nodes in the graph"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating actors gauge: %w", err)
	}

	d.edgesGauge, err = meter.Int64ObservableGauge("graph.edges.total",
		metric.WithDescription("Total number of COSTARRED edges in the graph"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating edges gauge: %w", err)
	}

	// Gauge callback fires on each Prometheus scrape, not per-request.
	// Uses a lightweight count query instead of GetStats to avoid the
	// expensive per-node degree computation running every 15 seconds.
	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		counts, err := d.GetCounts(ctx)
		if err != nil {
			return nil // don't fail the scrape if the DB is temporarily down
		}
		o.ObserveInt64(d.actorsGauge, int64(counts[0]))
		o.ObserveInt64(d.edgesGauge, int64(counts[1]))
		return nil
	}, d.actorsGauge, d.edgesGauge)
	if err != nil {
		return nil, fmt.Errorf("registering gauge callback: %w", err)
	}

	return d, nil
}

func (d *Driver) SetupSchema(ctx context.Context) error {
	queries := []string{
		"CREATE CONSTRAINT actor_tmdb_id IF NOT EXISTS FOR (a:Actor) REQUIRE a.tmdb_id IS UNIQUE",
		"CREATE FULLTEXT INDEX actor_name IF NOT EXISTS FOR (a:Actor) ON EACH [a.name]",
	}

	session := d.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	for _, query := range queries {
		if _, err := session.Run(ctx, query, nil); err != nil {
			return fmt.Errorf("error running schema query: %w", err)
		}
	}

	return nil
}

func (d *Driver) Close(ctx context.Context) error {
	return d.driver.Close(ctx)
}

func (d *Driver) VerifyConnectivity(ctx context.Context) error {
	return d.driver.VerifyConnectivity(ctx)
}

func (d *Driver) UpsertActor(ctx context.Context, actor models.Actor) error {
	cypher := "MERGE (a:Actor {tmdb_id: $id}) SET a.name = $name"
	params := map[string]any{"id": actor.TmdbID, "name": actor.Name}

	session := d.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	_, err := session.Run(ctx, cypher, params)
	if err != nil {
		return fmt.Errorf("error upserting actor: %w", err)
	}

	return nil
}

func (d *Driver) CreateCostarEdge(ctx context.Context, actorA, actorB int, movie models.Movie) error {
	cypher := `
		MATCH (a:Actor {tmdb_id: $idA}), (b:Actor {tmdb_id: $idB})
		MERGE (a)-[r:COSTARRED {tmdb_movie_id: $movieID}]->(b)
		SET r.movie_title = $title, r.year = $year`
	params := map[string]any{
		"idA":     actorA,
		"idB":     actorB,
		"movieID": movie.TmdbID,
		"title":   movie.Title,
		"year":    movie.Year,
	}

	session := d.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	_, err := session.Run(ctx, cypher, params)
	if err != nil {
		return fmt.Errorf("error creating costar edge: %w", err)
	}

	return nil
}

// IngestMovieCast upserts actors and their co-star edges in a single write transaction.
func (d *Driver) IngestMovieCast(ctx context.Context, movie models.Movie, cast []models.Actor) error {
	cypher := `UNWIND $actors AS a MERGE (act:Actor {tmdb_id: a.id}) SET act.name = a.name`
	start := time.Now()
	ctx, span := d.tracer.Start(ctx, "neo4j.IngestMovieCast",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.DBSystemNeo4j,
			semconv.DBQueryText(cypher),
			attribute.Int("cast.size", len(cast)),
		),
	)
	defer func() {
		d.queryDuration.Record(ctx, time.Since(start).Seconds(),
			metric.WithAttributes(attribute.String("query_name", "IngestMovieCast")))
		span.End()
	}()

	actors := make([]map[string]any, len(cast))
	for i, a := range cast {
		actors[i] = map[string]any{"id": a.TmdbID, "name": a.Name}
	}

	n := len(cast)
	pairs := make([]map[string]any, 0, n*(n-1)/2)
	for i := 0; i < n-1; i++ {
		for j := i + 1; j < n; j++ {
			pairs = append(pairs, map[string]any{
				"idA":     cast[i].TmdbID,
				"idB":     cast[j].TmdbID,
				"movieID": movie.TmdbID,
				"title":   movie.Title,
				"year":    movie.Year,
			})
		}
	}

	session := d.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx,
			"UNWIND $actors AS a MERGE (act:Actor {tmdb_id: a.id}) SET act.name = a.name",
			map[string]any{"actors": actors},
		)
		if err != nil {
			return nil, fmt.Errorf("error batch upserting actors: %w", err)
		}

		if len(pairs) > 0 {
			_, err = tx.Run(ctx,
				`UNWIND $pairs AS p
				 MATCH (a:Actor {tmdb_id: p.idA}), (b:Actor {tmdb_id: p.idB})
				 MERGE (a)-[r:COSTARRED {tmdb_movie_id: p.movieID}]->(b)
				 SET r.movie_title = p.title, r.year = p.year`,
				map[string]any{"pairs": pairs},
			)
			if err != nil {
				return nil, fmt.Errorf("error batch creating costar edges: %w", err)
			}
		}

		return nil, nil
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetAttributes(attribute.Int("pairs.count", len(pairs)))
	return nil
}

// ShortestPath finds the shortest co-star chain between two actors.
func (d *Driver) ShortestPath(ctx context.Context, actorA, actorB int) ([]PathStep, error) {
	cypher := `
		MATCH (a:Actor {tmdb_id: $idA}), (b:Actor {tmdb_id: $idB}),
		      p = shortestPath((a)-[:COSTARRED*]-(b))
		RETURN [n IN nodes(p) | {id: n.tmdb_id, name: n.name}] AS actors,
		       [r IN relationships(p) | {title: r.movie_title, year: r.year}] AS movies`

	start := time.Now()
	ctx, span := d.tracer.Start(ctx, "neo4j.ShortestPath",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.DBSystemNeo4j,
			semconv.DBQueryText(cypher),
			attribute.Int("actor_a", actorA),
			attribute.Int("actor_b", actorB),
		),
	)
	defer func() {
		d.queryDuration.Record(ctx, time.Since(start).Seconds(),
			metric.WithAttributes(attribute.String("query_name", "ShortestPath")))
		span.End()
	}()

	params := map[string]any{"idA": actorA, "idB": actorB}

	session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("error finding shortest path: %w", err)
	}

	record, err := result.Single(ctx)
	if err != nil {
		return nil, nil // no path found
	}

	actorList, _ := record.Get("actors")
	movieList, _ := record.Get("movies")
	actors := actorList.([]any)
	movies := movieList.([]any)

	steps := make([]PathStep, 0, len(actors)+len(movies))
	for i, actor := range actors {
		a := actor.(map[string]any)
		id, _ := a["id"].(int64)
		steps = append(steps, PathStep{Actor: &models.Actor{TmdbID: int(id), Name: a["name"].(string)}})
		if i < len(movies) {
			m := movies[i].(map[string]any)
			year, _ := m["year"].(int64)
			steps = append(steps, PathStep{
				MovieTitle: m["title"].(string),
				MovieYear:  int(year),
			})
		}
	}

	span.SetAttributes(attribute.Int("result.steps", len(steps)))
	return steps, nil
}

// SearchActors runs a fulltext index query against the actor_name index.
func (d *Driver) SearchActors(ctx context.Context, prefix string, limit int) ([]models.Actor, error) {
	cypher := `
		CALL db.index.fulltext.queryNodes("actor_name", $query)
		YIELD node, score
		RETURN node.tmdb_id AS id, node.name AS name
		ORDER BY score DESC
		LIMIT $limit`

	start := time.Now()
	ctx, span := d.tracer.Start(ctx, "neo4j.SearchActors",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.DBSystemNeo4j,
			semconv.DBQueryText(cypher),
			attribute.String("search.prefix", prefix),
		),
	)
	defer func() {
		d.queryDuration.Record(ctx, time.Since(start).Seconds(),
			metric.WithAttributes(attribute.String("query_name", "SearchActors")))
		span.End()
	}()

	params := map[string]any{"query": prefix + "*", "limit": limit}

	session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("error searching actors: %w", err)
	}

	var actors []models.Actor
	for result.Next(ctx) {
		record := result.Record()
		id, _ := record.Get("id")
		name, _ := record.Get("name")
		actors = append(actors, models.Actor{
			TmdbID: int(id.(int64)),
			Name:   name.(string),
		})
	}
	if err = result.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("error iterating actor results: %w", err)
	}

	span.SetAttributes(attribute.Int("result.count", len(actors)))
	return actors, nil
}

func (d *Driver) GetLastIngestedPage(ctx context.Context) (int, error) {
	cypher := "MATCH (s:IngestState) RETURN s.last_page AS page"

	session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		return 0, fmt.Errorf("error reading ingest state: %w", err)
	}

	record, err := result.Single(ctx)
	if err != nil {
		return 0, nil // no IngestState node yet (first run)
	}

	page, _ := record.Get("page")
	return int(page.(int64)), nil
}

func (d *Driver) SetLastIngestedPage(ctx context.Context, page int) error {
	cypher := "MERGE (s:IngestState) SET s.last_page = $page"
	params := map[string]any{"page": page}

	session := d.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, cypher, params)
		if err != nil {
			return nil, fmt.Errorf("error saving ingest state: %w", err)
		}
		return nil, nil
	})
	return err
}

// GetCounts returns actor and edge counts using two fast label/type scans.
// Used by the Prometheus gauge callback so the expensive degree-sort in
// GetStats doesn't run every scrape interval.
func (d *Driver) GetCounts(ctx context.Context) ([2]int, error) {
	cypher := `
		OPTIONAL MATCH (a:Actor)
		WITH count(a) AS actorCount
		OPTIONAL MATCH ()-[r:COSTARRED]->()
		RETURN actorCount, count(r) AS edgeCount`

	session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		return [2]int{}, fmt.Errorf("error getting counts: %w", err)
	}

	record, err := result.Single(ctx)
	if err != nil {
		return [2]int{}, nil
	}

	actorCount, _ := record.Get("actorCount")
	edgeCount, _ := record.Get("edgeCount")
	return [2]int{int(actorCount.(int64)), int(edgeCount.(int64))}, nil
}

// GetStats runs an aggregate Cypher query. Called from HTTP handlers and from
// the async gauge callback on each Prometheus scrape.
func (d *Driver) GetStats(ctx context.Context) (*Stats, error) {
	cypher := `
		OPTIONAL MATCH (a:Actor)
		WITH count(a) AS actorCount
		OPTIONAL MATCH ()-[r:COSTARRED]->()
		WITH actorCount, count(r) AS edgeCount
		OPTIONAL MATCH (a:Actor)-[r:COSTARRED]-()
		WITH actorCount, edgeCount, a, count(r) AS rels
		ORDER BY rels DESC
		LIMIT 1
		RETURN actorCount, edgeCount, a.name AS topActor, rels AS topCount`

	start := time.Now()
	ctx, span := d.tracer.Start(ctx, "neo4j.GetStats",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.DBSystemNeo4j,
			semconv.DBQueryText(cypher),
		),
	)
	defer func() {
		d.queryDuration.Record(ctx, time.Since(start).Seconds(),
			metric.WithAttributes(attribute.String("query_name", "GetStats")))
		span.End()
	}()

	session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("error getting stats: %w", err)
	}

	record, err := result.Single(ctx)
	if err != nil {
		return &Stats{}, nil // empty graph
	}

	actorCount, _ := record.Get("actorCount")
	edgeCount, _ := record.Get("edgeCount")
	topActor, _ := record.Get("topActor")
	topCount, _ := record.Get("topCount")

	stats := &Stats{
		ActorCount: int(actorCount.(int64)),
		EdgeCount:  int(edgeCount.(int64)),
	}
	if topActor != nil {
		stats.MostConnectedActor = topActor.(string)
		stats.MostConnectedCount = int(topCount.(int64))
	}

	return stats, nil
}
