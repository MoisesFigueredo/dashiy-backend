package http

import (
	"errors"
	"log/slog"
	"net/url"
	"strings"

	"github.com/canal/metricas-financeiro-app/backend/internal/auth"
	"github.com/canal/metricas-financeiro-app/backend/internal/cache"
	"github.com/canal/metricas-financeiro-app/backend/internal/http/handlers"
	"github.com/canal/metricas-financeiro-app/backend/internal/http/middleware"
	syncservice "github.com/canal/metricas-financeiro-app/backend/internal/sync"
	"github.com/canal/metricas-financeiro-app/backend/internal/utmify"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	fiberRecover "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Dependencies struct {
	Logger         *slog.Logger
	DB             *pgxpool.Pool
	AuthService    *auth.Service
	AllowedOrigins string
	SyncService    *syncservice.Service
	UTMifyClient   *utmify.Client
	Cache          *cache.Redis
}

func NewRouter(deps Dependencies) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			var fiberErr *fiber.Error
			if errors.As(err, &fiberErr) {
				return c.Status(fiberErr.Code).JSON(fiber.Map{
					"error": fiberErr.Message,
				})
			}

			deps.Logger.Error("request failed",
				slog.String("method", c.Method()),
				slog.String("path", c.Path()),
				slog.String("error", err.Error()),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		},
	})

	app.Use(requestid.New())
	app.Use(fiberRecover.New())
	app.Use(cors.New(cors.Config{
		AllowOriginsFunc: func(origin string) bool {
			return isAllowedOrigin(origin, deps.AllowedOrigins)
		},
		AllowMethods:  "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders:  "Origin, Content-Type, Accept, Authorization, X-Company-ID, X-Niche-ID, X-User-ID, X-User-Role, X-Collaborator-ID",
		ExposeHeaders: "X-Request-ID",
	}))

	healthHandler := handlers.NewHealthHandler(deps.DB)
	authHandler := handlers.NewAuthHandler(deps.AuthService)
	systemHandler := handlers.NewSystemHandler(deps.AuthService)
	adsHandler := handlers.NewAdsHandler(deps.DB)
	collaboratorsHandler := handlers.NewCollaboratorsHandler(deps.DB, deps.SyncService, deps.Logger)
	collaboratorHandler := handlers.NewCollaboratorHandler(deps.DB)
	dashboardHandler := handlers.NewDashboardHandler(deps.DB, deps.UTMifyClient, deps.Cache)
	adObjectsHandler := handlers.NewAdObjectsHandler(deps.DB, deps.Cache, deps.UTMifyClient)
	syncHandler := handlers.NewSyncHandler(deps.SyncService)

	registerRoutes := func(api fiber.Router) {
		api.Get("/health", healthHandler.Check)
		api.Get("/auth/bootstrap-status", authHandler.BootstrapStatus)
		api.Post("/auth/system/setup", authHandler.SetupSystem)
		api.Post("/auth/system/login", authHandler.LoginSystem)
		api.Post("/auth/company/login", authHandler.LoginCompany)
		api.Get("/auth/me", middleware.RequireAuthenticated(deps.AuthService), authHandler.Me)

		system := api.Group("/system", middleware.RequireSystemAuth(deps.AuthService))
		system.Get("/companies", systemHandler.ListCompanies)
		system.Post("/companies", systemHandler.CreateCompany)
		system.Get("/companies/:companyID/users", systemHandler.ListCompanyUsers)
		system.Post("/companies/:companyID/users", systemHandler.CreateCompanyUser)

		company := api.Group("", middleware.RequireCompanyContext(deps.AuthService))
		company.Post("/sync/today", syncHandler.SyncToday)
		company.Post("/auth/change-password", authHandler.ChangeCompanyPassword)
		company.Get("/ads", adsHandler.List)
		company.Get("/ads/summary", adsHandler.Summary)
		// Collaborator management – admin only
		adminOnly := middleware.RequireAdmin()
		company.Get("/collaborators", adminOnly, collaboratorsHandler.List)
		company.Post("/collaborators", adminOnly, collaboratorsHandler.Create)
		company.Put("/collaborators/:id", adminOnly, collaboratorsHandler.Update)
		company.Delete("/collaborators/:id", adminOnly, collaboratorsHandler.Delete)
		company.Get("/collaborators/dashboard/:dashboardId", adminOnly, collaboratorsHandler.ListByDashboard)
		company.Put("/collaborators/dashboard/:dashboardId/:collaboratorId", adminOnly, collaboratorsHandler.UpdateDashboardCommission)
		company.Get("/collaborators/dashboard/:dashboardId/access", adminOnly, collaboratorsHandler.GetDashboardAccess)
		company.Put("/collaborators/dashboard/:dashboardId/:collaboratorId/access", adminOnly, collaboratorsHandler.SetDashboardAccess)
		company.Get("/collaborators/:id/commissions", adminOnly, collaboratorsHandler.MonthlyCommissions)
		company.Get("/collaborator/me", collaboratorHandler.Me)
		company.Get("/collaborator/me/history", collaboratorHandler.MeHistory)
		company.Get("/dashboard/list", dashboardHandler.List)
		company.Get("/dashboard/summary", dashboardHandler.Summary)
		company.Get("/dashboard/history", dashboardHandler.History)
		company.Get("/ads/objects", adObjectsHandler.List)
	}

	registerRoutes(app.Group("/api"))
	registerRoutes(app.Group("/api/v1"))

	return app
}

func isAllowedOrigin(origin string, configuredOrigins string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return false
	}

	parsed, err := url.Parse(origin)
	if err == nil {
		host := strings.ToLower(parsed.Hostname())
		if (parsed.Scheme == "http" || parsed.Scheme == "https") && (host == "localhost" || host == "127.0.0.1" || host == "::1") {
			return true
		}
	}

	for _, allowedOrigin := range strings.Split(configuredOrigins, ",") {
		if strings.EqualFold(strings.TrimSpace(allowedOrigin), origin) {
			return true
		}
	}

	return false
}
