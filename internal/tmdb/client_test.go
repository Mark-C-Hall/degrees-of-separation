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

	resp, err := client.GetPopularMovies(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.TotalPages != 500 {
		t.Errorf("expected TotalPages=500, got %d", resp.TotalPages)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	if resp.Results[0].ID != 123 || resp.Results[0].Title != "Fight Club" {
		t.Errorf("unexpected first result: %+v", resp.Results[0])
	}
	if resp.Results[0].ReleaseDate != "1999-10-15" {
		t.Errorf("expected ReleaseDate=1999-10-15, got %s", resp.Results[0].ReleaseDate)
	}
}

func TestGetPopularMovies_EmptyResults(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"total_pages": 0, "results": []}`)
	})
	client, server := newTestServerClient(handler)
	defer server.Close()

	resp, err := client.GetPopularMovies(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(resp.Results))
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
