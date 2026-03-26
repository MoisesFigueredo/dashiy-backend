package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type AppConfig struct {
	Name        string
	Env         string
	Port        string
	LogLevel    string
	CorsOrigins string
}

type DatabaseConfig struct {
	URL               string
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
	ConnectionTimeout time.Duration
	StatementCacheCap int32
	Description       string
}

type RedisConfig struct {
	URL      string
	Addr     string
	Password string
	DB       int
}

type AsynqConfig struct {
	Concurrency    int
	CriticalWeight int
	DefaultWeight  int
	LowWeight      int
}

type UTMifyConfig struct {
	MCPURL  string
	Timeout time.Duration
}

type Config struct {
	App       AppConfig
	Database  DatabaseConfig
	Redis     RedisConfig
	Asynq     AsynqConfig
	UTMify    UTMifyConfig
	JWTSecret string
}

func Load() (Config, error) {
	loadDotEnv()
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = os.Getenv("REDIS_PUBLIC_URL")
	}

	cfg := Config{
		App: AppConfig{
			Name:        getEnv("APP_NAME", "metricas-financeiro-api"),
			Env:         getEnv("APP_ENV", "development"),
			Port:        getEnvFirstOf([]string{"APP_PORT", "PORT"}, "8080"),
			LogLevel:    getEnv("LOG_LEVEL", "info"),
			CorsOrigins: getEnv("CORS_ORIGINS", "http://localhost:8080,http://127.0.0.1:8080,http://localhost:5173,http://127.0.0.1:5173"),
		},
		Database: DatabaseConfig{
			URL:               os.Getenv("DATABASE_URL"),
			MaxConns:          getEnvInt32("DATABASE_MAX_CONNS", 10),
			MinConns:          getEnvInt32("DATABASE_MIN_CONNS", 2),
			MaxConnLifetime:   getEnvDuration("DATABASE_MAX_CONN_LIFETIME", 30*time.Minute),
			MaxConnIdleTime:   getEnvDuration("DATABASE_MAX_CONN_IDLE_TIME", 15*time.Minute),
			HealthCheckPeriod: getEnvDuration("DATABASE_HEALTHCHECK_PERIOD", 1*time.Minute),
			ConnectionTimeout: getEnvDuration("DATABASE_CONNECTION_TIMEOUT", 10*time.Second),
			StatementCacheCap: getEnvInt32("DATABASE_STATEMENT_CACHE_CAP", 0),
			Description:       getEnv("DATABASE_DESCRIPTION", "primary"),
		},
		Redis: RedisConfig{
			URL:      redisURL,
			Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       getEnvInt("REDIS_DB", 0),
		},
		Asynq: AsynqConfig{
			Concurrency:    getEnvInt("ASYNQ_CONCURRENCY", 10),
			CriticalWeight: getEnvInt("ASYNQ_CRITICAL_WEIGHT", 6),
			DefaultWeight:  getEnvInt("ASYNQ_DEFAULT_WEIGHT", 3),
			LowWeight:      getEnvInt("ASYNQ_LOW_WEIGHT", 1),
		},
		UTMify: UTMifyConfig{
			MCPURL:  strings.TrimSpace(os.Getenv("UTMIFY_MCP_URL")),
			Timeout: getEnvDuration("UTMIFY_TIMEOUT", 30*time.Second),
		},
		JWTSecret: getEnv("JWT_SECRET", "change-me"),
	}

	if cfg.Database.URL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	if cfg.Redis.URL != "" {
		redisCfg, err := parseRedisURL(cfg.Redis.URL)
		if err != nil {
			return Config{}, err
		}
		cfg.Redis = redisCfg
	}

	return cfg, nil
}

func loadDotEnv() {
	// Best-effort load so local commands work without manually exporting vars.
	_ = godotenv.Load(".env", ".env.local", filepath.Join("..", "dashiy-front", ".env"))
}

func getEnvFirstOf(keys []string, fallback string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return fallback
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	return value
}

func getEnvInt32(key string, fallback int32) int32 {
	return int32(getEnvInt(key, int(fallback)))
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}

	return value
}

func parseRedisURL(raw string) (RedisConfig, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return RedisConfig{}, fmt.Errorf("parse REDIS_URL: %w", err)
	}

	if parsed.Scheme != "redis" && parsed.Scheme != "rediss" {
		return RedisConfig{}, fmt.Errorf("REDIS_URL must start with redis:// or rediss://")
	}

	password, _ := parsed.User.Password()
	db := 0
	path := strings.TrimPrefix(strings.TrimSpace(parsed.Path), "/")
	if path != "" {
		db, err = strconv.Atoi(path)
		if err != nil {
			return RedisConfig{}, fmt.Errorf("parse REDIS_URL database index: %w", err)
		}
	}

	return RedisConfig{
		URL:      raw,
		Addr:     parsed.Host,
		Password: password,
		DB:       db,
	}, nil
}
