// Package metrics provides Prometheus metrics for ShopNexus.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// HTTP metrics (populated by middleware)
var (
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shopnexus_http_requests_total",
			Help: "Total HTTP requests processed, labeled by method, path template, status.",
		},
		[]string{"method", "path", "status"},
	)
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shopnexus_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets, // 0.005..10s
		},
		[]string{"method", "path"},
	)
)

// Order module metrics
var (
	OrdersCreatedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shopnexus_orders_created_total",
			Help: "Total orders created via ConfirmSellerPending.",
		},
	)
	CheckoutItemsCreatedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shopnexus_checkout_items_created_total",
			Help: "Total pending checkout items created.",
		},
		[]string{"result"}, // success, failure
	)
	PaymentsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shopnexus_payments_total",
			Help: "Total payment events by status.",
		},
		[]string{"status", "provider"}, // Success/Failed/Cancelled, vnpay/card/cod
	)
	RefundsCreatedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shopnexus_refunds_created_total",
			Help: "Total refunds created.",
		},
	)
	DisputesCreatedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shopnexus_disputes_created_total",
			Help: "Total refund disputes opened.",
		},
	)
	TransportStatusUpdatesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shopnexus_transport_status_updates_total",
			Help: "Transport status transitions via webhook.",
		},
		[]string{"new_status"},
	)
	InventoryReservesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shopnexus_inventory_reserves_total",
			Help: "Inventory reservation attempts.",
		},
		[]string{"result"}, // success, failure
	)
)

// Handler-level metrics: count invocations of each business handler by
// outcome. Works in tandem with business counters (OrdersCreatedTotal, etc.)
// which only fire on success — this one captures failures too, so SLO dashboards
// can compute error rate = handler{result=failure} / handler{total}.
var (
	HandlerInvocationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shopnexus_handler_invocations_total",
			Help: "Total business handler invocations by module, name, outcome.",
		},
		[]string{"module", "handler", "result"},
	)
	HandlerDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shopnexus_handler_duration_seconds",
			Help:    "Business handler execution duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"module", "handler"},
	)
)

// TrackHandler instruments a business handler. Call via
// `defer metrics.TrackHandler("order", "BuyerCheckout", &err)()` at the top
// of a handler that uses a named `err` return. On exit it increments
// HandlerInvocationsTotal with result=success|failure and records duration.
func TrackHandler(module, handler string, errPtr *error) func() {
	start := time.Now()
	return func() {
		result := "success"
		if errPtr != nil && *errPtr != nil {
			result = "failure"
		}
		HandlerInvocationsTotal.WithLabelValues(module, handler, result).Inc()
		HandlerDuration.WithLabelValues(module, handler).Observe(time.Since(start).Seconds())
	}
}
