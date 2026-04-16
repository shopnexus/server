package orderbiz

import orderdb "shopnexus-server/internal/module/order/db/sqlc"

// ValidTransitions exports the internal validTransitions map for external tests.
var ValidTransitions = validTransitions

// OrderTransportStatus type aliases for convenience in external tests.
type OrderTransportStatus = orderdb.OrderTransportStatus
