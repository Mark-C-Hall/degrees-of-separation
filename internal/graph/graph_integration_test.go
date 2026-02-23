//go:build integration

package graph

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
	tcneo4j "github.com/testcontainers/testcontainers-go/modules/neo4j"

	"github.com/mark-c-hall/degrees-of-separation/internal/config"
	"github.com/mark-c-hall/degrees-of-separation/internal/models"
)

var testDriver *Driver

func TestMain(m *testing.M) {
	ctx := context.Background()

	container, err := tcneo4j.Run(ctx, "neo4j:5", tcneo4j.WithoutAuthentication())
	if err != nil {
		log.Fatalf("failed to start neo4j container: %v", err)
	}
	defer container.Terminate(ctx)

	boltURL, err := container.BoltUrl(ctx)
	if err != nil {
		log.Fatalf("failed to get bolt url: %v", err)
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
		log.Fatalf("failed to create driver: %v", err)
	}
	defer d.Close(ctx)

	if err = d.SetupSchema(ctx); err != nil {
		log.Fatalf("failed to setup schema: %v", err)
	}

	// Wait for the fulltext index to come online
	time.Sleep(2 * time.Second)

	testDriver = d
	os.Exit(m.Run())
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
	ctx := context.Background()

	// Running schema setup a second time should not error
	if err := testDriver.SetupSchema(ctx); err != nil {
		t.Fatalf("second SetupSchema call failed: %v", err)
	}
}

