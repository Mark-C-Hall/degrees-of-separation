//go:build integration

package graph

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
	tcneo4j "github.com/testcontainers/testcontainers-go/modules/neo4j"

	"github.com/mark-c-hall/degrees-of-separation/internal/config"
)

var testDriver *Driver

func setupContainer(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	container, err := tcneo4j.Run(ctx, "neo4j:5", tcneo4j.WithoutAuthentication())
	if err != nil {
		t.Fatalf("failed to start neo4j container: %v", err)
	}
	t.Cleanup(func() { container.Terminate(context.Background()) })

	boltURL, err := container.BoltUrl(ctx)
	if err != nil {
		t.Fatalf("failed to get bolt url: %v", err)
	}

	cfg := config.Config{
		DB: config.DBConfig{
			URI:  boltURL,
			User: "neo4j",
			Pass: "",
		},
	}

	// Neo4j may need a moment to be ready for auth-free connections
	var d *Driver
	for range 10 {
		d, err = NewDriver(ctx, cfg)
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}
	t.Cleanup(func() { d.Close(context.Background()) })

	if err = d.SetupSchema(ctx); err != nil {
		t.Fatalf("failed to setup schema: %v", err)
	}

	// Wait for the fulltext index to come online
	time.Sleep(2 * time.Second)

	testDriver = d
}

func clearGraph(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	session := testDriver.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	if _, err := session.Run(ctx, "MATCH (n) DETACH DELETE n", nil); err != nil {
		t.Fatalf("failed to clear graph: %v", err)
	}
}

func TestSetupSchema_Idempotent(t *testing.T) {
	setupContainer(t)
	ctx := context.Background()

	// Running schema setup a second time should not error
	if err := testDriver.SetupSchema(ctx); err != nil {
		t.Fatalf("second SetupSchema call failed: %v", err)
	}
}

func TestUpsertActor(t *testing.T) {
	setupContainer(t)
	clearGraph(t)
	ctx := context.Background()

	actor := Actor{TmdbID: 1, Name: "Brad Pitt"}
	if err := testDriver.UpsertActor(ctx, actor); err != nil {
		t.Fatalf("UpsertActor failed: %v", err)
	}

	// Upsert same actor with updated name
	actor.Name = "William Bradley Pitt"
	if err := testDriver.UpsertActor(ctx, actor); err != nil {
		t.Fatalf("UpsertActor update failed: %v", err)
	}

	// Verify only one node exists and name was updated
	session := testDriver.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, "MATCH (a:Actor {tmdb_id: $id}) RETURN a.name AS name, count(a) AS c", map[string]any{"id": 1})
	if err != nil {
		t.Fatalf("verification query failed: %v", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		t.Fatalf("expected exactly one record: %v", err)
	}
	name, _ := record.Get("name")
	count, _ := record.Get("c")
	if name.(string) != "William Bradley Pitt" {
		t.Errorf("expected updated name, got %s", name)
	}
	if count.(int64) != 1 {
		t.Errorf("expected 1 actor node, got %d", count)
	}
}

func TestCreateCostarEdge(t *testing.T) {
	setupContainer(t)
	clearGraph(t)
	ctx := context.Background()

	// Create two actors
	testDriver.UpsertActor(ctx, Actor{TmdbID: 1, Name: "Brad Pitt"})
	testDriver.UpsertActor(ctx, Actor{TmdbID: 2, Name: "Edward Norton"})

	if err := testDriver.CreateCostarEdge(ctx, 1, 2, 550, 1999, "Fight Club"); err != nil {
		t.Fatalf("CreateCostarEdge failed: %v", err)
	}

	// Creating the same edge again should be idempotent
	if err := testDriver.CreateCostarEdge(ctx, 1, 2, 550, 1999, "Fight Club"); err != nil {
		t.Fatalf("idempotent CreateCostarEdge failed: %v", err)
	}

	// Verify only one edge exists
	session := testDriver.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, "MATCH ()-[r:COSTARRED]->() RETURN count(r) AS c, r.movie_title AS title, r.year AS year", nil)
	if err != nil {
		t.Fatalf("verification query failed: %v", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		t.Fatalf("expected exactly one record: %v", err)
	}
	count, _ := record.Get("c")
	title, _ := record.Get("title")
	year, _ := record.Get("year")
	if count.(int64) != 1 {
		t.Errorf("expected 1 edge, got %d", count)
	}
	if title.(string) != "Fight Club" {
		t.Errorf("expected Fight Club, got %s", title)
	}
	if year.(int64) != 1999 {
		t.Errorf("expected 1999, got %d", year)
	}
}

