package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/canal/metricas-financeiro-app/backend/internal/auth"
	"github.com/canal/metricas-financeiro-app/backend/internal/cache"
	"github.com/canal/metricas-financeiro-app/backend/internal/commissions"
	"github.com/canal/metricas-financeiro-app/backend/internal/config"
	"github.com/canal/metricas-financeiro-app/backend/internal/db/sqlc"
	"github.com/canal/metricas-financeiro-app/backend/internal/platform/database"
	"github.com/canal/metricas-financeiro-app/backend/internal/platform/logging"
	syncservice "github.com/canal/metricas-financeiro-app/backend/internal/sync"
	"github.com/canal/metricas-financeiro-app/backend/internal/utmify"
	"github.com/canal/metricas-financeiro-app/backend/internal/workers"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Container struct {
	Config            config.Config
	Logger            *slog.Logger
	DB                *pgxpool.Pool
	Queries           *sqlc.Queries
	AuthService       *auth.Service
	CommissionService *commissions.Service
	UTMifyClient      *utmify.Client
	SyncService       *syncservice.Service
	Cache             *cache.Redis
	Scheduler         *workers.Scheduler
}

func Bootstrap(ctx context.Context, cfg config.Config) (*Container, error) {
	logger := logging.New(cfg.App.LogLevel)

	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	queries := sqlc.New(pool)
	authService := auth.NewService(pool, cfg.JWTSecret)
	commissionService := commissions.NewService(logger)
	utmifyClient, err := utmify.NewClient(cfg.UTMify)
	if err != nil {
		return nil, fmt.Errorf("create utmify client: %w", err)
	}
	syncService := syncservice.NewService(pool, utmifyClient, logger, commissionService)

	container := &Container{
		Config:            cfg,
		Logger:            logger,
		DB:                pool,
		Queries:           queries,
		AuthService:       authService,
		CommissionService: commissionService,
		UTMifyClient:      utmifyClient,
		SyncService:       syncService,
	}

	// Initialize Redis cache (optional — app works without it).
	if cfg.Redis.Addr != "" {
		redisCache, err := cache.NewRedis(cfg.Redis)
		if err != nil {
			logger.Warn("redis unavailable, running without cache", slog.String("error", err.Error()))
		} else {
			container.Cache = redisCache
			logger.Info("redis cache connected", slog.String("addr", cfg.Redis.Addr))
		}
	}

	// Start background sync scheduler.
	// DB is the primary data store; Redis is optional for performance caching.
	scheduler := workers.NewScheduler(pool, container.Cache, utmifyClient, syncService, logger)
	scheduler.Start()
	container.Scheduler = scheduler

	return container, nil
}

func (c *Container) Close() {
	if c.Scheduler != nil {
		c.Scheduler.Stop()
	}
	if c.Cache != nil {
		c.Cache.Close()
	}
	if c.DB != nil {
		c.DB.Close()
	}
}