func TestUpsertActor(t *testing.T) {
	clearGraph(t)
	ctx := context.Background()

	actor := models.Actor{TmdbID: 1, Name: "Brad Pitt"}
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
	clearGraph(t)
	ctx := context.Background()

	// Create two actors
	testDriver.UpsertActor(ctx, models.Actor{TmdbID: 1, Name: "Brad Pitt"})
	testDriver.UpsertActor(ctx, models.Actor{TmdbID: 2, Name: "Edward Norton"})

	fightClub := models.Movie{TmdbID: 550, Title: "Fight Club", Year: 1999}
	if err := testDriver.CreateCostarEdge(ctx, 1, 2, fightClub); err != nil {
		t.Fatalf("CreateCostarEdge failed: %v", err)
	}

	// Creating the same edge again should be idempotent
	if err := testDriver.CreateCostarEdge(ctx, 1, 2, fightClub); err != nil {
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
	clearGraph(t)
	ctx := context.Background()

	// Build a chain: A --movie1-- B --movie2-- C
	testDriver.UpsertActor(ctx, models.Actor{TmdbID: 1, Name: "Actor A"})
	testDriver.UpsertActor(ctx, models.Actor{TmdbID: 2, Name: "Actor B"})
	testDriver.UpsertActor(ctx, models.Actor{TmdbID: 3, Name: "Actor C"})
	testDriver.CreateCostarEdge(ctx, 1, 2, models.Movie{TmdbID: 100, Title: "Movie One", Year: 2000})
	testDriver.CreateCostarEdge(ctx, 2, 3, models.Movie{TmdbID: 200, Title: "Movie Two", Year: 2010})

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
	clearGraph(t)
	ctx := context.Background()

	// Two disconnected actors
	testDriver.UpsertActor(ctx, models.Actor{TmdbID: 1, Name: "Actor A"})
	testDriver.UpsertActor(ctx, models.Actor{TmdbID: 2, Name: "Actor B"})

	steps, err := testDriver.ShortestPath(ctx, 1, 2)
	if err != nil {
		t.Fatalf("ShortestPath failed: %v", err)
	}
	if steps != nil {
		t.Errorf("expected nil steps for disconnected actors, got %+v", steps)
	}
}

func TestSearchActors(t *testing.T) {
	clearGraph(t)
	ctx := context.Background()

	testDriver.UpsertActor(ctx, models.Actor{TmdbID: 1, Name: "Leonardo DiCaprio"})
	testDriver.UpsertActor(ctx, models.Actor{TmdbID: 2, Name: "Leon Kennedy"})
	testDriver.UpsertActor(ctx, models.Actor{TmdbID: 3, Name: "Brad Pitt"})

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
	clearGraph(t)
	ctx := context.Background()

	for i := range 5 {
		testDriver.UpsertActor(ctx, models.Actor{TmdbID: i + 1, Name: fmt.Sprintf("Test Actor %d", i+1)})
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
	testDriver.UpsertActor(ctx, models.Actor{TmdbID: 1, Name: "Actor A"})
	testDriver.UpsertActor(ctx, models.Actor{TmdbID: 2, Name: "Actor B"})
	testDriver.UpsertActor(ctx, models.Actor{TmdbID: 3, Name: "Actor C"})
	testDriver.CreateCostarEdge(ctx, 1, 2, models.Movie{TmdbID: 100, Title: "Movie One", Year: 2000})
	testDriver.CreateCostarEdge(ctx, 1, 3, models.Movie{TmdbID: 200, Title: "Movie Two", Year: 2005})
	testDriver.CreateCostarEdge(ctx, 2, 3, models.Movie{TmdbID: 300, Title: "Movie Three", Year: 2010})

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

func TestGetLastIngestedPage_EmptyGraph(t *testing.T) {
	clearGraph(t)
	ctx := context.Background()

	page, err := testDriver.GetLastIngestedPage(ctx)
	if err != nil {
		t.Fatalf("GetLastIngestedPage failed: %v", err)
	}
	if page != 0 {
		t.Errorf("expected 0 on empty graph, got %d", page)
	}
}

func TestSetAndGetLastIngestedPage(t *testing.T) {
	clearGraph(t)
	ctx := context.Background()

	if err := testDriver.SetLastIngestedPage(ctx, 42); err != nil {
		t.Fatalf("SetLastIngestedPage failed: %v", err)
	}

	page, err := testDriver.GetLastIngestedPage(ctx)
	if err != nil {
		t.Fatalf("GetLastIngestedPage failed: %v", err)
	}
	if page != 42 {
		t.Errorf("expected 42, got %d", page)
	}
}

func TestSetLastIngestedPage_Overwrites(t *testing.T) {
	clearGraph(t)
	ctx := context.Background()

	testDriver.SetLastIngestedPage(ctx, 10)
	testDriver.SetLastIngestedPage(ctx, 25)

	page, err := testDriver.GetLastIngestedPage(ctx)
	if err != nil {
		t.Fatalf("GetLastIngestedPage failed: %v", err)
	}
	if page != 25 {
		t.Errorf("expected 25 after overwrite, got %d", page)
	}
}

func TestIngestMovieCast(t *testing.T) {
	clearGraph(t)
	ctx := context.Background()

	movie := models.Movie{TmdbID: 550, Title: "Fight Club", Year: 1999}
	cast := []models.Actor{
		{TmdbID: 1, Name: "Brad Pitt"},
		{TmdbID: 2, Name: "Edward Norton"},
		{TmdbID: 3, Name: "Helena Bonham Carter"},
	}

	if err := testDriver.IngestMovieCast(ctx, movie, cast); err != nil {
		t.Fatalf("IngestMovieCast failed: %v", err)
	}

	session := testDriver.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	// Verify 3 actor nodes
	result, err := session.Run(ctx, "MATCH (a:Actor) RETURN count(a) AS c", nil)
	if err != nil {
		t.Fatalf("actor count query failed: %v", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		t.Fatalf("expected one record: %v", err)
	}
	actorCount, _ := record.Get("c")
	if actorCount.(int64) != 3 {
		t.Errorf("expected 3 actors, got %d", actorCount)
	}

	// Verify 3 edges (pairs: 1-2, 1-3, 2-3)
	result, err = session.Run(ctx, "MATCH ()-[r:COSTARRED]->() RETURN count(r) AS c", nil)
	if err != nil {
		t.Fatalf("edge count query failed: %v", err)
	}
	record, err = result.Single(ctx)
	if err != nil {
		t.Fatalf("expected one record: %v", err)
	}
	edgeCount, _ := record.Get("c")
	if edgeCount.(int64) != 3 {
		t.Errorf("expected 3 edges, got %d", edgeCount)
	}

	// Idempotency: calling again should not create duplicate nodes or edges
	if err := testDriver.IngestMovieCast(ctx, movie, cast); err != nil {
		t.Fatalf("second IngestMovieCast failed: %v", err)
	}

	result, err = session.Run(ctx, "MATCH (a:Actor) RETURN count(a) AS c", nil)
	if err != nil {
		t.Fatalf("actor count query after re-ingest failed: %v", err)
	}
	record, err = result.Single(ctx)
	if err != nil {
		t.Fatalf("expected one record: %v", err)
	}
	actorCount, _ = record.Get("c")
	if actorCount.(int64) != 3 {
		t.Errorf("expected 3 actors after re-ingest, got %d", actorCount)
	}

	result, err = session.Run(ctx, "MATCH ()-[r:COSTARRED]->() RETURN count(r) AS c", nil)
	if err != nil {
		t.Fatalf("edge count query after re-ingest failed: %v", err)
	}
	record, err = result.Single(ctx)
	if err != nil {
		t.Fatalf("expected one record: %v", err)
	}
	edgeCount, _ = record.Get("c")
	if edgeCount.(int64) != 3 {
		t.Errorf("expected 3 edges after re-ingest, got %d", edgeCount)
	}
}

func TestVerifyConnectivity(t *testing.T) {
	ctx := context.Background()

	if err := testDriver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("VerifyConnectivity failed: %v", err)
	}
}
