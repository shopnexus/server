package commonecho

import (
	"net/http"

	"shopnexus-server/internal/shared/response"

	"github.com/labstack/echo/v4"
)

type ReverseGeocodeRequest struct {
	Latitude  float64 `json:"latitude" validate:"required"`
	Longitude float64 `json:"longitude" validate:"required"`
}

// ReverseGeocode converts lat/lng coordinates to an address string.
func (h *Handler) ReverseGeocode(c echo.Context) error {
	var req ReverseGeocodeRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.geocoder.ReverseGeocode(c.Request().Context(), req.Latitude, req.Longitude)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type ForwardGeocodeRequest struct {
	Address string `json:"address" validate:"required"`
}

// ForwardGeocode converts an address string to lat/lng coordinates.
func (h *Handler) ForwardGeocode(c echo.Context) error {
	var req ForwardGeocodeRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	result, err := h.geocoder.ForwardGeocode(c.Request().Context(), req.Address)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}

type SearchGeocodeRequest struct {
	Query string `query:"q" validate:"required,min=2"`
	Limit int    `query:"limit"`
}

// SearchGeocode returns location suggestions matching a partial query.
func (h *Handler) SearchGeocode(c echo.Context) error {
	var req SearchGeocodeRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	results, err := h.geocoder.Search(c.Request().Context(), req.Query, req.Limit)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, results)
}
