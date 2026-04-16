package metrics

import (
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
)

// EchoMiddleware records HTTP request metrics for each handler.
func EchoMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			// Use route pattern not raw path to avoid cardinality explosion
			path := c.Path()
			if path == "" {
				path = "unknown"
			}
			method := c.Request().Method
			status := strconv.Itoa(c.Response().Status)
			HTTPRequestsTotal.WithLabelValues(method, path, status).Inc()
			HTTPRequestDuration.WithLabelValues(method, path).Observe(time.Since(start).Seconds())
			return err
		}
	}
}
