package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/canal/metricas-financeiro-app/backend/internal/auth"
	"github.com/canal/metricas-financeiro-app/backend/internal/config"
	"github.com/jackc/pgx/v5"
)

func main() {
	var (
		email    = flag.String("email", "", "system user email")
		password = flag.String("password", "", "system user password (min 8 chars)")
	)
	flag.Parse()

	if *email == "" || *password == "" {
		_, _ = fmt.Fprintln(os.Stderr, "usage: create-system-user -email <email> -password <password>")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	passwordHash, err := auth.HashPassword(*password)
	if err != nil {
		panic(err)
	}

	totpSecret, err := auth.GenerateTOTPSecret()
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, cfg.Database.URL)
	if err != nil {
		panic(err)
	}
	defer conn.Close(ctx)

	var id string
	err = conn.QueryRow(ctx, `
		INSERT INTO system_users (email, password_hash, totp_secret)
		VALUES (lower($1), $2, $3)
		ON CONFLICT (email) DO UPDATE
		SET password_hash = EXCLUDED.password_hash, active = true, updated_at = now()
		RETURNING id
	`, *email, passwordHash, totpSecret).Scan(&id)
	if err != nil {
		panic(err)
	}

	_, _ = fmt.Fprintf(os.Stdout, "system user ready: id=%s email=%s\n", id, *email)
}
