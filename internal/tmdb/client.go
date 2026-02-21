package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"github.com/mark-c-hall/degrees-of-separation/internal/config"
)

const (
	DEFAULT_URL = "https://api.themoviedb.org"
	API_VERSION = "3"
)

type Client struct {
	HTTPClient  http.Client
	APIURL      string
	APIToken    string
	Limiter     *rate.Limiter
	MaxRetries  int
	BaseBackoff time.Duration
}

type MovieResults struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type PopularResponse struct {
	TotalPages int            `json:"total_pages"`
	Results    []MovieResults `json:"results"`
}

type CreditsResponse struct {
	Cast []CastResults `json:"cast"`
}

type CastResults struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func NewClient(cfg config.Config) *Client {
	client := Client{
		HTTPClient:  http.Client{Timeout: cfg.Client.Timeout},
		APIURL:      DEFAULT_URL,
		APIToken:    cfg.Client.APIToken,
		Limiter:     rate.NewLimiter(rate.Every(time.Second/time.Duration(cfg.Client.Limit)), cfg.Client.Burst),
		MaxRetries:  cfg.Client.MaxRetries,
		BaseBackoff: cfg.Client.BaseBackoff,
	}
	return &client
}

func (c *Client) GetPopularMovies(ctx context.Context, page int) (*PopularResponse, error) {
	url := fmt.Sprintf("%s/%s/movie/popular?page=%d", c.APIURL, API_VERSION, page)
	resp, err := c.getHTTP(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("error getting popular movies: %w", err)
	}
	defer resp.Body.Close()

	var APIResponse PopularResponse
	if err = json.NewDecoder(resp.Body).Decode(&APIResponse); err != nil {
		return nil, fmt.Errorf("error decoding popular movie response: %w", err)
	}

	return &APIResponse, nil
}

func (c *Client) GetMovieCast(ctx context.Context, movieID, maxCast int) ([]CastResults, error) {
	url := fmt.Sprintf("%s/%s/movie/%d/credits", c.APIURL, API_VERSION, movieID)
	resp, err := c.getHTTP(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("error getting movie's cast: %w", err)
	}
	defer resp.Body.Close()

	var APIResponse CreditsResponse
	if err = json.NewDecoder(resp.Body).Decode(&APIResponse); err != nil {
		return nil, fmt.Errorf("error decoding movie cast response: %w", err)
	}

	if maxCast > len(APIResponse.Cast) {
		maxCast = len(APIResponse.Cast)
	}
	return APIResponse.Cast[:maxCast], nil
}

func (c *Client) getHTTP(ctx context.Context, url string) (*http.Response, error) {
	for attempt := range c.MaxRetries {
		if err := c.Limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter wait: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("error creating http request: %w", err)
		}

		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.APIToken))
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error making http request: %w", err)
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}
		resp.Body.Close()

		backoff := c.BaseBackoff << attempt
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	return nil, fmt.Errorf("exceeded %d retries due to rate limiting", c.MaxRetries)
}
