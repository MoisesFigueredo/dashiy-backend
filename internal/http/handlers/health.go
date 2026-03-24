package handlers

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HealthHandler struct {
	db *pgxpool.Pool
}

func NewHealthHandler(db *pgxpool.Pool) *HealthHandler {
	return &HealthHandler{db: db}
}

func (h *HealthHandler) Check(c *fiber.Ctx) error {
	status := "ok"
	if err := h.db.Ping(c.Context()); err != nil {
		status = "degraded"
	}

	return c.JSON(fiber.Map{
		"status":    status,
		"service":   "metricas-financeiro-api",
		"timestamp": time.Now().UTC(),
	})
}
