package middleware

import (
	"github.com/canal/metricas-financeiro-app/backend/internal/auth"
	"github.com/gofiber/fiber/v2"
)

const authContextKey = "auth_context"

type AuthContext struct {
	Claims auth.TokenClaims `json:"claims"`
}

func RequireAuthenticated(authService *auth.Service) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, err := authenticateTokenFromRequest(c, authService)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid or missing authorization token")
		}

		c.Locals(authContextKey, AuthContext{Claims: claims})
		return c.Next()
	}
}

func RequireSystemAuth(authService *auth.Service) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, err := authenticateTokenFromRequest(c, authService)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid or missing authorization token")
		}
		if claims.SessionType != auth.SessionTypeSystem {
			return fiber.NewError(fiber.StatusForbidden, "system access required")
		}

		c.Locals(authContextKey, AuthContext{Claims: claims})
		return c.Next()
	}
}

func GetAuthContext(c *fiber.Ctx) AuthContext {
	value, ok := c.Locals(authContextKey).(AuthContext)
	if !ok {
		return AuthContext{}
	}
	return value
}

func authenticateTokenFromRequest(c *fiber.Ctx, authService *auth.Service) (auth.TokenClaims, error) {
	token := auth.ExtractBearerToken(c.Get("Authorization"))
	if token == "" {
		return auth.TokenClaims{}, fiber.ErrUnauthorized
	}

	return authService.AuthenticateToken(token)
}
