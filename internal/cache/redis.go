package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/canal/metricas-financeiro-app/backend/internal/config"
	"github.com/redis/go-redis/v9"
)

type Redis struct {
	client *redis.Client
}

func NewRedis(cfg config.RedisConfig) (*Redis, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &Redis{client: client}, nil
}

func (r *Redis) Close() error {
	return r.client.Close()
}

// Set stores a JSON-serializable value with the given TTL.
func (r *Redis) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache marshal: %w", err)
	}
	return r.client.Set(ctx, key, data, ttl).Err()
}

// Get retrieves a cached value and unmarshals it into dest. Returns false if key does not exist.
func (r *Redis) Get(ctx context.Context, key string, dest any) (bool, error) {
	data, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cache get: %w", err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return false, fmt.Errorf("cache unmarshal: %w", err)
	}
	return true, nil
}

// Delete removes a key from the cache.
func (r *Redis) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// DeletePattern removes all keys matching a glob pattern.
func (r *Redis) DeletePattern(ctx context.Context, pattern string) error {
	iter := r.client.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		r.client.Del(ctx, iter.Val())
	}
	return iter.Err()
}

// Expire updates the TTL of an existing key without changing its value.
func (r *Redis) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return r.client.Expire(ctx, key, ttl).Err()
}

// Cache key builders.

func DashboardSummaryKey(date string) string {
	return fmt.Sprintf("dashboard:summary:%s", date)
}

// DashboardItemSummaryKey builds a cache key for a single dashboard's MCP summary.
func DashboardItemSummaryKey(dashboardID string, date string) string {
	return fmt.Sprintf("dashboard:item:%s:%s", dashboardID, date)
}

// CompanySummaryKey builds a cache key for a company-scoped aggregated summary.
func CompanySummaryKey(companyID string, date string) string {
	return fmt.Sprintf("company:summary:%s:%s", companyID, date)
}

func DashboardsListKey() string {
	return "dashboards:list"
}

// AdObjectsKey builds a cache key for ad objects by dashboard and level.
// level: "account", "campaign", "adset", "ad"
func AdObjectsKey(dashboardID string, level string, date string) string {
	return fmt.Sprintf("ads:%s:%s:%s", dashboardID, level, date)
}

// AllAdObjectsKey builds a cache key for all ad objects across all dashboards for a given level.
func AllAdObjectsKey(level string, date string) string {
	return fmt.Sprintf("ads:all:%s:%s", level, date)
}
