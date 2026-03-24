package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/canal/metricas-financeiro-app/backend/db/migrations"
	"github.com/canal/metricas-financeiro-app/backend/internal/config"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func main() {
	var (
		direction = flag.String("direction", "up", "migration direction: up or down")
		steps     = flag.Int("steps", 0, "number of steps for down migrations; 0 means all")
	)
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	migrator, cleanup, err := newMigrator(cfg.Database.URL)
	if err != nil {
		panic(err)
	}
	defer cleanup()

	switch *direction {
	case "up":
		err = migrator.Up()
	case "down":
		if *steps > 0 {
			err = migrator.Steps(-*steps)
		} else {
			err = migrator.Down()
		}
	default:
		err = fmt.Errorf("unsupported direction %q", *direction)
	}

	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		panic(err)
	}

	_, _ = fmt.Fprintln(os.Stdout, "migrations finished")
}

func newMigrator(databaseURL string) (*migrate.Migrate, func(), error) {
	sourceDriver, err := iofs.New(migrations.Files, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("create migration source: %w", err)
	}

	connConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse database url: %w", err)
	}

	sqlDB := stdlib.OpenDB(*connConfig)
	cleanup := func() {
		_ = sqlDB.Close()
	}

	driver, err := postgres.WithInstance(sqlDB, &postgres.Config{})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("create postgres migration driver: %w", err)
	}

	migrator, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", driver)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("create migrator: %w", err)
	}

	return migrator, cleanup, nil
}
