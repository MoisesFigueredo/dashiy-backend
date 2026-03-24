package handlers

import (
	"errors"
	"fmt"

	"github.com/canal/metricas-financeiro-app/backend/internal/auth"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type SystemHandler struct {
	authService *auth.Service
}

func NewSystemHandler(authService *auth.Service) *SystemHandler {
	return &SystemHandler{authService: authService}
}

func (h *SystemHandler) ListCompanies(c *fiber.Ctx) error {
	items, err := h.authService.ListCompanies(c.Context())
	if err != nil {
		return err
	}

	return c.JSON(items)
}

func (h *SystemHandler) CreateCompany(c *fiber.Ctx) error {
	var request struct {
		Name          string `json:"name"`
		Slug          string `json:"slug"`
		LegalName     string `json:"legal_name"`
		TaxID         string `json:"tax_id"`
		Plan          string `json:"plan"`
		AdminName     string `json:"admin_name"`
		AdminEmail    string `json:"admin_email"`
		AdminPassword string `json:"admin_password"`
	}
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	result, err := h.authService.CreateCompany(c.Context(), auth.CreateCompanyInput{
		Name:          request.Name,
		Slug:          request.Slug,
		LegalName:     request.LegalName,
		TaxID:         request.TaxID,
		Plan:          request.Plan,
		AdminName:     request.AdminName,
		AdminEmail:    request.AdminEmail,
		AdminPassword: request.AdminPassword,
	})
	if err != nil {
		return mapSystemError(err)
	}

	return c.Status(fiber.StatusCreated).JSON(result)
}

func (h *SystemHandler) ListCompanyUsers(c *fiber.Ctx) error {
	companyID, err := uuid.Parse(c.Params("companyID"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid company id")
	}

	items, err := h.authService.ListCompanyUsers(c.Context(), companyID)
	if err != nil {
		return mapSystemError(err)
	}

	return c.JSON(items)
}

func (h *SystemHandler) CreateCompanyUser(c *fiber.Ctx) error {
	companyID, err := uuid.Parse(c.Params("companyID"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid company id")
	}

	var request struct {
		Code     string `json:"code"`
		FullName string `json:"full_name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	item, err := h.authService.CreateCompanyUser(c.Context(), companyID, auth.CreateCompanyUserInput{
		Code:     request.Code,
		FullName: request.FullName,
		Email:    request.Email,
		Password: request.Password,
		Role:     request.Role,
	})
	if err != nil {
		return mapSystemError(err)
	}

	return c.Status(fiber.StatusCreated).JSON(item)
}

func mapSystemError(err error) error {
	switch {
	case errors.Is(err, auth.ErrConflict):
		return fiber.NewError(fiber.StatusConflict, "registro ja existente")
	case errors.Is(err, auth.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "registro nao encontrado")
	default:
		return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("%v", err))
	}
}
