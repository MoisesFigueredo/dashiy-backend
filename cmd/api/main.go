package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	bootstrap "github.com/canal/metricas-financeiro-app/backend/internal/app"
	"github.com/canal/metricas-financeiro-app/backend/internal/config"
	apihttp "github.com/canal/metricas-financeiro-app/backend/internal/http"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	container, err := bootstrap.Bootstrap(ctx, cfg)
	if err != nil {
		panic(err)
	}
	defer container.Close()

	app := apihttp.NewRouter(apihttp.Dependencies{
		Logger:         container.Logger,
		DB:             container.DB,
		AuthService:    container.AuthService,
		AllowedOrigins: container.Config.App.CorsOrigins,
		SyncService:    container.SyncService,
		UTMifyClient:   container.UTMifyClient,
		Cache:          container.Cache,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Listen(":" + cfg.App.Port)
	}()

	select {
	case <-ctx.Done():
		_ = app.Shutdown()
	case err := <-errCh:
		if err != nil {
			panic(fmt.Errorf("api server failed: %w", err))
		}
	}
}
