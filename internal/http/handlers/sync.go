package handlers

import (
	"github.com/canal/metricas-financeiro-app/backend/internal/http/middleware"
	syncservice "github.com/canal/metricas-financeiro-app/backend/internal/sync"
	"github.com/gofiber/fiber/v2"
)

type SyncHandler struct {
	service *syncservice.Service
}

func NewSyncHandler(service *syncservice.Service) *SyncHandler {
	return &SyncHandler{service: service}
}

func (h *SyncHandler) SyncToday(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	summary, err := h.service.SyncToday(c.Context(), scope.CompanyID)
	if err != nil {
		return err
	}

	return c.JSON(summary)
}
