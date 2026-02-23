package tmdb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func newTestServerClient(handler http.Handler) (*Client, *httptest.Server) {
	server := httptest.NewServer(handler)
	client := &Client{
		HTTPClient:  http.Client{Timeout: 5 * time.Second},
		APIURL:      server.URL,
		APIToken:    "test-token",
		Limiter:     rate.NewLimiter(rate.Inf, 1),
		MaxRetries:  3,
		BaseBackoff: 10 * time.Millisecond,
	}
	return client, server
}

func TestGetPopularMovies_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"total_pages": 500,
			"results": [
				{"id": 123, "title": "Fight Club", "release_date": "1999-10-15"},
				{"id": 456, "title": "The Matrix", "release_date": "1999-03-31"}
			]
		}`)
	})
	client, server := newTestServerClient(handler)
	defer server.Close()

	totalPages, movies, err := client.GetPopularMovies(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if totalPages != 500 {
		t.Errorf("expected TotalPages=500, got %d", totalPages)
	}
	if len(movies) != 2 {
		t.Fatalf("expected 2 results, got %d", len(movies))
	}
	if movies[0].TmdbID != 123 || movies[0].Title != "Fight Club" {
		t.Errorf("unexpected first result: %+v", movies[0])
	}
	if movies[0].Year != 1999 {
		t.Errorf("expected Year=1999, got %d", movies[0].Year)
	}
}

func TestGetPopularMovies_EmptyResults(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"total_pages": 0, "results": []}`)
	})
	client, server := newTestServerClient(handler)
	defer server.Close()

	_, movies, err := client.GetPopularMovies(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(movies) != 0 {
		t.Errorf("expected 0 results, got %d", len(movies))
	}
}

func TestGetMovieCast_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"cast": [
				{"id": 1, "name": "Brad Pitt"},
				{"id": 2, "name": "Edward Norton"},
				{"id": 3, "name": "Helena Bonham Carter"},
				{"id": 4, "name": "Meat Loaf"},
				{"id": 5, "name": "Jared Leto"}
			]
		}`)
	})
	client, server := newTestServerClient(handler)
	defer server.Close()

	cast, err := client.GetMovieCast(context.Background(), 550, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cast) != 3 {
		t.Fatalf("expected 3 cast members, got %d", len(cast))
	}
	if cast[0].Name != "Brad Pitt" {
		t.Errorf("expected first cast member to be Brad Pitt, got %s", cast[0].Name)
	}
}

func TestParseYear(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"1999-10-15", 1999},
		{"2024-06-01", 2024},
		{"", 0},
	}
	for _, tt := range tests {
		if got := parseYear(tt.input); got != tt.want {
			t.Errorf("parseYear(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestGetHTTP_BearerToken(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %s", auth)
		}
		w.WriteHeader(http.StatusOK)
	})
	client, server := newTestServerClient(handler)
	defer server.Close()

	resp, err := client.getHTTP(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
}

func TestGetHTTP_RetryOn429(t *testing.T) {
	var attempts atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	client, server := newTestServerClient(handler)
	defer server.Close()

	resp, err := client.getHTTP(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestGetHTTP_ExhaustedRetries(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	client, server := newTestServerClient(handler)
	defer server.Close()

	_, err := client.getHTTP(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected error after exhausted retries, got nil")
	}
}

func TestGetHTTP_ContextCancelled(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	client, server := newTestServerClient(handler)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.getHTTP(ctx, server.URL)
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}
