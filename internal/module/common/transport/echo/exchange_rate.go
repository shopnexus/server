package commonecho

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// GetExchangeRates returns the latest exchange rate snapshot.
// Public endpoint; no auth required.
func (h *Handler) GetExchangeRates(c echo.Context) error {
	snap, err := h.biz.GetExchangeRates(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, snap)
}
