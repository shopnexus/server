package ghtk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"shopnexus-server/internal/provider/transport"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
)

const (
	ServiceExpress  sharedmodel.OptionMethod = "express"
	ServiceStandard sharedmodel.OptionMethod = "standard"
	ServiceEconomy  sharedmodel.OptionMethod = "economy"
)

// GHTKClient implements the transport.Client interface for GHTK (fake implementation)
type GHTKClient struct {
	config   sharedmodel.OptionConfig
	baseURL  string
	apiKey   string
	clientID string

	shipments map[string]*fakeShipment // In-memory storage for fake tracking
}

// fakeShipment represents a fake shipment in our mock system
type fakeShipment struct {
	ID      string
	Service string
	Status  string
	Cost    sharedmodel.Concurrency

	FromAddress string
	ToAddress   string

	WeightGrams int32

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ghtkData is stored as JSONB in the transport data field
type ghtkData struct {
	TrackingID string    `json:"tracking_id"`
	LabelURL   string    `json:"label_url"`
	ETA        time.Time `json:"eta"`
	Location   string    `json:"location,omitempty"`
}

// packageDetails mirrors the shape stored in ItemMetadata.PackageDetails
type packageDetails struct {
	WeightGrams int32 `json:"weight_grams"`
	LengthCM    int32 `json:"length_cm"`
	WidthCM     int32 `json:"width_cm"`
	HeightCM    int32 `json:"height_cm"`
}

// NewClients creates new GHTK transport clients — one per service variant
func NewClients(baseURL, apiKey, clientID string) []*GHTKClient {
	var clients []*GHTKClient
	methods := []sharedmodel.OptionMethod{ServiceExpress, ServiceStandard, ServiceEconomy}

	for _, method := range methods {
		clients = append(clients, &GHTKClient{
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

func (g *GHTKClient) Config() sharedmodel.OptionConfig {
	return g.config
}

// Quote calculates estimated cost without creating a shipment.
// Weight is extracted from PackageDetails of the first item.
func (g *GHTKClient) Quote(ctx context.Context, params transport.QuoteParams) (transport.QuoteResult, error) {
	weightGrams := g.extractWeight(params.Items)
	cost := g.calculateShippingCost(weightGrams, g.config.Method)

	data, err := json.Marshal(ghtkData{
		ETA: g.calculateETA(g.config.Method),
	})
	if err != nil {
		return transport.QuoteResult{}, fmt.Errorf("marshal quote data: %w", err)
	}

	return transport.QuoteResult{
		Cost: int64(cost),
		Data: data,
	}, nil
}

// Create books a shipment and returns transport with tracking data in Data JSONB.
func (g *GHTKClient) Create(ctx context.Context, params transport.CreateParams) (transport.Transport, error) {
	trackingID := g.generateTrackingID()
	weightGrams := g.extractWeight(params.Items)
	cost := g.calculateShippingCost(weightGrams, g.config.Method)
	eta := g.calculateETA(g.config.Method)

	ship := &fakeShipment{
		ID:          trackingID,
		Service:     string(g.config.Method),
		Status:      "LabelCreated",
		Cost:        cost,
		FromAddress: params.FromAddress,
		ToAddress:   params.ToAddress,
		WeightGrams: weightGrams,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	g.shipments[trackingID] = ship

	id, err := uuid.NewRandom()
	if err != nil {
		return transport.Transport{}, fmt.Errorf("generate transport id: %w", err)
	}

	data, err := json.Marshal(ghtkData{
		TrackingID: trackingID,
		LabelURL:   fmt.Sprintf("%s/labels/%s.pdf", g.baseURL, trackingID),
		ETA:        eta,
	})
	if err != nil {
		return transport.Transport{}, fmt.Errorf("marshal transport data: %w", err)
	}

	return transport.Transport{
		ID:     id,
		Option: string(g.config.Method),
		Cost:   int64(cost),
		Data:   data,
	}, nil
}

// Track returns the current status for a given tracking id.
func (g *GHTKClient) Track(ctx context.Context, id string) (transport.TrackResult, error) {
	ship, exists := g.shipments[id]
	if !exists {
		return transport.TrackResult{}, fmt.Errorf("tracking id not found: %s", id)
	}

	g.updateShipmentStatus(ship)

	location := g.getCurrentLocation(ship)
	data, err := json.Marshal(ghtkData{
		TrackingID: id,
		Location:   location,
	})
	if err != nil {
		return transport.TrackResult{}, fmt.Errorf("marshal track data: %w", err)
	}

	return transport.TrackResult{
		Status: ship.Status,
		Data:   data,
	}, nil
}

// Cancel attempts to cancel a shipment by its tracking id.
func (g *GHTKClient) Cancel(ctx context.Context, id string) error {
	ship, exists := g.shipments[id]
	if !exists {
		return fmt.Errorf("tracking id not found: %s", id)
	}

	if ship.Status == "Delivered" {
		return fmt.Errorf("cannot cancel delivered shipment: %s", id)
	}

	ship.Status = "Cancelled"
	ship.UpdatedAt = time.Now()
	return nil
}

// extractWeight pulls WeightGrams from the PackageDetails of the first item, defaulting to 500g.
func (g *GHTKClient) extractWeight(items []transport.ItemMetadata) int32 {
	if len(items) == 0 {
		return 500
	}
	var pkg packageDetails
	if err := json.Unmarshal(items[0].PackageDetails, &pkg); err != nil || pkg.WeightGrams == 0 {
		return 500
	}
	return pkg.WeightGrams
}

// calculateShippingCost calculates shipping cost based on weight and service type.
func (g *GHTKClient) calculateShippingCost(weightGrams int32, service sharedmodel.OptionMethod) sharedmodel.Concurrency {
	baseCost := sharedmodel.Int64ToConcurrency(15000) // 15,000 VND base cost

	var weightCost sharedmodel.Concurrency
	if weightGrams > 1000 {
		weightCost = sharedmodel.Int64ToConcurrency((int64(weightGrams) - 1000) / 1000 * 2000) // 2,000 VND per additional kg
	}

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

// calculateETA calculates estimated time of arrival.
func (g *GHTKClient) calculateETA(service sharedmodel.OptionMethod) time.Time {
	now := time.Now()
	switch service {
	case ServiceExpress:
		return now.Add(24 * time.Hour)
	case ServiceStandard:
		return now.Add(48 * time.Hour)
	case ServiceEconomy:
		return now.Add(72 * time.Hour)
	default:
		return now.Add(48 * time.Hour)
	}
}

// updateShipmentStatus simulates shipment status progression.
func (g *GHTKClient) updateShipmentStatus(ship *fakeShipment) {
	now := time.Now()
	elapsed := now.Sub(ship.CreatedAt)

	switch {
	case elapsed < 30*time.Minute:
		ship.Status = "LabelCreated"
	case elapsed < 2*time.Hour:
		ship.Status = "InTransit"
	case elapsed < 24*time.Hour:
		ship.Status = "OutForDelivery"
	case elapsed > 24*time.Hour && ship.Status != "Delivered":
		ship.Status = "Delivered"
	}

	ship.UpdatedAt = now
}

// getCurrentLocation returns a fake location based on shipment status.
func (g *GHTKClient) getCurrentLocation(ship *fakeShipment) string {
	switch ship.Status {
	case "LabelCreated":
		return "Hà Nội - Đang chuẩn bị hàng"
	case "InTransit":
		return "Trung tâm phân phối - Đang vận chuyển"
	case "OutForDelivery":
		return "Đang giao hàng - Gần địa chỉ nhận"
	case "Delivered":
		return "Đã giao hàng thành công"
	case "Failed":
		return "Giao hàng thất bại"
	case "Cancelled":
		return "Đơn hàng đã hủy"
	default:
		return "Đang xử lý"
	}
}

// generateTrackingID generates a fake GHTK tracking ID.
func (g *GHTKClient) generateTrackingID() string {
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	randomHex := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("GTK%s", strings.ToUpper(randomHex))
}
