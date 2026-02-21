package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type ClientConfig struct {
	APIToken    string
	Timeout     time.Duration
	Limit       int
	Burst       int
	MaxRetries  int
	BaseBackoff time.Duration
}

type Config struct {
	Client ClientConfig
}

func Load() (*Config, error) {
	cfg := Config{}

	token, err := getEnvString("TMDB_API_TOKEN")
	if err != nil {
		return nil, fmt.Errorf("Missing Env: %w", err)
	}
	cfg.Client.APIToken = token

	duration, err := getEnvTimeDefault("HTTP_CLIENT_TIMEOUT", "30s")
	if err != nil {
		return nil, fmt.Errorf("invalid timeout: %w", err)
	}
	cfg.Client.Timeout = duration

	limit, err := getEnvIntDefault("TMDB_RATE_LIMIT", "4") // Defaults to 4 reqs/s (40 reqs per 10s)
	if err != nil {
		return nil, fmt.Errorf("invalid rate limit: %w", err)
	}
	cfg.Client.Limit = limit

	burst, err := getEnvIntDefault("TMDB_BURST_AMOUNT", "5")
	if err != nil {
		return nil, fmt.Errorf("invalid burst amount: %w", err)
	}
	cfg.Client.Burst = burst

	maxRetries, err := getEnvIntDefault("TMDB_MAX_RETRIES", "3")
	if err != nil {
		return nil, fmt.Errorf("invalid max retries: %w", err)
	}
	cfg.Client.MaxRetries = maxRetries

	baseBackoff, err := getEnvTimeDefault("TMDB_BASE_BACKOFF", "1s")
	if err != nil {
		return nil, fmt.Errorf("invalid base backoff: %w", err)
	}
	cfg.Client.BaseBackoff = baseBackoff

	return &cfg, nil
}

func getEnvString(key string) (string, error) {
	result := os.Getenv(key)
	if result == "" {
		return "", fmt.Errorf("%s not defined", key)
	}
	return result, nil
}

func getEnvTimeDefault(key, defaultValue string) (time.Duration, error) {
	result := os.Getenv(key)
	if result == "" {
		result = defaultValue
	}

	duration, err := time.ParseDuration(result)
	if err != nil {
		return 0, fmt.Errorf("error parsing duration: %w", err)
	}
	return duration, nil
}

func getEnvIntDefault(key, defaultValue string) (int, error) {
	result := os.Getenv(key)
	if result == "" {
		result = defaultValue
	}
	value, err := strconv.Atoi(result)
	if err != nil {
		return 0, fmt.Errorf("error parsing env: %w", err)
	}
	return value, nil
}
