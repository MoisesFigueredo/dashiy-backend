package handlers

import (
	"errors"
	"fmt"

	"github.com/canal/metricas-financeiro-app/backend/internal/auth"
	"github.com/canal/metricas-financeiro-app/backend/internal/http/middleware"
	"github.com/gofiber/fiber/v2"
)

type AuthHandler struct {
	authService *auth.Service
}

func NewAuthHandler(authService *auth.Service) *AuthHandler {
	return &AuthHandler{authService: authService}
}

func (h *AuthHandler) BootstrapStatus(c *fiber.Ctx) error {
	status, err := h.authService.BootstrapStatus(c.Context())
	if err != nil {
		return err
	}

	return c.JSON(status)
}

func (h *AuthHandler) SetupSystem(c *fiber.Ctx) error {
	var request struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	session, err := h.authService.SetupInitialSystemUser(c.Context(), request.Email, request.Password, c.IP())
	if err != nil {
		return mapAuthError(err)
	}

	return c.Status(fiber.StatusCreated).JSON(session)
}

func (h *AuthHandler) LoginSystem(c *fiber.Ctx) error {
	var request struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	session, err := h.authService.LoginSystem(c.Context(), request.Email, request.Password, c.IP())
	if err != nil {
		return mapAuthError(err)
	}

	return c.JSON(session)
}

func (h *AuthHandler) LoginCompany(c *fiber.Ctx) error {
	var request struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	session, err := h.authService.LoginCompanyUser(c.Context(), request.Email, request.Password)
	if err != nil {
		return mapAuthError(err)
	}

	return c.JSON(session)
}

func (h *AuthHandler) Me(c *fiber.Ctx) error {
	authCtx := middleware.GetAuthContext(c)
	session := h.authService.SessionFromClaims(authCtx.Claims)
	if session.SessionKind == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid authorization token")
	}

	return c.JSON(session)
}

func (h *AuthHandler) ChangeCompanyPassword(c *fiber.Ctx) error {
	scope := middleware.GetCompanyContext(c)
	if scope.UserID == nil {
		return fiber.NewError(fiber.StatusUnauthorized, "usuario autenticado nao encontrado")
	}

	var request struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	if err := h.authService.ChangeCompanyUserPassword(
		c.Context(),
		scope.CompanyID,
		*scope.UserID,
		request.CurrentPassword,
		request.NewPassword,
	); err != nil {
		return mapAuthError(err)
	}

	return c.JSON(fiber.Map{"ok": true})
}

func mapAuthError(err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		return fiber.NewError(fiber.StatusUnauthorized, "credenciais invalidas")
	case errors.Is(err, auth.ErrSystemBootstrapClosed):
		return fiber.NewError(fiber.StatusConflict, "o usuario principal do sistema ja foi configurado")
	case errors.Is(err, auth.ErrConflict):
		return fiber.NewError(fiber.StatusConflict, "registro ja existente")
	case errors.Is(err, auth.ErrForbidden):
		return fiber.NewError(fiber.StatusForbidden, "acesso nao permitido")
	default:
		return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("%v", err))
	}
}
