package ghtk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"shopnexus-server/internal/infras/shipment"
	sharedmodel "shopnexus-server/internal/shared/model"
	"strings"
	"time"
)

const (
	ServiceExpress  sharedmodel.OptionMethod = "express"
	ServiceStandard sharedmodel.OptionMethod = "standard"
	ServiceEconomy  sharedmodel.OptionMethod = "economy"
)

// GTKClient implements the shipment.Client interface for GTK (fake implementation)
type GTKClient struct {
	config   sharedmodel.OptionConfig
	baseURL  string
	apiKey   string
	clientID string

	shipments map[string]*fakeShipment // In-memory storage for fake tracking
}

// fakeShipment represents a fake shipment in our mock system
type fakeShipment struct {
	ID           string
	LabelURL     string
	Service      string
	Status       shipment.ShipmentStatus
	Costs        sharedmodel.Concurrency
	EstimatedETA time.Time

	FromAddress string
	ToAddress   string

	// Package details
	WeightGrams int32
	LengthCM    int32
	WidthCM     int32
	HeightCM    int32

	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewClients creates a new GTK shipment clients
func NewClients(baseURL, apiKey, clientID string) []*GTKClient {
	var clients []*GTKClient
	methods := []sharedmodel.OptionMethod{ServiceExpress, ServiceStandard, ServiceEconomy}

	for _, method := range methods {
		clients = append(clients, &GTKClient{
			config: sharedmodel.OptionConfig{
				ID:          fmt.Sprintf("ghtk_%s", method),
				Name:        fmt.Sprintf("Giao hàng tiết kiệm - %s", string(method)),
				Description: "Dịch vụ giao hàng nhanh của Giao hàng tiết kiệm",
				Provider:    "ghtk",
				Method:      method,
			},
			baseURL:   baseURL,
			apiKey:    apiKey,
			clientID:  clientID,
			shipments: make(map[string]*fakeShipment),
		})
	}
	return clients
}

func (g *GTKClient) Config() sharedmodel.OptionConfig {
	return g.config
}

// Quote calculates estimated cost & ETD without creating a shipment
func (g *GTKClient) Quote(ctx context.Context, params shipment.CreateParams) (shipment.QuoteResult, error) {
	cost := g.calculateShippingCost(params.Package.WeightGrams, g.config.Method)
	etd := g.calculateETA(g.config.Method)

	return shipment.QuoteResult{
		ETA:   etd,
		Costs: cost,
	}, nil
}

// CreateShipment books a shipment and returns label + tracking info
func (g *GTKClient) Create(ctx context.Context, params shipment.CreateParams) (shipment.ShippingOrder, error) {
	trackingID := g.generateTrackingID()
	cost := g.calculateShippingCost(params.Package.WeightGrams, g.config.Method)
	eta := g.calculateETA(g.config.Method)

	// Create fake shipment record
	ship := &fakeShipment{
		ID:           trackingID,
		LabelURL:     fmt.Sprintf("%s/labels/%s.pdf", g.baseURL, trackingID),
		Service:      string(g.config.Method),
		Status:       shipment.ShipmentStatusLabelCreated,
		Costs:        cost,
		EstimatedETA: eta,
		FromAddress:  params.FromAddress,
		ToAddress:    params.ToAddress,
		WeightGrams:  params.Package.WeightGrams,
		LengthCM:     params.Package.LengthCM,
		WidthCM:      params.Package.WidthCM,
		HeightCM:     params.Package.HeightCM,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// Store in memory
	g.shipments[trackingID] = ship

	return shipment.ShippingOrder{
		ID:       trackingID,
		LabelURL: ship.LabelURL,
		Service:  ship.Service,
		ETA:      eta,
		Costs:    cost,
	}, nil
}

// Track returns the current status for a given tracking ShipmentID
func (g *GTKClient) Track(ctx context.Context, trackingID string) (shipment.TrackResult, error) {
	ship, exists := g.shipments[trackingID]
	if !exists {
		return shipment.TrackResult{}, fmt.Errorf("tracking ShipmentID not found: %s", trackingID)
	}

	// Simulate status progression based on time elapsed
	g.updateShipmentStatus(ship)

	return shipment.TrackResult{
		ID:        trackingID,
		Status:    ship.Status,
		UpdatedAt: ship.UpdatedAt.Format(time.RFC3339),
		Location:  g.getCurrentLocation(ship),
	}, nil
}

// calculateShippingCost calculates shipping cost based on weight and service type
func (g *GTKClient) calculateShippingCost(weightGrams int32, service sharedmodel.OptionMethod) sharedmodel.Concurrency {
	baseCost := sharedmodel.Int64ToConcurrency(15000) // 15,000 VND base cost

	// Weight-based pricing
	var weightCost sharedmodel.Concurrency
	if weightGrams > 1000 {
		weightCost = sharedmodel.Int64ToConcurrency((int64(weightGrams) - 1000) / 1000 * 2000) // 2,000 VND per additional kg
	}

	// Service type multiplier
	serviceMultiplier := float64(1.0)
	switch service {
	case ServiceExpress:
		serviceMultiplier = 1.5
	case ServiceStandard:
		serviceMultiplier = 1.0
	case ServiceEconomy:
		serviceMultiplier = 0.8
	default:
		serviceMultiplier = 1.0
	}

	totalCost := sharedmodel.FloatToConcurrency((baseCost + weightCost).Float64() * serviceMultiplier)
	// TODO: add currency conversion in concurrency struct
	return totalCost / 27000 // temporary convert to usdt
}

// calculateETA calculates estimated time of arrival
func (g *GTKClient) calculateETA(service sharedmodel.OptionMethod) time.Time {
	now := time.Now()

	switch service {
	case ServiceExpress:
		return now.Add(24 * time.Hour) // 1 day
	case ServiceStandard:
		return now.Add(48 * time.Hour) // 2 days
	case ServiceEconomy:
		return now.Add(72 * time.Hour) // 3 days
	default:
		return now.Add(48 * time.Hour) // 2 days default
	}
}

// updateShipmentStatus simulates shipment status progression
func (g *GTKClient) updateShipmentStatus(ship *fakeShipment) {
	now := time.Now()
	elapsed := now.Sub(ship.CreatedAt)

	// Simulate status progression based on elapsed time
	switch {
	case elapsed < 30*time.Minute:
		ship.Status = shipment.ShipmentStatusLabelCreated
	case elapsed < 2*time.Hour:
		ship.Status = shipment.ShipmentStatusInTransit
	case elapsed < 24*time.Hour:
		ship.Status = shipment.ShipmentStatusOutForDelivery
	case elapsed > 24*time.Hour && ship.Status != shipment.ShipmentStatusDelivered:
		ship.Status = shipment.ShipmentStatusDelivered
	}

	ship.UpdatedAt = now
}

// getCurrentLocation returns a fake location based on shipment status
func (g *GTKClient) getCurrentLocation(ship *fakeShipment) string {
	switch ship.Status {
	case shipment.ShipmentStatusLabelCreated:
		return "Hà Nội - Đang chuẩn bị hàng"
	case shipment.ShipmentStatusInTransit:
		return "Trung tâm phân phối - Đang vận chuyển"
	case shipment.ShipmentStatusOutForDelivery:
		return "Đang giao hàng - Gần địa chỉ nhận"
	case shipment.ShipmentStatusDelivered:
		return "Đã giao hàng thành công"
	case shipment.ShipmentStatusFailed:
		return "Giao hàng thất bại"
	case shipment.ShipmentStatusCancelled:
		return "Đơn hàng đã hủy"
	default:
		return "Đang xử lý"
	}
}

// generateTrackingID generates a fake GTK tracking ShipmentID
func (g *GTKClient) generateTrackingID() string {
	// GTK tracking IDs typically look like: GTK123456789
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	randomHex := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("GTK%s", strings.ToUpper(randomHex))
}

// generateFakeID generates a fake shipment ShipmentID
func (g *GTKClient) generateFakeID() string {
	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)
	return hex.EncodeToString(randomBytes)
}

// Cancel attempts to cancel a shipment by its tracking id
func (g *GTKClient) Cancel(ctx context.Context, id string) error {
	ship, exists := g.shipments[id]
	if !exists {
		return fmt.Errorf("tracking ShipmentID not found: %s", id)
	}

	// If it's already delivered, we cannot cancel
	if ship.Status == shipment.ShipmentStatusDelivered {
		return fmt.Errorf("cannot cancel delivered shipment: %s", id)
	}

	ship.Status = shipment.ShipmentStatusCancelled
	ship.UpdatedAt = time.Now()
	return nil
}
