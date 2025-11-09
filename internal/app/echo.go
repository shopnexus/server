package app

import (
	"context"
	"log/slog"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/module/shared/binder"

	"shopnexus-remastered/internal/module/shared/validator"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/fx"
)

// NewEcho creates a new Echo instance
func NewEcho() *echo.Echo {
	e := echo.New()

	// Middleware
	//e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	return e
}

// RouteParams holds all the dependencies needed for route registration
type RouteParams struct {
	fx.In
	Echo *echo.Echo
}

// SetupEcho registers all application routes
func SetupEcho(params RouteParams) {
	// Set the custom validator
	customVal, err := validator.New()
	if err != nil {
		slog.Error("Failed to create validator", err)
		panic(err)
	}
	params.Echo.Validator = customVal
	params.Echo.Binder = binder.NewCustomBinder()

	// Health check
	params.Echo.GET("/health", func(c echo.Context) error {
		return c.JSON(200, map[string]string{"status": "ok"})
	})
}

// StartHTTPServer starts the HTTP server with lifecycle management
func StartHTTPServer(lc fx.Lifecycle, e *echo.Echo, cfg *config.Config) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				port := ":8080" // Default port, you can make this configurable
				slog.Info("Starting HTTP server on port", "port", port)
				if err := e.Start(port); err != nil {
					slog.Error("HTTP server error", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			slog.Info("Shutting down HTTP server...")
			return e.Shutdown(ctx)
		},
	})
}
