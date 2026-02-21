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
		APIURL:      defaultAPIURL,
		APIToken:    token,
		Limiter:     rate.NewLimiter(rate.Every(time.Second/4), 5),
		MaxRetries:  3,
		BaseBackoff: 1 * time.Second,
	}
}

func TestGetPopularMovies(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()

	resp, err := client.GetPopularMovies(ctx, 1)
	if err != nil {
		t.Fatalf("GetPopularMovies returned error: %v", err)
	}

	if len(resp.Results) == 0 {
		t.Fatal("expected at least one movie result, got none")
	}

	if resp.TotalPages == 0 {
		t.Fatal("expected TotalPages > 0")
	}

	for _, movie := range resp.Results {
		if movie.ID == 0 {
			t.Error("movie has zero ID")
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
		if member.ID == 0 {
			t.Error("cast member has zero ID")
		}
		if member.Name == "" {
			t.Error("cast member has empty name")
		}
	}
}
