package middleware

import (
	"strings"

	"github.com/canal/metricas-financeiro-app/backend/internal/auth"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const companyContextKey = "company_context"

type CompanyContext struct {
	CompanyID uuid.UUID  `json:"company_id"`
	NicheID   *uuid.UUID `json:"niche_id,omitempty"`
	UserID    *uuid.UUID `json:"user_id,omitempty"`
	Role      string     `json:"role,omitempty"`
}

func RequireCompanyContext(authService *auth.Service) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if token := auth.ExtractBearerToken(c.Get("Authorization")); token != "" {
			claims, err := authenticateTokenFromRequest(c, authService)
			if err != nil {
				return fiber.NewError(fiber.StatusUnauthorized, "invalid authorization token")
			}
			if claims.SessionType != auth.SessionTypeCompany {
				return fiber.NewError(fiber.StatusForbidden, "company access required")
			}

			companyID, err := uuid.Parse(claims.CompanyID)
			if err != nil {
				return fiber.NewError(fiber.StatusUnauthorized, "invalid company token")
			}

			ctx := CompanyContext{
				CompanyID: companyID,
				Role:      strings.TrimSpace(claims.Role),
			}

			if claims.Subject != "" {
				userID, err := uuid.Parse(claims.Subject)
				if err != nil {
					return fiber.NewError(fiber.StatusUnauthorized, "invalid user token")
				}
				ctx.UserID = &userID
			}

			if nicheID, err := parseOptionalUUIDValue(c, "X-Niche-ID", "niche_id"); err == nil {
				ctx.NicheID = nicheID
			} else {
				return fiber.NewError(fiber.StatusBadRequest, "invalid niche context")
			}

			c.Locals(companyContextKey, ctx)
			return c.Next()
		}

		companyID, err := parseUUIDValue(c, "X-Company-ID", "company_id")
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "missing or invalid company context")
		}

		ctx := CompanyContext{
			CompanyID: companyID,
			Role:      strings.TrimSpace(firstNonEmpty(c.Get("X-User-Role"), c.Query("user_role"))),
		}

		if nicheID, err := parseOptionalUUIDValue(c, "X-Niche-ID", "niche_id"); err == nil {
			ctx.NicheID = nicheID
		} else {
			return fiber.NewError(fiber.StatusBadRequest, "invalid niche context")
		}

		if userID, err := parseOptionalUUIDValue(c, "X-User-ID", "user_id"); err == nil {
			ctx.UserID = userID
		} else {
			return fiber.NewError(fiber.StatusBadRequest, "invalid user context")
		}

		c.Locals(companyContextKey, ctx)
		return c.Next()
	}
}

func GetCompanyContext(c *fiber.Ctx) CompanyContext {
	value, ok := c.Locals(companyContextKey).(CompanyContext)
	if !ok {
		return CompanyContext{}
	}
	return value
}

func parseUUIDValue(c *fiber.Ctx, headerKey string, queryKey string) (uuid.UUID, error) {
	value := strings.TrimSpace(firstNonEmpty(c.Get(headerKey), c.Query(queryKey)))
	return uuid.Parse(value)
}

func parseOptionalUUIDValue(c *fiber.Ctx, headerKey string, queryKey string) (*uuid.UUID, error) {
	value := strings.TrimSpace(firstNonEmpty(c.Get(headerKey), c.Query(queryKey)))
	if value == "" {
		return nil, nil
	}

	id, err := uuid.Parse(value)
	if err != nil {
		return nil, err
	}

	return &id, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
