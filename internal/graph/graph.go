package graph

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j"

	"github.com/mark-c-hall/degrees-of-separation/internal/config"
)

type Driver struct {
	driver neo4j.Driver
}

type Actor struct {
	TmdbID int
	Name   string
}

type PathStep struct {
	Actor      *Actor
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

	return &Driver{driver: driver}, nil
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

func (d *Driver) UpsertActor(ctx context.Context, actor Actor) error {
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

func (d *Driver) CreateCostarEdge(ctx context.Context, actorA, actorB, movieID, year int, title string) error {
	cypher := `
		MATCH (a:Actor {tmdb_id: $idA}), (b:Actor {tmdb_id: $idB})
		MERGE (a)-[r:COSTARRED {tmdb_movie_id: $movieID}]->(b)
		SET r.movie_title = $title, r.year = $year`
	params := map[string]any{
		"idA":     actorA,
		"idB":     actorB,
		"movieID": movieID,
		"title":   title,
		"year":    year,
	}

	session := d.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)

	_, err := session.Run(ctx, cypher, params)
	if err != nil {
		return fmt.Errorf("error creating costar edge: %w", err)
	}

	return nil
}

func (d *Driver) ShortestPath(ctx context.Context, actorA, actorB int) ([]PathStep, error) {
	cypher := `
		MATCH (a:Actor {tmdb_id: $idA}), (b:Actor {tmdb_id: $idB}),
		      p = shortestPath((a)-[:COSTARRED*]-(b))
		RETURN [n IN nodes(p) | {id: n.tmdb_id, name: n.name}] AS actors,
		       [r IN relationships(p) | {title: r.movie_title, year: r.year}] AS movies`
	params := map[string]any{"idA": actorA, "idB": actorB}

	session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
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
		steps = append(steps, PathStep{Actor: &Actor{TmdbID: int(id), Name: a["name"].(string)}})
		if i < len(movies) {
			m := movies[i].(map[string]any)
			year, _ := m["year"].(int64)
			steps = append(steps, PathStep{
				MovieTitle: m["title"].(string),
				MovieYear:  int(year),
			})
		}
	}

	return steps, nil
}

func (d *Driver) SearchActors(ctx context.Context, prefix string, limit int) ([]Actor, error) {
	cypher := `
		CALL db.index.fulltext.queryNodes("actor_name", $query)
		YIELD node, score
		RETURN node.tmdb_id AS id, node.name AS name
		ORDER BY score DESC
		LIMIT $limit`
	params := map[string]any{"query": prefix + "*", "limit": limit}

	session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, fmt.Errorf("error searching actors: %w", err)
	}

	var actors []Actor
	for result.Next(ctx) {
		record := result.Record()
		id, _ := record.Get("id")
		name, _ := record.Get("name")
		actors = append(actors, Actor{
			TmdbID: int(id.(int64)),
			Name:   name.(string),
		})
	}
	if err = result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating actor results: %w", err)
	}

	return actors, nil
}

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

	session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
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
