package ordermodel

import sharedmodel "shopnexus-server/internal/shared/model"

var (
	ErrOrderItemNotFound      = sharedmodel.NewError("order.order_item_not_found", "Sorry, we couldn't find the item you requested")
	ErrPaymentGatewayNotFound = sharedmodel.NewError("order.payment_gateway_not_found", "Sorry, we couldn't find the payment gateway you requested")
	ErrRefundAddressRequired  = sharedmodel.NewError("order.refund_address_required", "Address is required for pick up method")
	ErrRefundCannotBeUpdated  = sharedmodel.NewError("order.refund_cannot_be_updated", "Refund cannot be updated in its current status")
	ErrBuyNowSingleSkuOnly    = sharedmodel.NewError("order.buy_now_single_sku", "Buy now is only available for a single product")
	ErrOrderNotFound          = sharedmodel.NewError("order.order_not_found", "The order could not be found")
	ErrQuantityParamRequired  = sharedmodel.NewError("order.quantity_required", "Either quantity or delta_quantity must be provided")
	ErrBuyNowQuantityRequired = sharedmodel.NewError("order.buy_now_quantity_required", "Quantity is required for buy now checkout")
)
