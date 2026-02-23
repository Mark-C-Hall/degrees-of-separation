//go:build integration

package tmdb

import (
	"context"
	"os"
	"testing"
	"time"

	"net/http"

	"golang.org/x/time/rate"
)

func newTestClient(t *testing.T) *Client {
	t.Helper()
	token := os.Getenv("TMDB_API_TOKEN")
	if token == "" {
		t.Fatal("TMDB_API_TOKEN must be set for integration tests")
	}
	return &Client{
		HTTPClient:  http.Client{Timeout: 10 * time.Second},
		APIURL:      DEFAULT_URL,
		APIToken:    token,
		Limiter:     rate.NewLimiter(rate.Every(time.Second/4), 5),
		MaxRetries:  3,
		BaseBackoff: 1 * time.Second,
	}
}

func TestGetPopularMovies(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()

	totalPages, movies, err := client.GetPopularMovies(ctx, 1)
	if err != nil {
		t.Fatalf("GetPopularMovies returned error: %v", err)
	}

	if len(movies) == 0 {
		t.Fatal("expected at least one movie result, got none")
	}

	if totalPages == 0 {
		t.Fatal("expected TotalPages > 0")
	}

	for _, movie := range movies {
		if movie.TmdbID == 0 {
			t.Error("movie has zero TmdbID")
		}
		if movie.Title == "" {
			t.Error("movie has empty title")
		}
	}
}

func TestGetMovieCast(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()

	// Fight Club (tmdb id 550) — well-known, stable cast list
	cast, err := client.GetMovieCast(ctx, 550, 5)
	if err != nil {
		t.Fatalf("GetMovieCast returned error: %v", err)
	}

	if len(cast) != 5 {
		t.Fatalf("expected 5 cast members, got %d", len(cast))
	}

	for _, member := range cast {
		if member.TmdbID == 0 {
			t.Error("cast member has zero TmdbID")
		}
		if member.Name == "" {
			t.Error("cast member has empty name")
		}
	}
}
