package shipment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"shopnexus-remastered/internal/db"
	"strings"
	"time"
)

// GTKClient implements the shipment.Client interface for GTK (fake implementation)
type GTKClient struct {
	baseURL   string
	apiKey    string
	clientID  string
	shipments map[string]*fakeShipment // In-memory storage for fake tracking
}

// fakeShipment represents a fake shipment in our mock system
type fakeShipment struct {
	ID           string
	TrackingID   string
	LabelURL     string
	Service      string
	Status       db.OrderShipmentStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
	EstimatedETA time.Time
	CostCents    int64
	FromAddress  string
	ToAddress    string
	WeightGrams  int64
}

// NewGTKClient creates a new GTK shipment client
func NewGTKClient(baseURL, apiKey, clientID string) *GTKClient {
	return &GTKClient{
		baseURL:   baseURL,
		apiKey:    apiKey,
		clientID:  clientID,
		shipments: make(map[string]*fakeShipment),
	}
}

// Quote calculates estimated cost & ETD without creating a shipment
func (g *GTKClient) Quote(ctx context.Context, params CreateShipmentParams) (Shipment, error) {
	cost := g.calculateShippingCost(params.WeightGrams, params.Service)
	etd := g.calculateETA(params.Service)

	return Shipment{
		ID:         g.generateFakeID(),
		LabelURL:   "", // No label URL for quotes
		TrackingID: "", // No tracking ID for quotes
		Service:    params.Service,
		ETA:        etd,
		CostCents:  cost,
	}, nil
}

// CreateShipment books a shipment and returns label + tracking info
func (g *GTKClient) CreateShipment(ctx context.Context, params CreateShipmentParams) (Shipment, error) {
	trackingID := g.generateTrackingID()
	shipmentID := g.generateFakeID()
	cost := g.calculateShippingCost(params.WeightGrams, params.Service)
	eta := g.calculateETA(params.Service)

	// Create fake shipment record
	shipment := &fakeShipment{
		ID:           shipmentID,
		TrackingID:   trackingID,
		LabelURL:     fmt.Sprintf("%s/labels/%s.pdf", g.baseURL, trackingID),
		Service:      params.Service,
		Status:       db.OrderShipmentStatusLabelCreated,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		EstimatedETA: eta,
		CostCents:    cost,
		FromAddress:  params.FromAddress,
		ToAddress:    params.ToAddress,
		WeightGrams:  params.WeightGrams,
	}

	// Store in memory
	g.shipments[trackingID] = shipment

	return Shipment{
		ID:         shipmentID,
		LabelURL:   shipment.LabelURL,
		TrackingID: trackingID,
		Service:    params.Service,
		ETA:        eta,
		CostCents:  cost,
	}, nil
}

// Track returns the current status for a given tracking ID
func (g *GTKClient) Track(ctx context.Context, trackingID string) (TrackResult, error) {
	shipment, exists := g.shipments[trackingID]
	if !exists {
		return TrackResult{}, fmt.Errorf("tracking ID not found: %s", trackingID)
	}

	// Simulate status progression based on time elapsed
	g.updateShipmentStatus(shipment)

	return TrackResult{
		TrackingID: trackingID,
		Status:     shipment.Status,
		UpdatedAt:  shipment.UpdatedAt.Format(time.RFC3339),
		Location:   g.getCurrentLocation(shipment),
	}, nil
}

// calculateShippingCost calculates shipping cost based on weight and service type
func (g *GTKClient) calculateShippingCost(weightGrams int64, service string) int64 {
	baseCost := int64(15000) // 15,000 VND base cost

	// Weight-based pricing
	weightCost := int64(0)
	if weightGrams > 1000 {
		weightCost = (weightGrams - 1000) / 1000 * 2000 // 2,000 VND per additional kg
	}

	// Service type multiplier
	serviceMultiplier := float64(1.0)
	switch strings.ToLower(service) {
	case "express":
		serviceMultiplier = 1.5
	case "standard":
		serviceMultiplier = 1.0
	case "economy":
		serviceMultiplier = 0.8
	default:
		serviceMultiplier = 1.0
	}

	totalCost := float64(baseCost+weightCost) * serviceMultiplier
	return int64(math.Ceil(totalCost))
}

// calculateETA calculates estimated time of arrival
func (g *GTKClient) calculateETA(service string) time.Time {
	now := time.Now()

	switch strings.ToLower(service) {
	case "express":
		return now.Add(24 * time.Hour) // 1 day
	case "standard":
		return now.Add(48 * time.Hour) // 2 days
	case "economy":
		return now.Add(72 * time.Hour) // 3 days
	default:
		return now.Add(48 * time.Hour) // 2 days default
	}
}

// updateShipmentStatus simulates shipment status progression
func (g *GTKClient) updateShipmentStatus(shipment *fakeShipment) {
	now := time.Now()
	elapsed := now.Sub(shipment.CreatedAt)

	// Simulate status progression based on elapsed time
	switch {
	case elapsed < 30*time.Minute:
		shipment.Status = db.OrderShipmentStatusLabelCreated
	case elapsed < 2*time.Hour:
		shipment.Status = db.OrderShipmentStatusInTransit
	case elapsed < 24*time.Hour:
		shipment.Status = db.OrderShipmentStatusOutForDelivery
	case elapsed > 24*time.Hour && shipment.Status != db.OrderShipmentStatusDelivered:
		shipment.Status = db.OrderShipmentStatusDelivered
	}

	shipment.UpdatedAt = now
}

// getCurrentLocation returns a fake location based on shipment status
func (g *GTKClient) getCurrentLocation(shipment *fakeShipment) string {
	switch shipment.Status {
	case db.OrderShipmentStatusLabelCreated:
		return "Hà Nội - Đang chuẩn bị hàng"
	case db.OrderShipmentStatusInTransit:
		return "Trung tâm phân phối - Đang vận chuyển"
	case db.OrderShipmentStatusOutForDelivery:
		return "Đang giao hàng - Gần địa chỉ nhận"
	case db.OrderShipmentStatusDelivered:
		return "Đã giao hàng thành công"
	case db.OrderShipmentStatusFailed:
		return "Giao hàng thất bại"
	case db.OrderShipmentStatusCancelled:
		return "Đơn hàng đã hủy"
	default:
		return "Đang xử lý"
	}
}

// generateTrackingID generates a fake GTK tracking ID
func (g *GTKClient) generateTrackingID() string {
	// GTK tracking IDs typically look like: GTK123456789
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	randomHex := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("GTK%s", strings.ToUpper(randomHex))
}

// generateFakeID generates a fake shipment ID
func (g *GTKClient) generateFakeID() string {
	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)
	return hex.EncodeToString(randomBytes)
}
