package ordermodel

import sharedmodel "shopnexus-remastered/internal/shared/model"

var (
	ErrOrderItemNotFound      = sharedmodel.NewError("order.order_item_not_found", "Sorry, we couldn't find the item you requested")
	ErrPaymentGatewayNotFound = sharedmodel.NewError("order.payment_gateway_not_found", "Sorry, we couldn't find the payment gateway you requested")
	ErrRefundAddressRequired  = sharedmodel.NewError("order.refund_address_required", "Address is required for pick up method")
	ErrRefundCannotBeUpdated  = sharedmodel.NewError("order.refund_cannot_be_updated", "Refund cannot be updated in its current status")
)
