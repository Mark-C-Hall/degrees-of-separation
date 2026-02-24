package config

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
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

type DBConfig struct {
	URI  string
	User string
	Pass string
}

type ServerConfig struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	RequestTimeout  time.Duration
	CORSOrigin      string
	RateLimitPerSec float64
	RateBurst       int
}

type Config struct {
	Client ClientConfig
	DB     DBConfig
	Server ServerConfig
}

func Load() (*Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		log.Printf("warning: could not load .env: %v", err)
	}

	cfg := Config{}

	cfg.Client.APIToken = os.Getenv("TMDB_API_TOKEN")

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

	uri, err := getEnvString("NEO4J_URI")
	if err != nil {
		return nil, fmt.Errorf("missing env: %w", err)
	}
	cfg.DB.URI = uri

	user, err := getEnvString("NEO4J_USER")
	if err != nil {
		return nil, fmt.Errorf("missing env: %w", err)
	}
	cfg.DB.User = user

	pass, err := getEnvString("NEO4J_PASSWORD")
	if err != nil {
		return nil, fmt.Errorf("missing env: %w", err)
	}
	cfg.DB.Pass = pass

	port, err := getEnvStringDefault("PORT", "8080")
	if err != nil {
		return nil, fmt.Errorf("invalid port: %w", err)
	}
	cfg.Server.Addr = ":" + port

	readTimeout, err := getEnvTimeDefault("SERVER_READ_TIMEOUT", "5s")
	if err != nil {
		return nil, fmt.Errorf("invalid read timeout: %w", err)
	}
	cfg.Server.ReadTimeout = readTimeout

	writeTimeout, err := getEnvTimeDefault("SERVER_WRITE_TIMEOUT", "10s")
	if err != nil {
		return nil, fmt.Errorf("invalid write timeout: %w", err)
	}
	cfg.Server.WriteTimeout = writeTimeout

	idleTimeout, err := getEnvTimeDefault("SERVER_IDLE_TIMEOUT", "120s")
	if err != nil {
		return nil, fmt.Errorf("invalid idle timeout: %w", err)
	}
	cfg.Server.IdleTimeout = idleTimeout

	shutdownTimeout, err := getEnvTimeDefault("SERVER_SHUTDOWN_TIMEOUT", "10s")
	if err != nil {
		return nil, fmt.Errorf("invalid shutdown timeout: %w", err)
	}
	cfg.Server.ShutdownTimeout = shutdownTimeout

	requestTimeout, err := getEnvTimeDefault("REQUEST_TIMEOUT", "10s")
	if err != nil {
		return nil, fmt.Errorf("invalid request timeout: %w", err)
	}
	cfg.Server.RequestTimeout = requestTimeout

	corsOrigin, err := getEnvStringDefault("CORS_ALLOWED_ORIGIN", "*")
	if err != nil {
		return nil, fmt.Errorf("invalid cors origin: %w", err)
	}
	cfg.Server.CORSOrigin = corsOrigin

	rateLimitPerSec, err := getEnvFloatDefault("RATE_LIMIT_PER_SEC", "0.5")
	if err != nil {
		return nil, fmt.Errorf("invalid rate limit: %w", err)
	}
	cfg.Server.RateLimitPerSec = rateLimitPerSec

	rateBurst, err := getEnvIntDefault("RATE_BURST", "5")
	if err != nil {
		return nil, fmt.Errorf("invalid rate burst: %w", err)
	}
	cfg.Server.RateBurst = rateBurst

	return &cfg, nil
}

// loadDotEnv reads a .env file and sets any variable not already present in
// the environment. It silently does nothing if the file doesn't exist.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}

func getEnvString(key string) (string, error) {
	result := os.Getenv(key)
	if result == "" {
		return "", fmt.Errorf("%s not defined", key)
	}
	return result, nil
}

func getEnvStringDefault(key, defaultValue string) (string, error) {
	result := os.Getenv(key)
	if result == "" {
		result = defaultValue
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

func getEnvFloatDefault(key, defaultValue string) (float64, error) {
	result := os.Getenv(key)
	if result == "" {
		result = defaultValue
	}
	value, err := strconv.ParseFloat(result, 64)
	if err != nil {
		return 0, fmt.Errorf("error parsing env: %w", err)
	}
	return value, nil
}
