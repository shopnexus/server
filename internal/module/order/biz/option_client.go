package orderbiz

import (
	"log/slog"
	sharedmodel "shopnexus-server/internal/shared/model"
)

type FactoryFunc[Client any] func(sharedmodel.Option) Client

// Map is a generic registry of provider clients keyed by Option.ID,
// with a factory per Option.Type that decodes Option.Data into the
// provider-specific config struct.
type Map[Client any] struct {
	Clients   map[string]Client
	factories map[sharedmodel.OptionType]FactoryFunc[Client]
}

func NewMap[Client any]() *Map[Client] {
	return &Map[Client]{
		Clients:   make(map[string]Client),
		factories: make(map[sharedmodel.OptionType]FactoryFunc[Client]),
	}
}

func (m *Map[Client]) Get(id string) (Client, bool) {
	c, ok := m.Clients[id]
	return c, ok
}

// Register installs a factory for a given Option.Type.
func (m *Map[Client]) Register(t sharedmodel.OptionType, factory FactoryFunc[Client]) {
	m.factories[t] = factory
}

// Add instantiates a client per config using the factory matched by cfg.Type.
func (m *Map[Client]) Add(configs ...sharedmodel.Option) {
	for _, cfg := range configs {
		factory, ok := m.factories[cfg.Type]
		if !ok {
			slog.Warn("no factory registered for option type", "type", cfg.Type, "id", cfg.ID)
			continue
		}
		if _, exists := m.Clients[cfg.ID]; exists {
			slog.Warn("client with this ID already exists and will be overwritten", "id", cfg.ID)
		}
		m.Clients[cfg.ID] = factory(cfg)
	}
}