func TestShortestPath(t *testing.T) {
	setupContainer(t)
	clearGraph(t)
	ctx := context.Background()

	// Build a chain: A --movie1-- B --movie2-- C
	testDriver.UpsertActor(ctx, Actor{TmdbID: 1, Name: "Actor A"})
	testDriver.UpsertActor(ctx, Actor{TmdbID: 2, Name: "Actor B"})
	testDriver.UpsertActor(ctx, Actor{TmdbID: 3, Name: "Actor C"})
	testDriver.CreateCostarEdge(ctx, 1, 2, 100, 2000, "Movie One")
	testDriver.CreateCostarEdge(ctx, 2, 3, 200, 2010, "Movie Two")

	steps, err := testDriver.ShortestPath(ctx, 1, 3)
	if err != nil {
		t.Fatalf("ShortestPath failed: %v", err)
	}

	// Expect: ActorA, MovieOne, ActorB, MovieTwo, ActorC
	if len(steps) != 5 {
		t.Fatalf("expected 5 steps, got %d: %+v", len(steps), steps)
	}

	if steps[0].Actor == nil || steps[0].Actor.Name != "Actor A" {
		t.Errorf("step 0: expected Actor A, got %+v", steps[0])
	}
	if steps[1].MovieTitle != "Movie One" || steps[1].MovieYear != 2000 {
		t.Errorf("step 1: expected Movie One (2000), got %+v", steps[1])
	}
	if steps[2].Actor == nil || steps[2].Actor.Name != "Actor B" {
		t.Errorf("step 2: expected Actor B, got %+v", steps[2])
	}
	if steps[3].MovieTitle != "Movie Two" || steps[3].MovieYear != 2010 {
		t.Errorf("step 3: expected Movie Two (2010), got %+v", steps[3])
	}
	if steps[4].Actor == nil || steps[4].Actor.Name != "Actor C" {
		t.Errorf("step 4: expected Actor C, got %+v", steps[4])
	}
}

func TestShortestPath_NoPath(t *testing.T) {
	setupContainer(t)
	clearGraph(t)
	ctx := context.Background()

	// Two disconnected actors
	testDriver.UpsertActor(ctx, Actor{TmdbID: 1, Name: "Actor A"})
	testDriver.UpsertActor(ctx, Actor{TmdbID: 2, Name: "Actor B"})

	steps, err := testDriver.ShortestPath(ctx, 1, 2)
	if err != nil {
		t.Fatalf("ShortestPath failed: %v", err)
	}
	if steps != nil {
		t.Errorf("expected nil steps for disconnected actors, got %+v", steps)
	}
}

func TestSearchActors(t *testing.T) {
	setupContainer(t)
	clearGraph(t)
	ctx := context.Background()

	testDriver.UpsertActor(ctx, Actor{TmdbID: 1, Name: "Leonardo DiCaprio"})
	testDriver.UpsertActor(ctx, Actor{TmdbID: 2, Name: "Leon Kennedy"})
	testDriver.UpsertActor(ctx, Actor{TmdbID: 3, Name: "Brad Pitt"})

	// Fulltext index needs a moment to index new data
	time.Sleep(2 * time.Second)

	actors, err := testDriver.SearchActors(ctx, "Leo", 10)
	if err != nil {
		t.Fatalf("SearchActors failed: %v", err)
	}

	if len(actors) != 2 {
		t.Fatalf("expected 2 results for 'Leo', got %d: %+v", len(actors), actors)
	}

	names := map[string]bool{}
	for _, a := range actors {
		names[a.Name] = true
	}
	if !names["Leonardo DiCaprio"] || !names["Leon Kennedy"] {
		t.Errorf("expected Leonardo DiCaprio and Leon Kennedy, got %+v", actors)
	}
}

func TestSearchActors_Limit(t *testing.T) {
	setupContainer(t)
	clearGraph(t)
	ctx := context.Background()

	for i := range 5 {
		testDriver.UpsertActor(ctx, Actor{TmdbID: i + 1, Name: fmt.Sprintf("Test Actor %d", i+1)})
	}

	time.Sleep(2 * time.Second)

	actors, err := testDriver.SearchActors(ctx, "Test", 3)
	if err != nil {
		t.Fatalf("SearchActors failed: %v", err)
	}
	if len(actors) != 3 {
		t.Errorf("expected 3 results with limit, got %d", len(actors))
	}
}

func TestGetStats(t *testing.T) {
	setupContainer(t)
	clearGraph(t)
	ctx := context.Background()

	// Empty graph
	stats, err := testDriver.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats on empty graph failed: %v", err)
	}
	if stats.ActorCount != 0 {
		t.Errorf("expected 0 actors on empty graph, got %d", stats.ActorCount)
	}

	// Add data: A connected to B and C, B connected to C
	testDriver.UpsertActor(ctx, Actor{TmdbID: 1, Name: "Actor A"})
	testDriver.UpsertActor(ctx, Actor{TmdbID: 2, Name: "Actor B"})
	testDriver.UpsertActor(ctx, Actor{TmdbID: 3, Name: "Actor C"})
	testDriver.CreateCostarEdge(ctx, 1, 2, 100, 2000, "Movie One")
	testDriver.CreateCostarEdge(ctx, 1, 3, 200, 2005, "Movie Two")
	testDriver.CreateCostarEdge(ctx, 2, 3, 300, 2010, "Movie Three")

	stats, err = testDriver.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if stats.ActorCount != 3 {
		t.Errorf("expected 3 actors, got %d", stats.ActorCount)
	}
	if stats.EdgeCount != 3 {
		t.Errorf("expected 3 edges, got %d", stats.EdgeCount)
	}
	// Actor A has edges to both B and C (directed + undirected counted)
	// so A should be most connected
	if stats.MostConnectedActor != "Actor A" {
		t.Errorf("expected most connected to be Actor A, got %s", stats.MostConnectedActor)
	}
}

func TestVerifyConnectivity(t *testing.T) {
	setupContainer(t)
	ctx := context.Background()

	if err := testDriver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("VerifyConnectivity failed: %v", err)
	}
}
