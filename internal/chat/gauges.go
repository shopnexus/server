package chat

import (
	"go.opentelemetry.io/otel"

	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/platform/telemetry"
)

// registerGauges reports how many conversations there are, for the ops
// dashboard. Messages have no index on their time, so there is no gauge of
// recent ones.
func registerGauges(d module.Deps) {
	g := telemetry.CountGauges{Meter: otel.Meter("github.com/shopnexus/server/internal/chat"), Now: d.Now, Log: d.Log}
	telemetry.GaugeBy(g, "chat.conversations", "Conversations by kind.", "kind",
		[]domain.ConversationKind{domain.KindPair, domain.KindSupport}, repository.NewChats(d.Pool).CountByKind)
}
